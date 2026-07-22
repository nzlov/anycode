package notification

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/nzlov/anycode/internal/application/apperror"
	eventdomain "github.com/nzlov/anycode/internal/domain/event"
	notificationdomain "github.com/nzlov/anycode/internal/domain/notification"
	projectdomain "github.com/nzlov/anycode/internal/domain/project"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
)

const (
	defaultVAPIDSubject = "https://github.com/nzlov/anycode"
	deliveryBatchSize   = 16
	deliveryTTLSeconds  = 24 * 60 * 60
	maxDeliveryAttempts = 6
	maxSummaryRunes     = 160
)

type UseCase interface {
	GetConfig(ctx context.Context) (ConfigDTO, error)
	RegisterSubscription(ctx context.Context, input RegisterSubscriptionInput) (SubscriptionDTO, error)
	UnregisterSubscription(ctx context.Context, input UnregisterSubscriptionInput) error
}

type ConfigDTO struct {
	Enabled   bool
	PublicKey string
}

type RegisterSubscriptionInput struct {
	PrincipalKeyHash string
	Endpoint         string
	P256DH           string
	Auth             string
}

type UnregisterSubscriptionInput struct {
	PrincipalKeyHash string
	ID               string
}

type SubscriptionDTO struct {
	ID string
}

type Service struct {
	repo       notificationdomain.Repository
	events     eventdomain.Store
	sessions   sessiondomain.Repository
	projects   projectdomain.Repository
	keys       notificationdomain.KeyGenerator
	sender     notificationdomain.Sender
	principal  string
	now        func() time.Time
	generateID func() (string, error)
}

func New(repo notificationdomain.Repository, events eventdomain.Store, sessions sessiondomain.Repository, projects projectdomain.Repository, keys notificationdomain.KeyGenerator, sender notificationdomain.Sender, principalKeyHash string) *Service {
	return &Service{
		repo: repo, events: events, sessions: sessions, projects: projects, keys: keys, sender: sender,
		principal: principalKeyHash, now: time.Now, generateID: generateID,
	}
}

func (s *Service) EnsureConfiguration(ctx context.Context) (notificationdomain.Configuration, error) {
	if s == nil || s.repo == nil || s.keys == nil {
		return notificationdomain.Configuration{}, errors.New("notification service is not configured")
	}
	configuration, ok, err := s.repo.GetConfiguration(ctx)
	if err != nil {
		return notificationdomain.Configuration{}, fmt.Errorf("get notification configuration: %w", err)
	}
	if ok {
		return configuration, nil
	}
	pair, err := s.keys.GenerateKeys()
	if err != nil {
		return notificationdomain.Configuration{}, fmt.Errorf("generate VAPID keys: %w", err)
	}
	configuration = notificationdomain.Configuration{
		VAPIDPublicKey: pair.PublicKey, VAPIDPrivateKey: pair.PrivateKey,
		VAPIDSubject: defaultVAPIDSubject, CreatedAt: s.now().UTC(),
	}
	configuration, err = s.repo.CreateConfiguration(ctx, configuration)
	if err != nil {
		return notificationdomain.Configuration{}, fmt.Errorf("create notification configuration: %w", err)
	}
	return configuration, nil
}

func (s *Service) GetConfig(ctx context.Context) (ConfigDTO, error) {
	configuration, err := s.EnsureConfiguration(ctx)
	if err != nil {
		return ConfigDTO{}, apperror.Wrap(err, apperror.CodeInternal, apperror.CategoryInfraError, "get web push configuration failed").WithRetryable(true)
	}
	return ConfigDTO{Enabled: true, PublicKey: configuration.VAPIDPublicKey}, nil
}

