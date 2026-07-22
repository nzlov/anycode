package entstore

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	notificationdomain "github.com/nzlov/anycode/internal/domain/notification"
)

func TestNotificationRepositoryPersistsConfigurationAndPlansDeliveriesOnce(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate store: %v", err)
	}
	repo := store.Notifications()
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)

	if _, ok, err := repo.GetConfiguration(ctx); err != nil || ok {
		t.Fatalf("initial configuration exists = %t, error = %v", ok, err)
	}
	wantConfig := notificationdomain.Configuration{
		VAPIDPublicKey: "public", VAPIDPrivateKey: "private", VAPIDSubject: "https://example.test",
		ProxyURL: "http://proxy.example:8080", CreatedAt: now,
	}
	if _, err := repo.CreateConfiguration(ctx, wantConfig); err != nil {
		t.Fatalf("create configuration: %v", err)
	}
	gotConfig, ok, err := repo.GetConfiguration(ctx)
	if err != nil || !ok || gotConfig != wantConfig {
		t.Fatalf("configuration = %#v, %t, %v", gotConfig, ok, err)
	}
	wantConfig.ProxyURL = "socks5://proxy.example:1080"
	if _, err := repo.SetProxyURL(ctx, wantConfig.ProxyURL); err != nil {
		t.Fatalf("update configuration: %v", err)
	}
	gotConfig, ok, err = repo.GetConfiguration(ctx)
	if err != nil || !ok || gotConfig != wantConfig {
		t.Fatalf("updated configuration = %#v, %t, %v", gotConfig, ok, err)
	}

	subscription := notificationdomain.Subscription{
		ID: "subscription-1", PrincipalKeyHash: "principal", EndpointHash: "endpoint-hash",
		Endpoint: "https://push.example/1", P256DH: "p256dh", Auth: "auth", CreatedAt: now, UpdatedAt: now,
	}
	if _, err := repo.UpsertSubscription(ctx, subscription); err != nil {
		t.Fatalf("upsert subscription: %v", err)
	}
	otherSubscription := subscription
	otherSubscription.ID = "subscription-2"
	otherSubscription.PrincipalKeyHash = "old-principal"
	otherSubscription.EndpointHash = "other-endpoint-hash"
	otherSubscription.Endpoint = "https://push.example/2"
	if _, err := repo.UpsertSubscription(ctx, otherSubscription); err != nil {
		t.Fatalf("upsert old-principal subscription: %v", err)
	}
	if err := repo.InitializeCheckpoint(ctx, "event-0", now); err != nil {
		t.Fatalf("initialize checkpoint: %v", err)
	}
	for range 2 {
		if err := repo.PlanEvent(ctx, "event-1", "principal", now.Add(time.Second), []byte(`{"title":"done"}`), now.Add(time.Second)); err != nil {
			t.Fatalf("plan event: %v", err)
		}
	}
	deliveries, err := repo.PendingDeliveries(ctx, now.Add(2*time.Second), 10)
	if err != nil {
		t.Fatalf("pending deliveries: %v", err)
	}
	if len(deliveries) != 1 || deliveries[0].EventID != "event-1" || deliveries[0].Subscription.ID != "subscription-1" {
		t.Fatalf("deliveries = %#v", deliveries)
	}
	checkpoint, ok, err := repo.GetCheckpoint(ctx)
	if err != nil || !ok || checkpoint != "event-1" {
		t.Fatalf("checkpoint = %q, %t, %v", checkpoint, ok, err)
	}
	if err := repo.DeleteSubscription(ctx, "subscription-1", "other-principal"); !errors.Is(err, notificationdomain.ErrSubscriptionNotFound) {
		t.Fatalf("delete subscription with another principal error = %v", err)
	}
	if err := repo.DeleteSubscription(ctx, "subscription-1", "principal"); err != nil {
		t.Fatalf("delete subscription: %v", err)
	}
}
