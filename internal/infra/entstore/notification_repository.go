package entstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	notificationdomain "github.com/nzlov/anycode/internal/domain/notification"
	"github.com/nzlov/anycode/internal/infra/entstore/ent"
	"github.com/nzlov/anycode/internal/infra/entstore/ent/notificationdelivery"
	"github.com/nzlov/anycode/internal/infra/entstore/ent/pushsubscription"
)

const (
	globalNotificationConfigurationID = "global"
	globalNotificationCheckpointID    = "card-events"
)

type NotificationRepository struct {
	client *ent.Client
}

func NewNotificationRepository(client *ent.Client) *NotificationRepository {
	return &NotificationRepository{client: client}
}

func (r *NotificationRepository) GetConfiguration(ctx context.Context) (notificationdomain.Configuration, bool, error) {
	row, err := r.client.NotificationConfiguration.Get(ctx, globalNotificationConfigurationID)
	if ent.IsNotFound(err) {
		return notificationdomain.Configuration{}, false, nil
	}
	if err != nil {
		return notificationdomain.Configuration{}, false, fmt.Errorf("get notification configuration: %w", err)
	}
	return toNotificationConfiguration(row), true, nil
}

func (r *NotificationRepository) CreateConfiguration(ctx context.Context, configuration notificationdomain.Configuration) (notificationdomain.Configuration, error) {
	row, err := r.client.NotificationConfiguration.Create().
		SetID(globalNotificationConfigurationID).
		SetVapidPublicKey(configuration.VAPIDPublicKey).
		SetVapidPrivateKey(configuration.VAPIDPrivateKey).
		SetVapidSubject(configuration.VAPIDSubject).
		SetCreatedAt(configuration.CreatedAt).
		Save(ctx)
	if ent.IsConstraintError(err) {
		existing, ok, getErr := r.GetConfiguration(ctx)
		if getErr == nil && ok {
			return existing, nil
		}
	}
	if err != nil {
		return notificationdomain.Configuration{}, fmt.Errorf("create notification configuration: %w", err)
	}
	return toNotificationConfiguration(row), nil
}

func (r *NotificationRepository) UpsertSubscription(ctx context.Context, subscription notificationdomain.Subscription) (notificationdomain.Subscription, error) {
	row, err := r.client.PushSubscription.Query().
		Where(pushsubscription.EndpointHashEQ(subscription.EndpointHash)).
		Only(ctx)
	if err == nil {
		row, err = row.Update().
			SetPrincipalKeyHash(subscription.PrincipalKeyHash).
			SetEndpoint(subscription.Endpoint).
			SetP256dh(subscription.P256DH).
			SetAuth(subscription.Auth).
			SetUpdatedAt(subscription.UpdatedAt).
			Save(ctx)
		if err != nil {
			return notificationdomain.Subscription{}, fmt.Errorf("update push subscription: %w", err)
		}
		return toPushSubscription(row), nil
	}
	if !ent.IsNotFound(err) {
		return notificationdomain.Subscription{}, fmt.Errorf("find push subscription: %w", err)
	}
	row, err = r.client.PushSubscription.Create().
		SetID(subscription.ID).
		SetPrincipalKeyHash(subscription.PrincipalKeyHash).
		SetEndpointHash(subscription.EndpointHash).
		SetEndpoint(subscription.Endpoint).
		SetP256dh(subscription.P256DH).
		SetAuth(subscription.Auth).
		SetCreatedAt(subscription.CreatedAt).
		SetUpdatedAt(subscription.UpdatedAt).
		Save(ctx)
	if err != nil {
		return notificationdomain.Subscription{}, fmt.Errorf("create push subscription: %w", err)
	}
	return toPushSubscription(row), nil
}

