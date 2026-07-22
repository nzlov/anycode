package promptcompletion

import (
	"context"
	"reflect"
	"strings"
	"testing"

	processdomain "github.com/nzlov/anycode/internal/domain/process"
	projectdomain "github.com/nzlov/anycode/internal/domain/project"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
)

func TestSearchFilesUsesProjectOrSessionWorkspace(t *testing.T) {
	provider := &fakeProvider{matches: []processdomain.CodexFileMatch{{
		Path: "src/main.go", Score: 91, Indices: []uint32{4, 5},
	}}}
	service := New(
		&fakeProjectRepository{project: projectdomain.Project{Path: projectdomain.ProjectPath{Value: "/projects/repo"}}},
		&fakeSessionRepository{session: sessiondomain.Session{WorktreePath: "/worktrees/session"}},
		provider,
	)

	projectMatches, err := service.SearchFiles(context.Background(), SearchFilesInput{ProjectID: "project-1", Query: " main "})
	if err != nil {
		t.Fatal(err)
	}
	if provider.root != "/projects/repo" || provider.query != "main" {
		t.Fatalf("project search = root %q, query %q", provider.root, provider.query)
	}
	if !reflect.DeepEqual(projectMatches, []FileMatchDTO{{Path: "src/main.go", Score: 91, Indices: []int{4, 5}}}) {
		t.Fatalf("project matches = %#v", projectMatches)
	}

	if _, err := service.SearchFiles(context.Background(), SearchFilesInput{SessionID: "session-1", Query: "test"}); err != nil {
		t.Fatal(err)
	}
	if provider.root != "/worktrees/session" || provider.query != "test" {
		t.Fatalf("session search = root %q, query %q", provider.root, provider.query)
	}
}

func TestSearchFilesRequiresExactlyOneScope(t *testing.T) {
	service := New(nil, nil, &fakeProvider{})
	for _, input := range []SearchFilesInput{
		{},
		{ProjectID: "project-1", SessionID: "session-1"},
	} {
		if _, err := service.SearchFiles(context.Background(), input); err == nil || !strings.Contains(err.Error(), "exactly one") {
			t.Fatalf("SearchFiles(%#v) error = %v", input, err)
		}
	}
}

func TestSlashCommandsMapsProviderMetadata(t *testing.T) {
	service := New(nil, nil, &fakeProvider{commands: []processdomain.CodexSlashCommand{{
		Name: "/review", Description: "review", AcceptsArgs: true,
	}}})
	commands := service.SlashCommands(context.Background())
	if !reflect.DeepEqual(commands, []SlashCommandDTO{{Name: "/review", Description: "review", AcceptsArgs: true}}) {
		t.Fatalf("SlashCommands() = %#v", commands)
	}
}

type fakeProvider struct {
	commands []processdomain.CodexSlashCommand
	matches  []processdomain.CodexFileMatch
	root     string
	query    string
}

func (f *fakeProvider) SlashCommands() []processdomain.CodexSlashCommand {
	return f.commands
}

func (f *fakeProvider) SearchFiles(_ context.Context, root string, query string) ([]processdomain.CodexFileMatch, error) {
	f.root = root
	f.query = query
	return f.matches, nil
}

type fakeProjectRepository struct {
	projectdomain.Repository
	project projectdomain.Project
}

func (f *fakeProjectRepository) Find(context.Context, projectdomain.ID) (projectdomain.Project, error) {
	return f.project, nil
}

type fakeSessionRepository struct {
	sessiondomain.Repository
	session sessiondomain.Session
}

func (f *fakeSessionRepository) Find(context.Context, sessiondomain.ID) (sessiondomain.Session, error) {
	return f.session, nil
}
