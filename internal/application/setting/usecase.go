package setting

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"strings"
	"time"

	"github.com/nzlov/anycode/internal/application/apperror"
	"github.com/nzlov/anycode/internal/application/port"
	domain "github.com/nzlov/anycode/internal/domain/setting"
)

type UseCase interface {
	GetAppearanceSettings(ctx context.Context) (AppearanceSettingsDTO, error)
	UpdateAppearanceSettings(ctx context.Context, input UpdateAppearanceSettingsInput) (AppearanceSettingsDTO, error)
	UploadAppearanceWallpaper(ctx context.Context, input UploadAppearanceWallpaperInput) (AppearanceSettingsDTO, error)
	OpenAppearanceWallpaper(ctx context.Context, id string) (WallpaperStream, error)
	OpenNASAWallpaper(ctx context.Context) (WallpaperStream, error)
	ListQuickCommands(ctx context.Context, input ListQuickCommandsInput) (port.Page[QuickCommandDTO], error)
	CreateQuickCommand(ctx context.Context, input CreateQuickCommandInput) (QuickCommandDTO, error)
	DeleteQuickCommand(ctx context.Context, input DeleteQuickCommandInput) error
}

type UpdateAppearanceSettingsInput struct {
	BackgroundType       domain.BackgroundType
	SolidTheme           domain.SolidTheme
	BackgroundMask       int
	WallpaperColorScheme domain.WallpaperColorScheme
}

type AppearanceSettingsDTO struct {
	BackgroundType       domain.BackgroundType
	SolidTheme           domain.SolidTheme
	BackgroundMask       int
	WallpaperColorScheme domain.WallpaperColorScheme
	WallpaperID          string
	WallpaperFilename    string
}

type UploadAppearanceWallpaperInput struct {
	Filename string
	Size     int64
	Reader   io.Reader
}

type WallpaperStream struct {
	Filename string
	MimeType string
	Reader   io.ReadCloser
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
	wallpapers domain.WallpaperStore
	nasa       domain.NASAWallpaperSource
	now        func() time.Time
	generateID func() (domain.QuickCommandID, error)
}

type Option func(*Service)

const (
	defaultPageSize  = 20
	maxPageSize      = 100
	maxWallpaperSize = 20 << 20
)

func WithWallpaperStore(store domain.WallpaperStore) Option {
	return func(service *Service) {
		service.wallpapers = store
	}
}

func WithNASAWallpaperSource(source domain.NASAWallpaperSource) Option {
	return func(service *Service) {
		service.nasa = source
	}
}

func New(repo domain.Repository, options ...Option) *Service {
	service := &Service{
		repo:       repo,
		now:        time.Now,
		generateID: generateID,
	}
	for _, option := range options {
		option(service)
	}
	return service
}

func (s *Service) GetAppearanceSettings(ctx context.Context) (AppearanceSettingsDTO, error) {
	if s == nil || s.repo == nil {
		return AppearanceSettingsDTO{}, errors.New("setting usecase: nil service")
	}
	configuration, err := s.repo.GetSystemConfiguration(ctx)
	if err != nil {
		return AppearanceSettingsDTO{}, apperror.Wrap(err, apperror.CodeInternal, apperror.CategoryInfraError, "get appearance settings failed").WithRetryable(true)
	}
	if !configuration.BackgroundType.Valid() || !configuration.SolidTheme.Valid() || !configuration.WallpaperColorScheme.Valid() || configuration.BackgroundMask < 0 || configuration.BackgroundMask > 100 || configuration.BackgroundType == domain.BackgroundTypeImage && configuration.WallpaperID == "" {
		configuration = domain.DefaultSystemConfiguration()
	}
	return appearanceDTO(configuration), nil
}

func (s *Service) UpdateAppearanceSettings(ctx context.Context, input UpdateAppearanceSettingsInput) (AppearanceSettingsDTO, error) {
	if s == nil || s.repo == nil {
		return AppearanceSettingsDTO{}, errors.New("setting usecase: nil service")
	}
	if !input.BackgroundType.Valid() {
		return AppearanceSettingsDTO{}, validationError("backgroundType", "background type is invalid")
	}
	if !input.SolidTheme.Valid() {
		return AppearanceSettingsDTO{}, validationError("solidTheme", "solid theme is invalid")
	}
	if input.BackgroundMask < 0 || input.BackgroundMask > 100 {
		return AppearanceSettingsDTO{}, validationError("backgroundMask", "background mask must be between 0 and 100")
	}
	if !input.WallpaperColorScheme.Valid() {
		return AppearanceSettingsDTO{}, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "wallpaper color scheme is invalid").
			WithDetails(map[string]any{"field": "wallpaperColorScheme"})
	}
	configuration, err := s.repo.GetSystemConfiguration(ctx)
	if err != nil {
		return AppearanceSettingsDTO{}, apperror.Wrap(err, apperror.CodeInternal, apperror.CategoryInfraError, "get appearance settings failed").WithRetryable(true)
	}
	if input.BackgroundType == domain.BackgroundTypeImage && configuration.WallpaperID == "" {
		return AppearanceSettingsDTO{}, validationError("backgroundType", "an uploaded wallpaper is required")
	}
	configuration.BackgroundType = input.BackgroundType
	configuration.SolidTheme = input.SolidTheme
	configuration.BackgroundMask = input.BackgroundMask
	configuration.WallpaperColorScheme = input.WallpaperColorScheme
	if err := s.repo.SaveSystemConfiguration(ctx, configuration); err != nil {
		return AppearanceSettingsDTO{}, apperror.Wrap(err, apperror.CodeInternal, apperror.CategoryInfraError, "update appearance settings failed").WithRetryable(true)
	}
	return appearanceDTO(configuration), nil
}

