package project

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	domain "github.com/nzlov/anycode/internal/domain/project"
)

type UseCase interface {
	CreateProject(ctx context.Context, input CreateProjectInput) (DTO, error)
	BrowseDirectory(ctx context.Context, input BrowseDirectoryInput) (DirectoryPageDTO, error)
	SetDefaultWorkflow(ctx context.Context, input SetDefaultWorkflowInput) (DTO, error)
	ListProjects(ctx context.Context) ([]DTO, error)
}

type CreateProjectInput struct {
	Path string
	Name string
}

type BrowseDirectoryInput struct {
	Path string
}

type SetDefaultWorkflowInput struct {
	ProjectID  domain.ID
	WorkflowID domain.WorkflowDefinitionID
}

type DTO struct {
	ID                domain.ID
	Name              string
	Path              string
	IsGit             bool
	DefaultWorkflowID *domain.WorkflowDefinitionID
	GitState          domain.GitState
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type DirectoryPageDTO struct {
	Path    string
	Parent  string
	Entries []domain.DirectoryEntry
}

type Service struct {
	repo       domain.Repository
	browser    domain.DirectoryBrowser
	inspector  domain.GitInspector
	now        func() time.Time
	generateID func() (domain.ID, error)
}

func New(repo domain.Repository, browser domain.DirectoryBrowser, inspector domain.GitInspector) *Service {
	return &Service{
		repo:       repo,
		browser:    browser,
		inspector:  inspector,
		now:        time.Now,
		generateID: generateID,
	}
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
	id, err := s.generateID()
	if err != nil {
		return DTO{}, fmt.Errorf("generate project id: %w", err)
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = filepath.Base(filepath.Clean(projectPath))
	}
	now := s.now()
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
	return toDTO(project, gitState), nil
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
		dtos = append(dtos, toDTO(project, s.gitState(ctx, project.Path.Value)))
	}
	return dtos, nil
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

func toDTO(project domain.Project, gitState domain.GitState) DTO {
	return DTO{
		ID:                project.ID,
		Name:              project.Name,
		Path:              project.Path.Value,
		IsGit:             project.IsGit,
		DefaultWorkflowID: project.DefaultWorkflowID,
		GitState:          gitState,
		CreatedAt:         project.CreatedAt,
		UpdatedAt:         project.UpdatedAt,
	}
}

func generateID() (domain.ID, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return domain.ID(hex.EncodeToString(b[:])), nil
}
