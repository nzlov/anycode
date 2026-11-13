package sharing

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	attachmentapp "github.com/nzlov/anycode/internal/application/attachment"
	sessionapp "github.com/nzlov/anycode/internal/application/session"
	"github.com/nzlov/anycode/internal/domain/session"
)

type SessionCommands interface {
	AppendPrompt(context.Context, sessionapp.AppendPromptInput) (sessionapp.PromptAppendDTO, error)
}
type Service struct {
	repo        session.ShareRepository
	sessions    session.Repository
	commands    SessionCommands
	attachments attachmentapp.UseCase
	staged      session.StagedAttachmentRepository
	files       session.AttachmentStore
	// Serialize reviews and revocations: appends enter the existing session queue in approval order.
	mu sync.Mutex
}

func New(repo session.ShareRepository, sessions session.Repository, commands SessionCommands, attachments attachmentapp.UseCase, staged session.StagedAttachmentRepository, files session.AttachmentStore) *Service {
	return &Service{repo: repo, sessions: sessions, commands: commands, attachments: attachments, staged: staged, files: files}
}
func secret() string {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(value[:])
}
func hash(v string) string { sum := sha256.Sum256([]byte(v)); return hex.EncodeToString(sum[:]) }
func (s *Service) Create(ctx context.Context, id session.ID) (session.ShareLink, string, error) {
	v, err := s.sessions.Find(ctx, id)
	if err != nil {
		return session.ShareLink{}, "", err
	}
	if v.Status == session.StatusClosed || v.Mode == session.ModeTerminal {
		return session.ShareLink{}, "", errors.New("该会话不能分享")
	}
	token := secret()
	link := session.ShareLink{ID: secret()[:24], SessionID: id, TokenHash: hash(token), CreatedAt: time.Now().UTC()}
	err = s.repo.CreateShare(ctx, link)
	return link, token, err
}
func (s *Service) Claim(ctx context.Context, id, token string) (session.ShareLink, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if token == "" {
		return session.ShareLink{}, "", session.ErrShareAccess
	}
	guest := secret()
	ok, err := s.repo.ClaimShare(ctx, id, hash(token), hash(guest), time.Now().UTC())
	if err != nil {
		return session.ShareLink{}, "", err
	}
	if !ok {
		return session.ShareLink{}, "", session.ErrShareConflict
	}
	link, err := s.repo.FindShare(ctx, id)
	return link, guest, err
}
func (s *Service) Active(ctx context.Context, id string) (session.ShareLink, error) {
	link, err := s.repo.FindShare(ctx, id)
	if err != nil {
		return session.ShareLink{}, err
	}
	if link.RevokedAt != nil || link.ClaimedAt == nil {
		return session.ShareLink{}, session.ErrShareAccess
	}
	v, err := s.sessions.Find(ctx, link.SessionID)
	if errors.Is(err, session.ErrSessionNotFound) || v.Status == session.StatusClosed {
		return session.ShareLink{}, session.ErrShareAccess
	}
	if err != nil {
		return session.ShareLink{}, err
	}
	return link, nil
}
func (s *Service) Authorize(ctx context.Context, id, credential string) (session.ShareLink, error) {
	link, err := s.Active(ctx, id)
	if err != nil {
		return link, err
	}
	if credential == "" || subtle.ConstantTimeCompare([]byte(link.GuestHash), []byte(hash(credential))) != 1 {
		return session.ShareLink{}, session.ErrShareAccess
	}
	return link, nil
}
func (s *Service) List(ctx context.Context, id session.ID) ([]session.ShareLink, error) {
	return s.repo.ListShares(ctx, id)
}
func (s *Service) Revoke(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.repo.RevokeShare(ctx, id, time.Now().UTC())
}
func (s *Service) Messages(ctx context.Context, id session.ID) ([]session.SharedMessage, error) {
	return s.repo.ListSharedMessages(ctx, id)
}

type Upload struct {
	Filename, MimeType string
	Size               int64
	Reader             io.Reader
}