func (s *Service) UploadAppearanceWallpaper(ctx context.Context, input UploadAppearanceWallpaperInput) (AppearanceSettingsDTO, error) {
	if s == nil || s.repo == nil || s.wallpapers == nil {
		return AppearanceSettingsDTO{}, errors.New("setting usecase: wallpaper service unavailable")
	}
	if input.Reader == nil || input.Size <= 0 || input.Size > maxWallpaperSize {
		return AppearanceSettingsDTO{}, validationError("file", "wallpaper must be a non-empty image up to 20 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(input.Reader, maxWallpaperSize+1))
	if err != nil {
		return AppearanceSettingsDTO{}, apperror.Wrap(err, apperror.CodeAttachmentFailed, apperror.CategoryInfraError, "read wallpaper failed").WithRetryable(true)
	}
	if int64(len(data)) > maxWallpaperSize || int64(len(data)) != input.Size {
		return AppearanceSettingsDTO{}, validationError("file", "wallpaper size is invalid")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width) > 40_000_000/int64(config.Height) {
		return AppearanceSettingsDTO{}, validationError("file", "wallpaper image is invalid or too large")
	}
	mimeType := map[string]string{"jpeg": "image/jpeg", "png": "image/png"}[format]
	if mimeType == "" {
		return AppearanceSettingsDTO{}, validationError("file", "wallpaper must be a JPEG or PNG image")
	}
	id, err := generateStringID()
	if err != nil {
		return AppearanceSettingsDTO{}, fmt.Errorf("generate wallpaper id: %w", err)
	}
	if err := s.wallpapers.SaveWallpaper(ctx, id, bytes.NewReader(data)); err != nil {
		return AppearanceSettingsDTO{}, apperror.Wrap(err, apperror.CodeAttachmentFailed, apperror.CategoryInfraError, "save wallpaper failed").WithRetryable(true)
	}
	configuration, err := s.repo.GetSystemConfiguration(ctx)
	if err != nil {
		_ = s.wallpapers.DeleteWallpaper(ctx, id)
		return AppearanceSettingsDTO{}, apperror.Wrap(err, apperror.CodeInternal, apperror.CategoryInfraError, "get appearance settings failed").WithRetryable(true)
	}
	previousID := configuration.WallpaperID
	configuration.BackgroundType = domain.BackgroundTypeImage
	configuration.WallpaperID = id
	configuration.WallpaperFilename = strings.TrimSpace(input.Filename)
	configuration.WallpaperMimeType = mimeType
	if err := s.repo.SaveSystemConfiguration(ctx, configuration); err != nil {
		_ = s.wallpapers.DeleteWallpaper(ctx, id)
		return AppearanceSettingsDTO{}, apperror.Wrap(err, apperror.CodeInternal, apperror.CategoryInfraError, "update appearance settings failed").WithRetryable(true)
	}
	if previousID != "" && previousID != id {
		_ = s.wallpapers.DeleteWallpaper(ctx, previousID)
	}
	return appearanceDTO(configuration), nil
}

func (s *Service) OpenAppearanceWallpaper(ctx context.Context, id string) (WallpaperStream, error) {
	if s == nil || s.repo == nil || s.wallpapers == nil {
		return WallpaperStream{}, errors.New("setting usecase: wallpaper service unavailable")
	}
	configuration, err := s.repo.GetSystemConfiguration(ctx)
	if err != nil {
		return WallpaperStream{}, apperror.Wrap(err, apperror.CodeInternal, apperror.CategoryInfraError, "get appearance settings failed").WithRetryable(true)
	}
	if id == "" || id != configuration.WallpaperID {
		return WallpaperStream{}, apperror.New(apperror.CodeNotFound, apperror.CategoryValidationError, "wallpaper not found")
	}
	reader, err := s.wallpapers.OpenWallpaper(ctx, id)
	if err != nil {
		return WallpaperStream{}, apperror.Wrap(err, apperror.CodeNotFound, apperror.CategoryInfraError, "wallpaper not found")
	}
	return WallpaperStream{Filename: configuration.WallpaperFilename, MimeType: configuration.WallpaperMimeType, Reader: reader}, nil
}

func (s *Service) OpenNASAWallpaper(ctx context.Context) (WallpaperStream, error) {
	if s == nil || s.nasa == nil {
		return WallpaperStream{}, errors.New("setting usecase: NASA wallpaper service unavailable")
	}
	wallpaper, err := s.nasa.Open(ctx)
	if err != nil {
		return WallpaperStream{}, apperror.Wrap(err, apperror.CodeInternal, apperror.CategoryInfraError, "load NASA wallpaper failed").WithRetryable(true)
	}
	return WallpaperStream{Filename: "nasa-image-of-the-day", MimeType: wallpaper.MimeType, Reader: wallpaper.Reader}, nil
}

func validationError(field string, message string) error {
	return apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, message).
		WithDetails(map[string]any{"field": field})
}

func appearanceDTO(configuration domain.SystemConfiguration) AppearanceSettingsDTO {
	return AppearanceSettingsDTO{
		BackgroundType:       configuration.BackgroundType,
		SolidTheme:           configuration.SolidTheme,
		BackgroundMask:       configuration.BackgroundMask,
		WallpaperColorScheme: configuration.WallpaperColorScheme,
		WallpaperID:          configuration.WallpaperID,
		WallpaperFilename:    configuration.WallpaperFilename,
	}
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
	id, err := generateStringID()
	return domain.QuickCommandID(id), err
}

func generateStringID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}
