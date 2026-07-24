package tunnel

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/nzlov/anycode/internal/application/apperror"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
	domain "github.com/nzlov/anycode/internal/domain/tunnel"
)

type UseCase interface {
	Create(ctx context.Context, input CreateInput) (CreateResult, error)
	List(ctx context.Context) ([]DTO, error)
	Close(ctx context.Context, id domain.ID) error
	CloseOwned(ctx context.Context, sessionID domain.SessionID, id domain.ID) error
	CloseSession(ctx context.Context, sessionID domain.SessionID) error
}

type CreateInput struct {
	SessionID domain.SessionID
	Port      int
}

type CreateResult struct {
	Tunnel    DTO
	Auth      string
	AccessURL string
}

type DTO struct {
	ID        domain.ID
	SessionID domain.SessionID
	Port      int
	Hostname  string
	URL       string
	AccessURL string
	Status    domain.Status
	CreatedAt time.Time
}

type Service struct {
	runtime       domain.Runtime
	now           func() time.Time
	random        func(int) (string, error)
	reservedPorts map[int]struct{}
}

type Option func(*Service)

func WithReservedPorts(ports ...int) Option {
	return func(s *Service) {
		for _, port := range ports {
			if port > 0 {
				s.reservedPorts[port] = struct{}{}
			}
		}
	}
}

func New(runtime domain.Runtime, options ...Option) *Service {
	service := &Service{
		runtime:       runtime,
		now:           time.Now,
		random:        randomString,
		reservedPorts: map[int]struct{}{},
	}
	for _, option := range options {
		option(service)
	}
	return service
}

func (s *Service) Create(ctx context.Context, input CreateInput) (CreateResult, error) {
	if s == nil || s.runtime == nil {
		return CreateResult{}, errors.New("tunnel runtime is unavailable")
	}
	if input.SessionID == "" {
		return CreateResult{}, errors.New("tunnel session id is required")
	}
	if input.Port < 1024 || input.Port > 65535 {
		return CreateResult{}, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "tunnel port must be between 1024 and 65535")
	}
	if _, reserved := s.reservedPorts[input.Port]; reserved {
		return CreateResult{}, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "tunnel port is reserved")
	}
	id, err := s.random(12)
	if err != nil {
		return CreateResult{}, fmt.Errorf("generate tunnel id: %w", err)
	}
	auth, err := s.random(24)
	if err != nil {
		return CreateResult{}, fmt.Errorf("generate tunnel auth: %w", err)
	}
	now := s.now().UTC()
	started, err := s.runtime.Start(ctx, domain.StartInput{
		Tunnel: domain.Tunnel{
			ID:        domain.ID("tunnel-" + strings.ToLower(id)),
			SessionID: input.SessionID,
			Port:      input.Port,
			Status:    domain.StatusStarting,
			CreatedAt: now,
		},
		Auth: auth,
	})
	if err != nil {
		return CreateResult{}, err
	}
	return CreateResult{
		Tunnel:    toDTO(started),
		Auth:      auth,
		AccessURL: started.AccessURL,
	}, nil
}

func (s *Service) List(ctx context.Context) ([]DTO, error) {
	if s == nil || s.runtime == nil {
		return nil, errors.New("tunnel runtime is unavailable")
	}
	tunnels, err := s.runtime.List(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(tunnels, func(i, j int) bool { return tunnels[i].CreatedAt.After(tunnels[j].CreatedAt) })
	result := make([]DTO, 0, len(tunnels))
	for _, item := range tunnels {
		result = append(result, toDTO(item))
	}
	return result, nil
}

func (s *Service) Close(ctx context.Context, id domain.ID) error {
	if s == nil || s.runtime == nil {
		return errors.New("tunnel runtime is unavailable")
	}
	if id == "" {
		return errors.New("tunnel id is required")
	}
	return s.runtime.Close(ctx, id)
}

func (s *Service) CloseOwned(ctx context.Context, sessionID domain.SessionID, id domain.ID) error {
	items, err := s.List(ctx)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.ID != id {
			continue
		}
		if item.SessionID != sessionID {
			return apperror.New(apperror.CodeAuthFailed, apperror.CategoryAuthError, "tunnel does not belong to this session")
		}
		return s.Close(ctx, id)
	}
	return nil
}

func (s *Service) CloseSession(ctx context.Context, sessionID domain.SessionID) error {
	if s == nil || s.runtime == nil {
		return nil
	}
	return s.runtime.CloseSession(ctx, sessionID)
}

// GLUE: Session lifecycle cleanup crosses the session/tunnel ID boundary; remove if cleanup moves to a shared application runner.
func (s *Service) CloseTunnelsForSession(ctx context.Context, sessionID sessiondomain.ID) error {
	return s.CloseSession(ctx, domain.SessionID(sessionID))
}

func (s *Service) CloseAll(ctx context.Context) error {
	if s == nil || s.runtime == nil {
		return nil
	}
	return s.runtime.CloseAll(ctx)
}

func toDTO(item domain.Tunnel) DTO {
	return DTO{
		ID: item.ID, SessionID: item.SessionID, Port: item.Port, Hostname: item.Hostname,
		URL: item.URL, AccessURL: item.AccessURL, Status: item.Status, CreatedAt: item.CreatedAt,
	}
}

func randomString(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
