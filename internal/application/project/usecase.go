package project

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nzlov/anycode/internal/application/apperror"
	domain "github.com/nzlov/anycode/internal/domain/project"
	settingdomain "github.com/nzlov/anycode/internal/domain/setting"
)

type UseCase interface {
	CreateProject(ctx context.Context, input CreateProjectInput) (DTO, error)
	CloneProject(ctx context.Context, input CloneProjectInput) (DTO, error)
	BrowseDirectory(ctx context.Context, input BrowseDirectoryInput) (DirectoryPageDTO, error)
	UpdateProjectSettings(ctx context.Context, input UpdateProjectSettingsInput) (DTO, error)
	SetDefaultWorkflow(ctx context.Context, input SetDefaultWorkflowInput) (DTO, error)
	RemoveProject(ctx context.Context, input RemoveProjectInput) error
	ListProjects(ctx context.Context) ([]DTO, error)
	ProjectGitState(ctx context.Context, input ProjectGitStateInput) (domain.GitState, error)
}

type CreateProjectInput struct {
	Path string
	Name string
}

type CloneProjectInput struct {
	ParentPath    string
	RepositoryURL string
}

type BrowseDirectoryInput struct {
	Path string
}

type SetDefaultWorkflowInput struct {
	ProjectID  domain.ID
	WorkflowID domain.WorkflowDefinitionID
}

type UpdateProjectSettingsInput struct {
	ProjectID           domain.ID
	WorktreeInitCommand string
	MindMapEnabled      bool
}

type RemoveProjectInput struct {
	ProjectID domain.ID
}

type ProjectGitStateInput struct {
	ProjectID domain.ID
	Refresh   bool
}

