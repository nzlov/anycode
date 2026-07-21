package notification

import (
	"context"
	"strings"
	"testing"
	"time"

	eventdomain "github.com/nzlov/anycode/internal/domain/event"
	notificationdomain "github.com/nzlov/anycode/internal/domain/notification"
)

func TestEnsureConfigurationGeneratesAndReusesVAPIDKeys(t *testing.T) {
	repo := &fakeNotificationRepository{}
	keys := &fakeKeyGenerator{pair: notificationdomain.KeyPair{PublicKey: "public", PrivateKey: "private"}}
	service := New(repo, nil, nil, nil, keys, nil, "principal")
	service.now = func() time.Time { return time.Unix(100, 0).UTC() }

	first, err := service.EnsureConfiguration(context.Background())
	if err != nil {
		t.Fatalf("EnsureConfiguration() error = %v", err)
	}
	second, err := service.EnsureConfiguration(context.Background())
	if err != nil {
		t.Fatalf("EnsureConfiguration() second error = %v", err)
	}
	if first != second || first.VAPIDPublicKey != "public" || first.VAPIDPrivateKey != "private" {
		t.Fatalf("configurations = %#v, %#v", first, second)
	}
	if keys.calls != 1 || repo.createCalls != 1 {
		t.Fatalf("key generation calls = %d, create calls = %d", keys.calls, repo.createCalls)
	}
}

func TestInitializeStartsCheckpointAtLatestExistingEvent(t *testing.T) {
	repo := &fakeNotificationRepository{
		configuration: notificationdomain.Configuration{VAPIDPublicKey: "public", VAPIDPrivateKey: "private"},
		hasConfig:     true,
	}
	events := &fakeEventStore{latest: eventdomain.DomainEvent{ID: "event-2"}}
	service := New(repo, events, nil, nil, &fakeKeyGenerator{}, nil, "principal")
	service.now = func() time.Time { return time.Unix(100, 0).UTC() }

	if err := service.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if !repo.hasCheckpoint || repo.checkpoint != "event-2" {
		t.Fatalf("checkpoint = %q, exists = %t", repo.checkpoint, repo.hasCheckpoint)
	}
}

func TestRegisterSubscriptionValidatesAndHashesEndpoint(t *testing.T) {
	repo := &fakeNotificationRepository{}
	service := New(repo, nil, nil, nil, nil, nil, "principal")
	service.now = func() time.Time { return time.Unix(100, 0).UTC() }
	service.generateID = func() (string, error) { return "subscription-1", nil }

	got, err := service.RegisterSubscription(context.Background(), RegisterSubscriptionInput{
		PrincipalKeyHash: "principal", Endpoint: " https://push.example/subscription ", P256DH: " key ", Auth: " auth ",
	})
	if err != nil {
		t.Fatalf("RegisterSubscription() error = %v", err)
	}
	if got.ID != "subscription-1" || repo.subscription.Endpoint != "https://push.example/subscription" || repo.subscription.EndpointHash == "" {
		t.Fatalf("subscription = %#v, saved = %#v", got, repo.subscription)
	}
	if _, err := service.RegisterSubscription(context.Background(), RegisterSubscriptionInput{
		PrincipalKeyHash: "principal", Endpoint: "http://push.example/subscription", P256DH: "key", Auth: "auth",
	}); err == nil {
		t.Fatal("RegisterSubscription() accepted an insecure endpoint")
	}
}

func TestFirstLineLimitsNotificationPayload(t *testing.T) {
	got := firstLine("  " + strings.Repeat("字", maxSummaryRunes+20))
	if len([]rune(got)) != maxSummaryRunes || got[len(got)-3:] != "..." {
		t.Fatalf("firstLine() length = %d, value = %q", len([]rune(got)), got)
	}
}

func TestNotificationKindDistinguishesCompletionFromRequestedStop(t *testing.T) {
	tests := []struct {
		name     string
		event    eventdomain.DomainEvent
		wantKind string
		want     bool
	}{
		{name: "workflow completed", event: eventdomain.DomainEvent{Type: "session.completed"}, wantKind: "completed", want: true},
		{name: "chat completed", event: eventdomain.DomainEvent{Type: "session.stopped", Payload: map[string]any{"cause": "completed"}}, wantKind: "completed", want: true},
		{name: "requested stop", event: eventdomain.DomainEvent{Type: "session.stopped", Payload: map[string]any{"cause": "stop_requested"}}},
		{name: "waiting for answer", event: eventdomain.DomainEvent{Type: "session.waiting_user"}, wantKind: "waiting_user", want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			kind, ok := notificationKind(test.event)
			if kind != test.wantKind || ok != test.want {
				t.Fatalf("notificationKind() = %q, %t", kind, ok)
			}
		})
	}
}

type fakeNotificationRepository struct {
	notificationdomain.Repository
	configuration notificationdomain.Configuration
	hasConfig     bool
	createCalls   int
	subscription  notificationdomain.Subscription
	checkpoint    string
	hasCheckpoint bool
}

func (f *fakeNotificationRepository) GetConfiguration(context.Context) (notificationdomain.Configuration, bool, error) {
	return f.configuration, f.hasConfig, nil
}

func (f *fakeNotificationRepository) CreateConfiguration(_ context.Context, configuration notificationdomain.Configuration) (notificationdomain.Configuration, error) {
	f.configuration = configuration
	f.hasConfig = true
	f.createCalls++
	return configuration, nil
}

func (f *fakeNotificationRepository) UpsertSubscription(_ context.Context, subscription notificationdomain.Subscription) (notificationdomain.Subscription, error) {
	f.subscription = subscription
	return subscription, nil
}

func (f *fakeNotificationRepository) GetCheckpoint(context.Context) (string, bool, error) {
	return f.checkpoint, f.hasCheckpoint, nil
}

func (f *fakeNotificationRepository) InitializeCheckpoint(_ context.Context, eventID string, _ time.Time) error {
	f.checkpoint = eventID
	f.hasCheckpoint = true
	return nil
}

type fakeEventStore struct {
	eventdomain.Store
	latest eventdomain.DomainEvent
}

func (f *fakeEventStore) Before(context.Context, eventdomain.Scope, eventdomain.ID, int) ([]eventdomain.DomainEvent, int, bool, error) {
	return []eventdomain.DomainEvent{f.latest}, 1, false, nil
}

type fakeKeyGenerator struct {
	pair  notificationdomain.KeyPair
	calls int
}

func (f *fakeKeyGenerator) GenerateKeys() (notificationdomain.KeyPair, error) {
	f.calls++
	return f.pair, nil
}