func (s *Service) RegisterSubscription(ctx context.Context, input RegisterSubscriptionInput) (SubscriptionDTO, error) {
	if s == nil || s.repo == nil {
		return SubscriptionDTO{}, errors.New("notification service is not configured")
	}
	endpoint := strings.TrimSpace(input.Endpoint)
	if !validEndpoint(endpoint) || strings.TrimSpace(input.P256DH) == "" || strings.TrimSpace(input.Auth) == "" {
		return SubscriptionDTO{}, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "push subscription is invalid")
	}
	if strings.TrimSpace(input.PrincipalKeyHash) == "" {
		return SubscriptionDTO{}, apperror.New(apperror.CodeAuthFailed, apperror.CategoryAuthError, "notification principal is required")
	}
	id, err := s.generateID()
	if err != nil {
		return SubscriptionDTO{}, apperror.Wrap(err, apperror.CodeInternal, apperror.CategoryInfraError, "generate push subscription id failed").WithRetryable(true)
	}
	now := s.now().UTC()
	sum := sha256.Sum256([]byte(endpoint))
	subscription, err := s.repo.UpsertSubscription(ctx, notificationdomain.Subscription{
		ID: id, PrincipalKeyHash: input.PrincipalKeyHash, EndpointHash: hex.EncodeToString(sum[:]),
		Endpoint: endpoint, P256DH: strings.TrimSpace(input.P256DH), Auth: strings.TrimSpace(input.Auth),
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return SubscriptionDTO{}, apperror.Wrap(err, apperror.CodeInternal, apperror.CategoryInfraError, "register push subscription failed").WithRetryable(true)
	}
	return SubscriptionDTO{ID: subscription.ID}, nil
}

func (s *Service) UnregisterSubscription(ctx context.Context, input UnregisterSubscriptionInput) error {
	if s == nil || s.repo == nil {
		return errors.New("notification service is not configured")
	}
	if strings.TrimSpace(input.ID) == "" || strings.TrimSpace(input.PrincipalKeyHash) == "" {
		return apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "push subscription id is required")
	}
	err := s.repo.DeleteSubscription(ctx, input.ID, input.PrincipalKeyHash)
	if errors.Is(err, notificationdomain.ErrSubscriptionNotFound) {
		return nil
	}
	if err != nil {
		return apperror.Wrap(err, apperror.CodeInternal, apperror.CategoryInfraError, "unregister push subscription failed").WithRetryable(true)
	}
	return nil
}

func (s *Service) Run(ctx context.Context) error {
	if s == nil || s.sender == nil {
		return errors.New("notification dispatcher is not configured")
	}
	if err := s.Initialize(ctx); err != nil {
		return err
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := s.processEvents(ctx); err != nil {
			return err
		}
		if err := s.sendDeliveries(ctx); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Service) Initialize(ctx context.Context) error {
	if s == nil || s.repo == nil || s.events == nil || strings.TrimSpace(s.principal) == "" {
		return errors.New("notification dispatcher is not configured")
	}
	if _, err := s.EnsureConfiguration(ctx); err != nil {
		return err
	}
	return s.initializeCheckpoint(ctx)
}

func (s *Service) initializeCheckpoint(ctx context.Context) error {
	_, ok, err := s.repo.GetCheckpoint(ctx)
	if err != nil || ok {
		return err
	}
	events, _, _, err := s.events.Before(ctx, eventdomain.Scope{}, "", 1)
	if err != nil {
		return fmt.Errorf("find latest event for notification checkpoint: %w", err)
	}
	latest := ""
	if len(events) > 0 {
		latest = string(events[len(events)-1].ID)
	}
	return s.repo.InitializeCheckpoint(ctx, latest, s.now().UTC())
}

func (s *Service) processEvents(ctx context.Context) error {
	checkpoint, _, err := s.repo.GetCheckpoint(ctx)
	if err != nil {
		return fmt.Errorf("get notification checkpoint: %w", err)
	}
	events, err := s.events.After(ctx, eventdomain.Scope{}, eventdomain.ID(checkpoint))
	if err != nil {
		return fmt.Errorf("list notification events: %w", err)
	}
	for _, event := range events {
		kind, ok := notificationKind(event)
		if !ok || event.SessionID == nil {
			if err := s.repo.AdvanceCheckpoint(ctx, string(event.ID), s.now().UTC()); err != nil {
				return err
			}
			continue
		}
		payload, err := s.notificationPayload(ctx, event, kind)
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encode notification payload: %w", err)
		}
		if err := s.repo.PlanEvent(ctx, string(event.ID), s.principal, event.CreatedAt, encoded, s.now().UTC()); err != nil {
			return fmt.Errorf("plan notification event: %w", err)
		}
	}
	return nil
}

