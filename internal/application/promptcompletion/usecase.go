package promptcompletion

import (
	"context"
	"errors"
	"fmt"
	"strings"

	processdomain "github.com/nzlov/anycode/internal/domain/process"
	projectdomain "github.com/nzlov/anycode/internal/domain/project"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
)

type UseCase interface {
	SlashCommands(context.Context) []SlashCommandDTO
	SearchFiles(context.Context, SearchFilesInput) ([]FileMatchDTO, error)
}

type SearchFilesInput struct {
	ProjectID projectdomain.ID
	SessionID sessiondomain.ID
	Query     string
}

type SlashCommandDTO struct {
	Name           string
	Description    string
	AcceptsArgs    bool
	RequiresThread bool
}

type FileMatchDTO struct {
	Path    string
	Score   int
	Indices []int
}

type Service struct {
	projects projectdomain.Repository
	sessions sessiondomain.Repository
	provider processdomain.CodexPromptCompletionProvider
}

func New(projects projectdomain.Repository, sessions sessiondomain.Repository, provider processdomain.CodexPromptCompletionProvider) *Service {
	return &Service{projects: projects, sessions: sessions, provider: provider}
}

func (s *Service) SlashCommands(context.Context) []SlashCommandDTO {
	if s == nil || s.provider == nil {
		return nil
	}
	commands := s.provider.SlashCommands()
	result := make([]SlashCommandDTO, 0, len(commands))
	for _, command := range commands {
		result = append(result, SlashCommandDTO{
			Name: command.Name, Description: command.Description,
			AcceptsArgs: command.AcceptsArgs, RequiresThread: command.RequiresThread,
		})
	}
	return result
}

func (s *Service) SearchFiles(ctx context.Context, input SearchFilesInput) ([]FileMatchDTO, error) {
	if s == nil || s.provider == nil {
		return nil, errors.New("prompt completion service is unavailable")
	}
	if (input.ProjectID == "") == (input.SessionID == "") {
		return nil, errors.New("exactly one project or session is required")
	}
	root := ""
	if input.SessionID != "" {
		if s.sessions == nil {
			return nil, errors.New("session repository is unavailable")
		}
		session, err := s.sessions.Find(ctx, input.SessionID)
		if err != nil {
			return nil, fmt.Errorf("find prompt completion session: %w", err)
		}
		root = strings.TrimSpace(session.WorktreePath)
	} else {
		if s.projects == nil {
			return nil, errors.New("project repository is unavailable")
		}
		project, err := s.projects.Find(ctx, input.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("find prompt completion project: %w", err)
		}
		root = strings.TrimSpace(project.Path.Value)
	}
	if root == "" {
		return nil, errors.New("prompt completion workspace is unavailable")
	}
	matches, err := s.provider.SearchFiles(ctx, root, strings.TrimSpace(input.Query))
	if err != nil {
		return nil, err
	}
	result := make([]FileMatchDTO, 0, len(matches))
	for _, match := range matches {
		indices := make([]int, len(match.Indices))
		for index, value := range match.Indices {
			indices[index] = int(value)
		}
		result = append(result, FileMatchDTO{Path: match.Path, Score: int(match.Score), Indices: indices})
	}
	return result, nil
}