type DTO struct {
	ID                  domain.ID
	Name                string
	Path                string
	IsGit               bool
	WorktreeInitCommand string
	MindMapEnabled      bool
	DefaultWorkflowID   *domain.WorkflowDefinitionID
	RemovedAt           *time.Time
	GitState            domain.GitState
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type DirectoryPageDTO struct {
	Path    string
	Parent  string
	Entries []domain.DirectoryEntry
}

type Service struct {
	repo            domain.Repository
	browser         domain.DirectoryBrowser
	inspector       domain.GitInspector
	cloner          domain.RepositoryCloner
	gitCacheMu      sync.Mutex
	gitCache        map[domain.ID]domain.GitState
	now             func() time.Time
	generateID      func() (domain.ID, error)
	mindMapSettings settingdomain.MindMapConfigurationProvider
}

type Option func(*Service)

func WithMindMapSettings(provider settingdomain.MindMapConfigurationProvider) Option {
	return func(service *Service) { service.mindMapSettings = provider }
}

func WithRepositoryCloner(cloner domain.RepositoryCloner) Option {
	return func(service *Service) { service.cloner = cloner }
}

func New(repo domain.Repository, browser domain.DirectoryBrowser, inspector domain.GitInspector, options ...Option) *Service {
	service := &Service{
		repo:       repo,
		browser:    browser,
		inspector:  inspector,
		gitCache:   map[domain.ID]domain.GitState{},
		now:        time.Now,
		generateID: generateID,
	}
	for _, option := range options {
		option(service)
	}
	return service
}

func (s *Service) CreateProject(ctx context.Context, input CreateProjectInput) (DTO, error) {
	if s == nil {
		return DTO{}, errors.New("project usecase: nil service")
	}
	projectPath := strings.TrimSpace(input.Path)
	if projectPath == "" {
		return DTO{}, errors.New("project path is required")
	}
	gitState, err := s.inspector.Detect(ctx, projectPath)
	if err != nil {
		return DTO{}, fmt.Errorf("detect project git state: %w", err)
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = filepath.Base(filepath.Clean(projectPath))
	}
	now := s.now()
	existing, ok, err := s.repo.FindByPath(ctx, projectPath)
	if err != nil {
		return DTO{}, fmt.Errorf("find project by path: %w", err)
	}
	if ok {
		existing.Name = name
		existing.IsGit = gitState.IsRepository
		existing.RemovedAt = nil
		existing.UpdatedAt = now
		if err := s.repo.Save(ctx, existing); err != nil {
			return DTO{}, fmt.Errorf("restore project: %w", err)
		}
		s.cacheGitState(existing.ID, gitState)
		return toDTO(existing, gitState), nil
	}
	id, err := s.generateID()
	if err != nil {
		return DTO{}, fmt.Errorf("generate project id: %w", err)
	}
	project := domain.Project{
		ID:        id,
		Name:      name,
		Path:      domain.ProjectPath{Value: projectPath},
		IsGit:     gitState.IsRepository,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.repo.Save(ctx, project); err != nil {
		return DTO{}, fmt.Errorf("save project: %w", err)
	}
	s.cacheGitState(project.ID, gitState)
	return toDTO(project, gitState), nil
}

func (s *Service) CloneProject(ctx context.Context, input CloneProjectInput) (DTO, error) {
	if s == nil {
		return DTO{}, errors.New("project usecase: nil service")
	}
	parentPath := strings.TrimSpace(input.ParentPath)
	repositoryURL := strings.TrimSpace(input.RepositoryURL)
	if parentPath == "" || repositoryURL == "" {
		return DTO{}, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "本地父目录和项目地址不能为空")
	}
	if s.cloner == nil {
		return DTO{}, apperror.New(apperror.CodeInternal, apperror.CategoryInfraError, "远程项目克隆服务不可用").WithRetryable(true)
	}
	projectPath, err := s.cloner.Clone(ctx, parentPath, repositoryURL)
	if err != nil {
		return DTO{}, apperror.Wrap(err, apperror.CodeProjectCloneFailed, apperror.CategoryInfraError, "克隆远程项目失败，请检查项目地址、访问权限和本地父目录").WithRetryable(true)
	}
	return s.CreateProject(ctx, CreateProjectInput{Path: projectPath})
}

func (s *Service) BrowseDirectory(ctx context.Context, input BrowseDirectoryInput) (DirectoryPageDTO, error) {
	if s == nil {
		return DirectoryPageDTO{}, errors.New("project usecase: nil service")
	}
	listing, err := s.browser.List(ctx, input.Path)
	if err != nil {
		return DirectoryPageDTO{}, fmt.Errorf("browse directory: %w", err)
	}
	return DirectoryPageDTO{
		Path:    listing.Path,
		Parent:  listing.Parent,
		Entries: listing.Entries,
	}, nil
}

func (s *Service) UpdateProjectSettings(ctx context.Context, input UpdateProjectSettingsInput) (DTO, error) {
	if s == nil {
		return DTO{}, errors.New("project usecase: nil service")
	}
	if input.ProjectID == "" {
		return DTO{}, errors.New("project id is required")
	}
	project, err := s.repo.Find(ctx, input.ProjectID)
	if err != nil {
		return DTO{}, fmt.Errorf("find project: %w", err)
	}
	if input.MindMapEnabled && !project.MindMapEnabled {
		if s.mindMapSettings == nil {
			return DTO{}, errors.New("mind map settings are unavailable")
		}
		configuration, err := s.mindMapSettings.MindMapConfiguration(ctx)
		if err != nil {
			return DTO{}, fmt.Errorf("get mind map settings: %w", err)
		}
		if !configuration.Enabled || !configuration.Mode.Valid() || configuration.Mode == settingdomain.MindMapModeAsync && (strings.TrimSpace(configuration.Model) == "" || strings.TrimSpace(configuration.ReasoningEffort) == "") {
			return DTO{}, errors.New("global mind map settings must be enabled and complete")
		}
	}
	project.WorktreeInitCommand = input.WorktreeInitCommand
	project.MindMapEnabled = input.MindMapEnabled
	project.UpdatedAt = s.now()
	if err := s.repo.Save(ctx, project); err != nil {
		return DTO{}, fmt.Errorf("save project settings: %w", err)
	}
	return toDTO(project, s.gitState(ctx, project.Path.Value)), nil
}

func (s *Service) SetDefaultWorkflow(ctx context.Context, input SetDefaultWorkflowInput) (DTO, error) {
	if s == nil {
		return DTO{}, errors.New("project usecase: nil service")
	}
	if err := s.repo.UpdateDefaultWorkflow(ctx, input.ProjectID, input.WorkflowID); err != nil {
		return DTO{}, fmt.Errorf("set project default workflow: %w", err)
	}
	project, err := s.repo.Find(ctx, input.ProjectID)
	if err != nil {
		return DTO{}, fmt.Errorf("find project: %w", err)
	}
	return toDTO(project, s.gitState(ctx, project.Path.Value)), nil
}

func (s *Service) RemoveProject(ctx context.Context, input RemoveProjectInput) error {
	if s == nil {
		return errors.New("project usecase: nil service")
	}
	if input.ProjectID == "" {
		return errors.New("project id is required")
	}
	if err := s.repo.Remove(ctx, input.ProjectID, s.now()); err != nil {
		return fmt.Errorf("remove project: %w", err)
	}
	return nil
}

func (s *Service) ListProjects(ctx context.Context) ([]DTO, error) {
	if s == nil {
		return nil, errors.New("project usecase: nil service")
	}
	projects, err := s.repo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	dtos := make([]DTO, 0, len(projects))
	for _, project := range projects {
		dtos = append(dtos, toDTO(project, domain.GitState{}))
	}
	return dtos, nil
}

func (s *Service) ProjectGitState(ctx context.Context, input ProjectGitStateInput) (domain.GitState, error) {
	if s == nil {
		return domain.GitState{}, errors.New("project usecase: nil service")
	}
	if input.ProjectID == "" {
		return domain.GitState{}, errors.New("project id is required")
	}
	if !input.Refresh {
		if state, ok := s.cachedGitState(input.ProjectID); ok {
			return state, nil
		}
	}
	project, err := s.repo.Find(ctx, input.ProjectID)
	if err != nil {
		return domain.GitState{}, fmt.Errorf("find project: %w", err)
	}
	state := s.gitState(ctx, project.Path.Value)
	s.cacheGitState(project.ID, state)
	return state, nil
}

func (s *Service) gitState(ctx context.Context, path string) domain.GitState {
	state, err := s.inspector.Detect(ctx, path)
	if err == nil {
		return state
	}
	if state.ErrorCode == "" {
		state.ErrorCode = "git_detect_failed"
	}
	if state.ErrorMessage == "" {
		state.ErrorMessage = err.Error()
	}
	return state
}

func (s *Service) cachedGitState(projectID domain.ID) (domain.GitState, bool) {
	s.gitCacheMu.Lock()
	defer s.gitCacheMu.Unlock()
	state, ok := s.gitCache[projectID]
	return state, ok
}

func (s *Service) cacheGitState(projectID domain.ID, state domain.GitState) {
	s.gitCacheMu.Lock()
	defer s.gitCacheMu.Unlock()
	s.gitCache[projectID] = state
}

func toDTO(project domain.Project, gitState domain.GitState) DTO {
	return DTO{
		ID:                  project.ID,
		Name:                project.Name,
		Path:                project.Path.Value,
		IsGit:               project.IsGit,
		WorktreeInitCommand: project.WorktreeInitCommand,
		MindMapEnabled:      project.MindMapEnabled,
		DefaultWorkflowID:   project.DefaultWorkflowID,
		RemovedAt:           project.RemovedAt,
		GitState:            gitState,
		CreatedAt:           project.CreatedAt,
		UpdatedAt:           project.UpdatedAt,
	}
}

func generateID() (domain.ID, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return domain.ID(hex.EncodeToString(b[:])), nil
}