func (r *NotificationRepository) DeleteSubscription(ctx context.Context, id string, principalKeyHash string) error {
	row, err := r.client.PushSubscription.Query().
		Where(pushsubscription.IDEQ(id), pushsubscription.PrincipalKeyHashEQ(principalKeyHash)).
		Only(ctx)
	if ent.IsNotFound(err) {
		return notificationdomain.ErrSubscriptionNotFound
	}
	if err != nil {
		return fmt.Errorf("find push subscription for delete: %w", err)
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin push subscription delete: %w", err)
	}
	if _, err := tx.NotificationDelivery.Update().
		Where(notificationdelivery.SubscriptionIDEQ(row.ID), notificationdelivery.StatusEQ(string(notificationdomain.DeliveryPending))).
		SetStatus(string(notificationdomain.DeliveryDiscarded)).
		SetLastError("subscription removed").
		SetUpdatedAt(time.Now().UTC()).
		Save(ctx); err != nil {
		return rollbackNotificationTx(tx, fmt.Errorf("discard subscription deliveries: %w", err))
	}
	if err := tx.PushSubscription.DeleteOneID(row.ID).Exec(ctx); err != nil {
		return rollbackNotificationTx(tx, fmt.Errorf("delete push subscription: %w", err))
	}
	return commitNotificationTx(tx)
}

func (r *NotificationRepository) GetCheckpoint(ctx context.Context) (string, bool, error) {
	row, err := r.client.NotificationCheckpoint.Get(ctx, globalNotificationCheckpointID)
	if ent.IsNotFound(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get notification checkpoint: %w", err)
	}
	return row.LastEventID, true, nil
}

func (r *NotificationRepository) InitializeCheckpoint(ctx context.Context, eventID string, now time.Time) error {
	_, err := r.client.NotificationCheckpoint.Create().
		SetID(globalNotificationCheckpointID).
		SetLastEventID(eventID).
		SetUpdatedAt(now).
		Save(ctx)
	if ent.IsConstraintError(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("initialize notification checkpoint: %w", err)
	}
	return nil
}

func (r *NotificationRepository) AdvanceCheckpoint(ctx context.Context, eventID string, now time.Time) error {
	return advanceNotificationCheckpoint(ctx, r.client, eventID, now)
}

func (r *NotificationRepository) PlanEvent(ctx context.Context, eventID string, principalKeyHash string, occurredAt time.Time, payload []byte, now time.Time) error {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin notification plan: %w", err)
	}
	query := tx.PushSubscription.Query().Where(pushsubscription.PrincipalKeyHashEQ(principalKeyHash))
	if !occurredAt.IsZero() {
		query.Where(pushsubscription.CreatedAtLTE(occurredAt))
	}
	subscriptions, err := query.All(ctx)
	if err != nil {
		return rollbackNotificationTx(tx, fmt.Errorf("list notification subscriptions: %w", err))
	}
	for _, subscription := range subscriptions {
		sum := sha256.Sum256([]byte(eventID + "\x00" + subscription.ID))
		id := hex.EncodeToString(sum[:])
		err := tx.NotificationDelivery.Create().
			SetID(id).
			SetEventID(eventID).
			SetSubscriptionID(subscription.ID).
			SetPayload(payload).
			SetStatus(string(notificationdomain.DeliveryPending)).
			SetNextAttemptAt(now).
			SetCreatedAt(now).
			SetUpdatedAt(now).
			Exec(ctx)
		if err != nil && !ent.IsConstraintError(err) {
			return rollbackNotificationTx(tx, fmt.Errorf("create notification delivery: %w", err))
		}
	}
	if err := advanceNotificationCheckpoint(ctx, tx.Client(), eventID, now); err != nil {
		return rollbackNotificationTx(tx, err)
	}
	return commitNotificationTx(tx)
}

func (r *NotificationRepository) PendingDeliveries(ctx context.Context, now time.Time, limit int) ([]notificationdomain.Delivery, error) {
	rows, err := r.client.NotificationDelivery.Query().
		Where(
			notificationdelivery.StatusEQ(string(notificationdomain.DeliveryPending)),
			notificationdelivery.NextAttemptAtLTE(now),
		).
		Order(ent.Asc(notificationdelivery.FieldNextAttemptAt), ent.Asc(notificationdelivery.FieldCreatedAt)).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list notification deliveries: %w", err)
	}
	deliveries := make([]notificationdomain.Delivery, 0, len(rows))
	for _, row := range rows {
		subscription, err := r.client.PushSubscription.Get(ctx, row.SubscriptionID)
		if ent.IsNotFound(err) {
			if discardErr := r.DiscardDelivery(ctx, row.ID, "subscription unavailable", now); discardErr != nil {
				return nil, discardErr
			}
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("get delivery subscription: %w", err)
		}
		deliveries = append(deliveries, notificationdomain.Delivery{
			ID: row.ID, EventID: row.EventID, Subscription: toPushSubscription(subscription),
			Payload: append([]byte(nil), row.Payload...), Attempts: row.Attempts, NextAttempt: row.NextAttemptAt,
		})
	}
	return deliveries, nil
}

