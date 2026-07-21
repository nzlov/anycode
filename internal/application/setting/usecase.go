package setting

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/nzlov/anycode/internal/application/apperror"
	"github.com/nzlov/anycode/internal/application/port"
	domain "github.com/nzlov/anycode/internal/domain/setting"
)

type UseCase interface {
	GetAppearanceSettings(ctx context.Context) (AppearanceSettingsDTO, error)
	UpdateAppearanceSettings(ctx context.Context, input UpdateAppearanceSettingsInput) (AppearanceSettingsDTO, error)
	ListQuickCommands(ctx context.Context, input ListQuickCommandsInput) (port.Page[QuickCommandDTO], error)
	CreateQuickCommand(ctx context.Context, input CreateQuickCommandInput) (QuickCommandDTO, error)
	DeleteQuickCommand(ctx context.Context, input DeleteQuickCommandInput) error
}

type UpdateAppearanceSettingsInput struct {
	WallpaperColorScheme domain.WallpaperColorScheme
}

type AppearanceSettingsDTO struct {
	WallpaperColorScheme domain.WallpaperColorScheme
}

type ListQuickCommandsInput struct {
	Page     int
	PageSize int
}

type CreateQuickCommandInput struct {
	Content string
}

type DeleteQuickCommandInput struct {
	ID domain.QuickCommandID
}

type QuickCommandDTO struct {
	ID        domain.QuickCommandID
	Content   string
	CreatedAt time.Time
}

type Service struct {
	repo       domain.Repository
	now        func() time.Time
	generateID func() (domain.QuickCommandID, error)
}

const (
	defaultPageSize = 20
	maxPageSize     = 100
)

func New(repo domain.Repository) *Service {
	return &Service{
		repo:       repo,
		now:        time.Now,
		generateID: generateID,
	}
}

func (s *Service) GetAppearanceSettings(ctx context.Context) (AppearanceSettingsDTO, error) {
	if s == nil || s.repo == nil {
		return AppearanceSettingsDTO{}, errors.New("setting usecase: nil service")
	}
	configuration, err := s.repo.GetSystemConfiguration(ctx)
	if err != nil {
		return AppearanceSettingsDTO{}, apperror.Wrap(err, apperror.CodeInternal, apperror.CategoryInfraError, "get appearance settings failed").WithRetryable(true)
	}
	if !configuration.WallpaperColorScheme.Valid() {
		configuration = domain.DefaultSystemConfiguration()
	}
	return AppearanceSettingsDTO{WallpaperColorScheme: configuration.WallpaperColorScheme}, nil
}

func (s *Service) UpdateAppearanceSettings(ctx context.Context, input UpdateAppearanceSettingsInput) (AppearanceSettingsDTO, error) {
	if s == nil || s.repo == nil {
		return AppearanceSettingsDTO{}, errors.New("setting usecase: nil service")
	}
	if !input.WallpaperColorScheme.Valid() {
		return AppearanceSettingsDTO{}, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "wallpaper color scheme is invalid").
			WithDetails(map[string]any{"field": "wallpaperColorScheme"})
	}
	configuration := domain.SystemConfiguration{WallpaperColorScheme: input.WallpaperColorScheme}
	if err := s.repo.SaveSystemConfiguration(ctx, configuration); err != nil {
		return AppearanceSettingsDTO{}, apperror.Wrap(err, apperror.CodeInternal, apperror.CategoryInfraError, "update appearance settings failed").WithRetryable(true)
	}
	return AppearanceSettingsDTO{WallpaperColorScheme: configuration.WallpaperColorScheme}, nil
}

func (s *Service) ListQuickCommands(ctx context.Context, input ListQuickCommandsInput) (port.Page[QuickCommandDTO], error) {
	if s == nil || s.repo == nil {
		return port.Page[QuickCommandDTO]{}, errors.New("setting usecase: nil service")
	}
	page, pageSize := normalizePage(input.Page, input.PageSize)
	result, err := s.repo.List(ctx, domain.QuickCommandQuery{Page: page, PageSize: pageSize})
	if err != nil {
		return port.Page[QuickCommandDTO]{}, apperror.Wrap(err, apperror.CodeInternal, apperror.CategoryInfraError, "list quick commands failed").WithRetryable(true)
	}
	dtos := make([]QuickCommandDTO, 0, len(result.Items))
	for _, command := range result.Items {
		dtos = append(dtos, toDTO(command))
	}
	return port.Page[QuickCommandDTO]{
		Items:    dtos,
		Page:     result.Page,
		PageSize: result.PageSize,
		Total:    result.Total,
	}, nil
}

func (s *Service) CreateQuickCommand(ctx context.Context, input CreateQuickCommandInput) (QuickCommandDTO, error) {
	if s == nil || s.repo == nil {
		return QuickCommandDTO{}, errors.New("setting usecase: nil service")
	}
	content := strings.TrimSpace(input.Content)
	if content == "" {
		return QuickCommandDTO{}, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "quick command content is required").
			WithDetails(map[string]any{"field": "content"})
	}
	id, err := s.generateID()
	if err != nil {
		return QuickCommandDTO{}, apperror.Wrap(err, apperror.CodeInternal, apperror.CategoryInfraError, "generate quick command id failed").WithRetryable(true)
	}
	command := domain.QuickCommand{
		ID:        id,
		Content:   content,
		CreatedAt: s.now(),
	}
	if err := s.repo.Create(ctx, command); err != nil {
		return QuickCommandDTO{}, apperror.Wrap(err, apperror.CodeInternal, apperror.CategoryInfraError, "create quick command failed").WithRetryable(true)
	}
	return toDTO(command), nil
}

func (s *Service) DeleteQuickCommand(ctx context.Context, input DeleteQuickCommandInput) error {
	if s == nil || s.repo == nil {
		return errors.New("setting usecase: nil service")
	}
	if input.ID == "" {
		return apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "quick command id is required").
			WithDetails(map[string]any{"field": "id"})
	}
	if err := s.repo.Delete(ctx, input.ID); err != nil {
		if errors.Is(err, domain.ErrQuickCommandNotFound) {
			return apperror.Wrap(err, apperror.CodeNotFound, apperror.CategoryValidationError, "quick command not found").
				WithDetails(map[string]any{"quickCommandId": string(input.ID)})
		}
		return apperror.Wrap(err, apperror.CodeInternal, apperror.CategoryInfraError, "delete quick command failed").WithRetryable(true)
	}
	return nil
}

func normalizePage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	return page, pageSize
}

func toDTO(command domain.QuickCommand) QuickCommandDTO {
	return QuickCommandDTO{
		ID:        command.ID,
		Content:   command.Content,
		CreatedAt: command.CreatedAt,
	}
}

func generateID() (domain.QuickCommandID, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return domain.QuickCommandID(hex.EncodeToString(value[:])), nil
}
