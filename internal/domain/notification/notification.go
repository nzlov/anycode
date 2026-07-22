package notification

import (
	"context"
	"errors"
	"time"
)

var ErrSubscriptionNotFound = errors.New("push subscription not found")

type Configuration struct {
	VAPIDPublicKey  string
	VAPIDPrivateKey string
	VAPIDSubject    string
	ProxyURL        string
	CreatedAt       time.Time
}

type KeyPair struct {
	PublicKey  string
	PrivateKey string
}

type Subscription struct {
	ID               string
	PrincipalKeyHash string
	EndpointHash     string
	Endpoint         string
	P256DH           string
	Auth             string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type DeliveryStatus string

const (
	DeliveryPending   DeliveryStatus = "pending"
	DeliverySent      DeliveryStatus = "sent"
	DeliveryDiscarded DeliveryStatus = "discarded"
)

type Delivery struct {
	ID           string
	EventID      string
	Subscription Subscription
	Payload      []byte
	Attempts     int
	NextAttempt  time.Time
}

type SendResult struct {
	StatusCode int
}

type Repository interface {
	GetConfiguration(ctx context.Context) (Configuration, bool, error)
	CreateConfiguration(ctx context.Context, configuration Configuration) (Configuration, error)
	SetProxyURL(ctx context.Context, proxyURL string) (Configuration, error)
	UpsertSubscription(ctx context.Context, subscription Subscription) (Subscription, error)
	DeleteSubscription(ctx context.Context, id string, principalKeyHash string) error
	GetCheckpoint(ctx context.Context) (string, bool, error)
	InitializeCheckpoint(ctx context.Context, eventID string, now time.Time) error
	AdvanceCheckpoint(ctx context.Context, eventID string, now time.Time) error
	PlanEvent(ctx context.Context, eventID string, principalKeyHash string, occurredAt time.Time, payload []byte, now time.Time) error
	PendingDeliveries(ctx context.Context, now time.Time, limit int) ([]Delivery, error)
	MarkDeliverySent(ctx context.Context, id string, now time.Time) error
	RetryDelivery(ctx context.Context, id string, nextAttempt time.Time, message string, now time.Time) error
	DiscardDelivery(ctx context.Context, id string, message string, now time.Time) error
	ExpireSubscription(ctx context.Context, subscriptionID string, now time.Time) error
}

type KeyGenerator interface {
	GenerateKeys() (KeyPair, error)
}

type Sender interface {
	Send(ctx context.Context, configuration Configuration, subscription Subscription, payload []byte, ttl int) (SendResult, error)
}
