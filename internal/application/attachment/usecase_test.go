package attachment

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	domain "github.com/nzlov/anycode/internal/domain/session"
)

func TestStageAttachmentPersistsMetadataAndCleansFileOnRepositoryFailure(t *testing.T) {
	ctx := context.Background()
	store := newFakeStore()
	repo := newFakeAttachmentRepository()
	service := New(repo, store)

	got, err := service.StageAttachment(ctx, StageAttachmentInput{
		OwnerKeyHash: "owner",
		Filename:     "note.txt",
		MimeType:     "text/plain",
		Reader:       strings.NewReader("hello"),
	})
	if err != nil {
		t.Fatalf("StageAttachment() error = %v", err)
	}
	if got.ID != "staged-1" || got.Filename != "note.txt" || got.MimeType != "text/plain" {
		t.Fatalf("StageAttachment() DTO = %#v", got)
	}
	if _, ok := repo.staged["staged-1"]; !ok {
		t.Fatal("staged metadata was not saved")
	}

	repo = newFakeAttachmentRepository()
	repo.saveStagedErr = errors.New("db failed")
	store = newFakeStore()
	service = New(repo, store)
	if _, err := service.StageAttachment(ctx, StageAttachmentInput{
		Filename: "broken.txt",
		Reader:   strings.NewReader("hello"),
	}); err == nil {
		t.Fatal("StageAttachment() expected error")
	}
	if !store.deletedStaged["staged-1"] {
		t.Fatal("StageAttachment() did not clean staged file after metadata failure")
	}
}

func TestDeleteAttachmentsRemovesFileAndMetadata(t *testing.T) {
	ctx := context.Background()
	repo := newFakeAttachmentRepository()
	repo.staged["staged-1"] = domain.StagedAttachment{ID: "staged-1"}
	repo.sessions["attachment-1"] = domain.SessionAttachment{ID: "attachment-1", Path: "/attachments/file.txt"}
	store := newFakeStore()
	service := New(repo, store)

	if err := service.DeleteStagedAttachment(ctx, "staged-1"); err != nil {
		t.Fatalf("DeleteStagedAttachment() error = %v", err)
	}
	if !store.deletedStaged["staged-1"] {
		t.Fatal("staged file was not deleted")
	}
	if _, ok := repo.staged["staged-1"]; ok {
		t.Fatal("staged metadata was not deleted")
	}

	if err := service.DeleteSessionAttachment(ctx, "attachment-1"); err != nil {
		t.Fatalf("DeleteSessionAttachment() error = %v", err)
	}
	if !store.deletedSessions["attachment-1"] {
		t.Fatal("session file was not deleted")
	}
	if _, ok := repo.sessions["attachment-1"]; ok {
		t.Fatal("session metadata was not deleted")
	}
}

type fakeAttachmentRepository struct {
	staged        map[domain.StagedAttachmentID]domain.StagedAttachment
	sessions      map[domain.SessionAttachmentID]domain.SessionAttachment
	saveStagedErr error
}

func newFakeAttachmentRepository() *fakeAttachmentRepository {
	return &fakeAttachmentRepository{
		staged:   map[domain.StagedAttachmentID]domain.StagedAttachment{},
		sessions: map[domain.SessionAttachmentID]domain.SessionAttachment{},
	}
}

func (r *fakeAttachmentRepository) SaveStagedAttachment(_ context.Context, attachment domain.StagedAttachment) error {
	if r.saveStagedErr != nil {
		return r.saveStagedErr
	}
	r.staged[attachment.ID] = attachment
	return nil
}

func (r *fakeAttachmentRepository) FindStagedAttachment(_ context.Context, id domain.StagedAttachmentID) (domain.StagedAttachment, error) {
	attachment, ok := r.staged[id]
	if !ok {
		return domain.StagedAttachment{}, errors.New("not found")
	}
	return attachment, nil
}

func (r *fakeAttachmentRepository) DeleteStagedAttachment(_ context.Context, id domain.StagedAttachmentID) error {
	delete(r.staged, id)
	return nil
}

func (r *fakeAttachmentRepository) SaveSessionAttachment(_ context.Context, attachment domain.SessionAttachment) error {
	r.sessions[attachment.ID] = attachment
	return nil
}

func (r *fakeAttachmentRepository) FindSessionAttachment(_ context.Context, id domain.SessionAttachmentID) (domain.SessionAttachment, error) {
	attachment, ok := r.sessions[id]
	if !ok {
		return domain.SessionAttachment{}, errors.New("not found")
	}
	return attachment, nil
}

func (r *fakeAttachmentRepository) ListSessionAttachments(_ context.Context, sessionID domain.ID) ([]domain.SessionAttachment, error) {
	var attachments []domain.SessionAttachment
	for _, attachment := range r.sessions {
		if attachment.SessionID == sessionID {
			attachments = append(attachments, attachment)
		}
	}
	return attachments, nil
}

func (r *fakeAttachmentRepository) ListPromptAppendAttachments(context.Context, domain.ID, string) ([]domain.SessionAttachment, error) {
	return nil, nil
}

func (r *fakeAttachmentRepository) DeleteSessionAttachment(_ context.Context, id domain.SessionAttachmentID) error {
	delete(r.sessions, id)
	return nil
}

type fakeStore struct {
	deletedStaged   map[domain.StagedAttachmentID]bool
	deletedSessions map[domain.SessionAttachmentID]bool
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		deletedStaged:   map[domain.StagedAttachmentID]bool{},
		deletedSessions: map[domain.SessionAttachmentID]bool{},
	}
}

func (s *fakeStore) Stage(_ context.Context, input domain.StageAttachmentInput) (domain.StagedAttachment, error) {
	return domain.StagedAttachment{
		ID:           "staged-1",
		OwnerKeyHash: input.OwnerKeyHash,
		Filename:     input.Filename,
		Path:         "/attachments/staged-1/" + input.Filename,
		MimeType:     input.MimeType,
		Size:         5,
		CreatedAt:    time.Unix(10, 0).UTC(),
	}, nil
}

func (s *fakeStore) Promote(_ context.Context, staged domain.StagedAttachment, sessionID domain.ID) (domain.SessionAttachment, error) {
	return domain.SessionAttachment{
		ID:        domain.SessionAttachmentID(staged.ID),
		SessionID: sessionID,
		Filename:  staged.Filename,
		Path:      "/attachments/session/" + staged.Filename,
		MimeType:  staged.MimeType,
		Size:      staged.Size,
		CreatedAt: time.Unix(11, 0).UTC(),
	}, nil
}

func (s *fakeStore) DeleteStaged(_ context.Context, id domain.StagedAttachmentID) error {
	s.deletedStaged[id] = true
	return nil
}

func (s *fakeStore) DeleteSession(_ context.Context, id domain.SessionAttachmentID) error {
	s.deletedSessions[id] = true
	return nil
}

func (s *fakeStore) Open(context.Context, string) (domain.AttachmentStream, error) {
	return domain.AttachmentStream{Reader: io.NopCloser(strings.NewReader(""))}, nil
}