func (s *Service) notificationPayload(ctx context.Context, event eventdomain.DomainEvent, kind string) (map[string]string, error) {
	sessionID := sessiondomain.ID(*event.SessionID)
	session, err := s.sessions.Find(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("find session for notification: %w", err)
	}
	projectName := "AnyCode"
	if project, projectErr := s.projects.Find(ctx, projectdomain.ID(session.ProjectID)); projectErr == nil && strings.TrimSpace(project.Name) != "" {
		projectName = strings.TrimSpace(project.Name)
	}
	title, status := notificationCopy(kind)
	return map[string]string{
		"title": title, "body": projectName + " - " + firstLine(session.Requirement),
		"sessionId": string(session.ID), "status": status,
		"path": "/#/sessions/" + url.PathEscape(string(session.ID)), "tag": "anycode-session-" + string(session.ID),
	}, nil
}

func (s *Service) sendDeliveries(ctx context.Context) error {
	configuration, _, err := s.repo.GetConfiguration(ctx)
	if err != nil {
		return err
	}
	deliveries, err := s.repo.PendingDeliveries(ctx, s.now().UTC(), deliveryBatchSize)
	if err != nil {
		return fmt.Errorf("list pending notification deliveries: %w", err)
	}
	for _, delivery := range deliveries {
		result, sendErr := s.sender.Send(ctx, configuration, delivery.Subscription, delivery.Payload, deliveryTTLSeconds)
		now := s.now().UTC()
		if sendErr == nil && result.StatusCode >= 200 && result.StatusCode < 300 {
			if err := s.repo.MarkDeliverySent(ctx, delivery.ID, now); err != nil {
				return err
			}
			continue
		}
		message := deliveryError(sendErr, result.StatusCode)
		if result.StatusCode == 404 || result.StatusCode == 410 {
			if err := s.repo.ExpireSubscription(ctx, delivery.Subscription.ID, now); err != nil {
				return err
			}
			continue
		}
		if delivery.Attempts+1 >= maxDeliveryAttempts || sendErr == nil && result.StatusCode >= 400 && result.StatusCode < 500 && result.StatusCode != 429 {
			if err := s.repo.DiscardDelivery(ctx, delivery.ID, message, now); err != nil {
				return err
			}
			continue
		}
		delay := time.Minute << min(delivery.Attempts, 5)
		if err := s.repo.RetryDelivery(ctx, delivery.ID, now.Add(delay), message, now); err != nil {
			return err
		}
	}
	return nil
}

func notificationKind(event eventdomain.DomainEvent) (string, bool) {
	switch event.Type {
	case "session.waiting_user", "session.recovery_waiting_user", "workflow.merge_waiting_user":
		return "waiting_user", true
	case "session.waiting_approval":
		return "waiting_approval", true
	case "session.completed":
		return "completed", true
	case "session.stopped":
		cause, _ := event.Payload["cause"].(string)
		if cause != "completed" {
			return "", false
		}
		return "completed", true
	case "session.failed", "workflow.failed":
		return "failed", true
	case "session.blocked":
		return "blocked", true
	case "session.resume_failed":
		return "resume_failed", true
	default:
		return "", false
	}
}

func notificationCopy(kind string) (string, string) {
	switch kind {
	case "waiting_user":
		return "AnyCode: 待回答", "waiting_user"
	case "waiting_approval":
		return "AnyCode: 待审批", "waiting_approval"
	case "completed":
		return "AnyCode: 卡片已完成", "completed"
	case "failed":
		return "AnyCode: 卡片执行失败", "failed"
	case "blocked":
		return "AnyCode: 卡片已阻塞", "blocked"
	default:
		return "AnyCode: 卡片恢复失败", "resume_failed"
	}
}

func firstLine(value string) string {
	for _, line := range strings.Split(value, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			runes := []rune(line)
			if len(runes) > maxSummaryRunes {
				return string(runes[:maxSummaryRunes-3]) + "..."
			}
			return string(runes)
		}
	}
	return "未命名卡片"
}

func validEndpoint(endpoint string) bool {
	parsed, err := url.Parse(endpoint)
	return err == nil && parsed.Scheme == "https" && parsed.Host != ""
}

func deliveryError(err error, statusCode int) string {
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("push service returned HTTP %d", statusCode)
}

func generateID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}