func (s *Service) Submit(ctx context.Context, shareID, credential, body string, config session.Config, uploads []Upload) (session.SharedMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	link, err := s.Authorize(ctx, shareID, credential)
	if err != nil {
		return session.SharedMessage{}, err
	}
	if strings.TrimSpace(body) == "" && len(uploads) == 0 {
		return session.SharedMessage{}, errors.New("消息不能为空")
	}
	msg := session.SharedMessage{ID: secret()[:24], ShareID: shareID, SessionID: link.SessionID, Status: "pending", CreatedAt: time.Now().UTC(), Content: session.SharedMessageContent{Body: body, Config: config, Attachments: []session.SharedMessageAttachment{}}}
	for _, u := range uploads {
		v, e := s.attachments.StageAttachment(ctx, attachmentapp.StageAttachmentInput{OwnerKeyHash: link.GuestHash, Filename: u.Filename, MimeType: u.MimeType, Size: u.Size, Reader: u.Reader})
		if e != nil {
			err = e
			break
		}
		msg.Content.Attachments = append(msg.Content.Attachments, session.SharedMessageAttachment{ID: session.StagedAttachmentID(v.ID), Filename: v.Filename, MimeType: v.MimeType, Size: v.Size})
	}
	if err == nil {
		err = s.repo.CreateSharedMessage(ctx, msg)
	}
	if err != nil {
		for _, a := range msg.Content.Attachments {
			_ = s.attachments.DeleteStagedAttachment(context.WithoutCancel(ctx), a.ID)
		}
	}
	return msg, err
}
func (s *Service) Review(ctx context.Context, id string, approved bool) (session.SharedMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	msg, err := s.repo.FindSharedMessage(ctx, id)
	if err != nil {
		return msg, err
	}
	if msg.Status != "pending" {
		return msg, session.ErrShareConflict
	}
	status := "rejected"
	if approved {
		status = "approved"
		if _, err = s.Active(ctx, msg.ShareID); err != nil {
			return msg, err
		}
		messages, e := s.repo.ListSharedMessages(ctx, msg.SessionID)
		if e != nil {
			return msg, e
		}
		for _, previous := range messages {
			if previous.ApprovalOrder > msg.ApprovalOrder {
				msg.ApprovalOrder = previous.ApprovalOrder
			}
		}
		msg.ApprovalOrder++
	}
	now := time.Now().UTC()
	msg.ReviewedAt = &now
	ok, err := s.repo.TransitionSharedMessage(ctx, id, "pending", status, msg.ApprovalOrder, *msg.ReviewedAt)
	if err != nil {
		return msg, err
	}
	if !ok {
		return msg, session.ErrShareConflict
	}
	msg.Status = status
	return msg, nil
}

// DispatchApproved sends at most one approved message per idle session. Approved
// messages survive restarts; dispatch retries use the approved message ID to prevent duplicate appends.
func (s *Service) DispatchApproved(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	messages, err := s.repo.ListApprovedSharedMessages(ctx)
	if err != nil {
		return err
	}
	seen := map[session.ID]bool{}
	for _, msg := range messages {
		if seen[msg.SessionID] {
			continue
		}
		seen[msg.SessionID] = true
		if _, err = s.Active(ctx, msg.ShareID); err != nil {
			if errors.Is(err, session.ErrShareAccess) {
				_, err = s.repo.TransitionSharedMessage(ctx, msg.ID, msg.Status, "cancelled", 0, time.Now().UTC())
				if err != nil {
					return err
				}
				continue
			}
			return err
		}
		current, err := s.sessions.Find(ctx, msg.SessionID)
		if err != nil {
			return err
		}
		if !current.CanAcceptSharedMessage() {
			continue
		}
		all, err := s.repo.ListSharedMessages(ctx, msg.SessionID)
		if err != nil {
			return err
		}
		uncertain := false
		for _, previous := range all {
			if previous.Status == "sending" && previous.ID != msg.ID {
				uncertain = true
				break
			}
		}
		if uncertain {
			continue
		}
		ok := true
		if msg.Status == "approved" {
			ok, err = s.repo.TransitionSharedMessage(ctx, msg.ID, "approved", "sending", 0, time.Now().UTC())
		}
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		ids := make([]session.StagedAttachmentID, 0, len(msg.Content.Attachments))
		for _, a := range msg.Content.Attachments {
			ids = append(ids, a.ID)
		}
		_, dispatchErr := s.commands.AppendPrompt(ctx, sessionapp.AppendPromptInput{SessionID: msg.SessionID, Body: msg.Content.Body, StagedAttachmentIDs: ids, ExpectedConfig: &msg.Content.Config, RequireIdle: true, ApprovedMessageID: msg.ID})
		status := "sent"
		if dispatchErr != nil {
			status = "failed"
		}
		if errors.Is(dispatchErr, session.ErrShareSessionBusy) {
			status = "approved"
		}
		_, saveErr := s.repo.TransitionSharedMessage(context.WithoutCancel(ctx), msg.ID, "sending", status, 0, time.Now().UTC())
		if saveErr != nil {
			return saveErr
		}
	}
	return nil
}

func (s *Service) CheckFile(ctx context.Context, link session.ShareLink, id string) error {
	v, err := s.files.FindSessionFile(ctx, session.SessionFileID(id))
	if err != nil || v.SessionID != link.SessionID {
		return session.ErrShareAccess
	}
	return nil
}
func (s *Service) OpenReviewAttachment(ctx context.Context, messageID, fileID string) (attachmentapp.Stream, error) {
	msg, err := s.repo.FindSharedMessage(ctx, messageID)
	if err != nil {
		return attachmentapp.Stream{}, err
	}
	for _, a := range msg.Content.Attachments {
		if string(a.ID) == fileID {
			v, err := s.staged.FindStagedAttachment(ctx, a.ID)
			if err != nil {
				archived, findErr := s.files.FindSessionFile(ctx, session.SessionFileID(fileID))
				if findErr != nil || archived.SessionID != msg.SessionID || archived.SourceID != "share-"+msg.ID {
					return attachmentapp.Stream{}, session.ErrShareAccess
				}
				return s.attachments.OpenAttachment(ctx, session.AttachmentID(fileID), attachmentapp.OpenDownload)
			}
			stream, err := s.files.Open(ctx, v.Path)
			return attachmentapp.Stream{Filename: v.Filename, MimeType: v.MimeType, Size: v.Size, ModifiedAt: v.CreatedAt, Reader: stream.Reader, Seeker: stream.Seeker}, err
		}
	}
	return attachmentapp.Stream{}, session.ErrShareAccess
}