func (r *NotificationRepository) MarkDeliverySent(ctx context.Context, id string, now time.Time) error {
	return r.updateDelivery(ctx, id, notificationdomain.DeliverySent, "", nil, now)
}

func (r *NotificationRepository) RetryDelivery(ctx context.Context, id string, nextAttempt time.Time, message string, now time.Time) error {
	return r.updateDelivery(ctx, id, notificationdomain.DeliveryPending, message, &nextAttempt, now)
}

func (r *NotificationRepository) DiscardDelivery(ctx context.Context, id string, message string, now time.Time) error {
	return r.updateDelivery(ctx, id, notificationdomain.DeliveryDiscarded, message, nil, now)
}

func (r *NotificationRepository) updateDelivery(ctx context.Context, id string, status notificationdomain.DeliveryStatus, message string, nextAttempt *time.Time, now time.Time) error {
	update := r.client.NotificationDelivery.UpdateOneID(id).
		SetStatus(string(status)).
		SetLastError(message).
		SetUpdatedAt(now)
	if status != notificationdomain.DeliverySent {
		update.AddAttempts(1)
	}
	if nextAttempt != nil {
		update.SetNextAttemptAt(*nextAttempt)
	}
	if err := update.Exec(ctx); err != nil {
		return fmt.Errorf("update notification delivery: %w", err)
	}
	return nil
}

func (r *NotificationRepository) ExpireSubscription(ctx context.Context, subscriptionID string, now time.Time) error {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin subscription expiry: %w", err)
	}
	if _, err := tx.NotificationDelivery.Update().
		Where(notificationdelivery.SubscriptionIDEQ(subscriptionID), notificationdelivery.StatusEQ(string(notificationdomain.DeliveryPending))).
		SetStatus(string(notificationdomain.DeliveryDiscarded)).
		SetLastError("push subscription expired").
		SetUpdatedAt(now).
		Save(ctx); err != nil {
		return rollbackNotificationTx(tx, fmt.Errorf("discard expired subscription deliveries: %w", err))
	}
	if err := tx.PushSubscription.DeleteOneID(subscriptionID).Exec(ctx); err != nil && !ent.IsNotFound(err) {
		return rollbackNotificationTx(tx, fmt.Errorf("delete expired push subscription: %w", err))
	}
	return commitNotificationTx(tx)
}

func advanceNotificationCheckpoint(ctx context.Context, client *ent.Client, eventID string, now time.Time) error {
	err := client.NotificationCheckpoint.UpdateOneID(globalNotificationCheckpointID).
		SetLastEventID(eventID).
		SetUpdatedAt(now).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("advance notification checkpoint: %w", err)
	}
	return nil
}

func toNotificationConfiguration(row *ent.NotificationConfiguration) notificationdomain.Configuration {
	return notificationdomain.Configuration{
		VAPIDPublicKey: row.VapidPublicKey, VAPIDPrivateKey: row.VapidPrivateKey,
		VAPIDSubject: row.VapidSubject, CreatedAt: row.CreatedAt,
	}
}

func toPushSubscription(row *ent.PushSubscription) notificationdomain.Subscription {
	return notificationdomain.Subscription{
		ID: row.ID, PrincipalKeyHash: row.PrincipalKeyHash, EndpointHash: row.EndpointHash,
		Endpoint: row.Endpoint, P256DH: row.P256dh, Auth: row.Auth,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func rollbackNotificationTx(tx *ent.Tx, cause error) error {
	if rollbackErr := tx.Rollback(); rollbackErr != nil {
		return fmt.Errorf("%w; rollback notification transaction: %v", cause, rollbackErr)
	}
	return cause
}

func commitNotificationTx(tx *ent.Tx) error {
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit notification transaction: %w", err)
	}
	return nil
}
