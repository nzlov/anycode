package entstore

import (
	"context"
	"time"

	"github.com/nzlov/anycode/internal/domain/session"
	"github.com/nzlov/anycode/internal/infra/entstore/ent"
	"github.com/nzlov/anycode/internal/infra/entstore/ent/sessionshare"
	"github.com/nzlov/anycode/internal/infra/entstore/ent/sharedmessage"
)

type ShareRepository struct{ client *ent.Client }

func (s *Store) Shares() *ShareRepository { return &ShareRepository{s.client} }

var _ session.ShareRepository = (*ShareRepository)(nil)

func (r *ShareRepository) CreateShare(ctx context.Context, v session.ShareLink) error {
	return r.client.SessionShare.Create().SetID(v.ID).SetSessionID(string(v.SessionID)).SetTokenHash(v.TokenHash).SetCreatedAt(v.CreatedAt).Exec(ctx)
}
func shareLink(v *ent.SessionShare) session.ShareLink {
	return session.ShareLink{ID: v.ID, SessionID: session.ID(v.SessionID), TokenHash: v.TokenHash, GuestHash: v.GuestHash, CreatedAt: v.CreatedAt, ClaimedAt: v.ClaimedAt, RevokedAt: v.RevokedAt}
}
func (r *ShareRepository) FindShare(ctx context.Context, id string) (session.ShareLink, error) {
	v, err := r.client.SessionShare.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return session.ShareLink{}, session.ErrShareAccess
		}
		return session.ShareLink{}, err
	}
	return shareLink(v), nil
}
func (r *ShareRepository) ListShares(ctx context.Context, id session.ID) ([]session.ShareLink, error) {
	rows, err := r.client.SessionShare.Query().Where(sessionshare.SessionID(string(id))).Order(ent.Desc(sessionshare.FieldCreatedAt)).All(ctx)
	result := make([]session.ShareLink, 0, len(rows))
	for _, v := range rows {
		result = append(result, shareLink(v))
	}
	return result, err
}
func (r *ShareRepository) ClaimShare(ctx context.Context, id, tokenHash, guestHash string, now time.Time) (bool, error) {
	n, err := r.client.SessionShare.Update().Where(sessionshare.ID(id), sessionshare.TokenHash(tokenHash), sessionshare.ClaimedAtIsNil(), sessionshare.RevokedAtIsNil()).SetGuestHash(guestHash).SetClaimedAt(now).SetTokenHash("").Save(ctx)
	return n == 1, err
}
func (r *ShareRepository) RevokeShare(ctx context.Context, id string, now time.Time) error {
	return r.client.SessionShare.UpdateOneID(id).SetRevokedAt(now).SetGuestHash("").SetTokenHash("").Exec(ctx)
}
func (r *ShareRepository) CreateSharedMessage(ctx context.Context, v session.SharedMessage) error {
	return r.client.SharedMessage.Create().SetID(v.ID).SetSessionID(string(v.SessionID)).SetShareID(v.ShareID).SetContent(v.Content).SetStatus(v.Status).SetCreatedAt(v.CreatedAt).Exec(ctx)
}
func sharedMessage(v *ent.SharedMessage) session.SharedMessage {
	return session.SharedMessage{ID: v.ID, ShareID: v.ShareID, SessionID: session.ID(v.SessionID), Content: v.Content, Status: v.Status, ApprovalOrder: v.ApprovalOrder, CreatedAt: v.CreatedAt, ReviewedAt: v.ReviewedAt}
}
func (r *ShareRepository) FindSharedMessage(ctx context.Context, id string) (session.SharedMessage, error) {
	v, err := r.client.SharedMessage.Get(ctx, id)
	if err != nil {
		return session.SharedMessage{}, err
	}
	return sharedMessage(v), nil
}
func (r *ShareRepository) ListSharedMessages(ctx context.Context, id session.ID) ([]session.SharedMessage, error) {
	rows, err := r.client.SharedMessage.Query().Where(sharedmessage.SessionID(string(id))).Order(ent.Desc(sharedmessage.FieldCreatedAt)).All(ctx)
	result := make([]session.SharedMessage, 0, len(rows))
	for _, v := range rows {
		result = append(result, sharedMessage(v))
	}
	return result, err
}
func (r *ShareRepository) TransitionSharedMessage(ctx context.Context, id, from, to string, order int64, now time.Time) (bool, error) {
	update := r.client.SharedMessage.Update().Where(sharedmessage.ID(id), sharedmessage.Status(from)).SetStatus(to)
	if from == "pending" {
		update.SetReviewedAt(now)
	}
	if order > 0 {
		update.SetApprovalOrder(order)
	}
	n, err := update.Save(ctx)
	return n == 1, err
}

func (r *ShareRepository) ListApprovedSharedMessages(ctx context.Context) ([]session.SharedMessage, error) {
	rows, err := r.client.SharedMessage.Query().Where(sharedmessage.StatusIn("approved", "sending")).Order(ent.Asc(sharedmessage.FieldApprovalOrder)).All(ctx)
	result := make([]session.SharedMessage, 0, len(rows))
	for _, v := range rows {
		result = append(result, sharedMessage(v))
	}
	return result, err
}
