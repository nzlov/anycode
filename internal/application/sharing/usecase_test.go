package sharing_test

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	attachmentapp "github.com/nzlov/anycode/internal/application/attachment"
	sessionapp "github.com/nzlov/anycode/internal/application/session"
	"github.com/nzlov/anycode/internal/application/sharing"
	"github.com/nzlov/anycode/internal/domain/session"
	"github.com/nzlov/anycode/internal/infra/entstore"
	"github.com/nzlov/anycode/internal/infra/filestore"
)

type commands struct {
	mu     sync.Mutex
	bodies []string
	fail   bool
}

func (c *commands) AppendPrompt(_ context.Context, input sessionapp.AppendPromptInput) (sessionapp.PromptAppendDTO, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bodies = append(c.bodies, input.Body)
	if input.ExpectedConfig == nil {
		panic("missing reviewed config")
	}
	if c.fail {
		return sessionapp.PromptAppendDTO{}, errors.New("dispatch failed")
	}
	return sessionapp.PromptAppendDTO{}, nil
}
func setup(t *testing.T) (*sharing.Service, *entstore.Store, *commands) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	store, err := entstore.Open(ctx, entstore.OpenOptions{DatabaseURL: filepath.Join(dir, "db"), DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = store.Sessions().Create(ctx, session.Session{ID: "session", ProjectID: "project", Mode: session.ModeChat, Status: session.StatusStopped}); err != nil {
		t.Fatal(err)
	}
	files := filestore.New(dir)
	attachments := attachmentapp.New(store.Attachments(), files)
	cmd := &commands{}
	return sharing.New(store.Shares(), store.Sessions(), cmd, attachments, store.Attachments(), files), store, cmd
}
func TestShareClaimIsSingleUseUnderConcurrencyAndPersists(t *testing.T) {
	s, store, cmd := setup(t)
	ctx := context.Background()
	link, token, err := s.Create(ctx, "session")
	if err != nil {
		t.Fatal(err)
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	var guest string
	var mu sync.Mutex
	for range 16 {
		wg.Go(func() {
			_, credential, e := s.Claim(ctx, link.ID, token)
			if e == nil {
				wins.Add(1)
				mu.Lock()
				guest = credential
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("claims=%d", wins.Load())
	}
	other := sharing.New(store.Shares(), store.Sessions(), cmd, nil, nil, nil)
	if _, _, err = other.Claim(ctx, link.ID, token); !errors.Is(err, session.ErrShareConflict) {
		t.Fatalf("token reused: %v", err)
	}
	if _, err = other.Authorize(ctx, link.ID, guest); err != nil {
		t.Fatal(err)
	}
	if _, err = other.Authorize(ctx, link.ID, token); !errors.Is(err, session.ErrShareAccess) {
		t.Fatal("invitation accepted as guest credential")
	}
	if err = s.Revoke(ctx, link.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = other.Authorize(ctx, link.ID, guest); !errors.Is(err, session.ErrShareAccess) {
		t.Fatal("revoked guest authorized")
	}
}
func TestMultipleGuestsEachMessageRequiresReviewAndUsesApprovalOrder(t *testing.T) {
	s, _, cmd := setup(t)
	ctx := context.Background()
	l1, t1, _ := s.Create(ctx, "session")
	l2, t2, _ := s.Create(ctx, "session")
	_, g1, _ := s.Claim(ctx, l1.ID, t1)
	_, g2, _ := s.Claim(ctx, l2.ID, t2)
	m1, err := s.Submit(ctx, l1.ID, g1, "first submitted", session.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	m2, err := s.Submit(ctx, l2.ID, g2, "second submitted", session.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	m3, _ := s.Submit(ctx, l1.ID, g1, "rejected", session.Config{}, nil)
	if len(cmd.bodies) != 0 {
		t.Fatal("unreviewed messages dispatched")
	}
	if _, err = s.Review(ctx, m2.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Review(ctx, m1.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Review(ctx, m3.ID, false); err != nil {
		t.Fatal(err)
	}
	s.DispatchApproved(ctx)
	s.DispatchApproved(ctx)
	if !reflect.DeepEqual(cmd.bodies, []string{"second submitted", "first submitted"}) {
		t.Fatalf("order=%v", cmd.bodies)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() { s.Review(ctx, m2.ID, true) })
	}
	wg.Wait()
	if len(cmd.bodies) != 2 {
		t.Fatal("review replay dispatched again")
	}
	if _, err = s.Submit(ctx, l1.ID, g2, "wrong guest", session.Config{}, nil); !errors.Is(err, session.ErrShareAccess) {
		t.Fatal("cross-share credential accepted")
	}
}
func TestFailedDispatchAndRevocationNeverReplayApproval(t *testing.T) {
	s, _, cmd := setup(t)
	ctx := context.Background()
	link, token, _ := s.Create(ctx, "session")
	_, guest, _ := s.Claim(ctx, link.ID, token)
	msg, _ := s.Submit(ctx, link.ID, guest, "failure", session.Config{}, nil)
	cmd.fail = true
	s.Review(ctx, msg.ID, true)
	s.DispatchApproved(ctx)
	s.Review(ctx, msg.ID, true)
	s.DispatchApproved(ctx)
	if len(cmd.bodies) != 1 {
		t.Fatal("failed approval repeated")
	}
	pending, _ := s.Submit(ctx, link.ID, guest, "revoked", session.Config{}, nil)
	s.Revoke(ctx, link.ID)
	if _, err := s.Review(ctx, pending.ID, true); !errors.Is(err, session.ErrShareAccess) {
		t.Fatal("revoked share approved")
	}
	if _, err := s.Submit(ctx, link.ID, guest, "later", session.Config{}, nil); !errors.Is(err, session.ErrShareAccess) {
		t.Fatal("revoked guest sent")
	}
}

func TestApprovedQueueWaitsForIdleAndSurvivesServiceRestart(t *testing.T) {
	s, store, cmd := setup(t)
	ctx := context.Background()
	link, token, _ := s.Create(ctx, "session")
	_, guest, _ := s.Claim(ctx, link.ID, token)
	m1, _ := s.Submit(ctx, link.ID, guest, "submitted first", session.Config{}, nil)
	m2, _ := s.Submit(ctx, link.ID, guest, "approved first", session.Config{}, nil)
	v, _ := store.Sessions().Find(ctx, "session")
	v.Status = session.StatusRunning
	if err := store.Sessions().Save(ctx, v); err != nil {
		t.Fatal(err)
	}
	s.Review(ctx, m2.ID, true)
	s.Review(ctx, m1.ID, true)
	if err := s.DispatchApproved(ctx); err != nil {
		t.Fatal(err)
	}
	if len(cmd.bodies) != 0 {
		t.Fatal("approved message interrupted running turn")
	}
	restarted := sharing.New(store.Shares(), store.Sessions(), cmd, nil, nil, nil)
	v.Status = session.StatusStopped
	store.Sessions().Save(ctx, v)
	if err := restarted.DispatchApproved(ctx); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cmd.bodies, []string{"approved first"}) {
		t.Fatalf("first dispatch %v", cmd.bodies)
	}
	v.Status = session.StatusRunning
	store.Sessions().Save(ctx, v)
	restarted.DispatchApproved(ctx)
	if len(cmd.bodies) != 1 {
		t.Fatal("second message overlapped first")
	}
	v.Status = session.StatusStopped
	store.Sessions().Save(ctx, v)
	restarted.DispatchApproved(ctx)
	if !reflect.DeepEqual(cmd.bodies, []string{"approved first", "submitted first"}) {
		t.Fatalf("dispatch order %v", cmd.bodies)
	}
}
