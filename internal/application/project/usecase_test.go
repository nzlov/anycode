package project

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	domain "github.com/nzlov/anycode/internal/domain/project"
)

func TestCreateProjectDefaultsNameAndGitState(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	inspector := &fakeGitInspector{
		states: map[string]domain.GitState{
			"/workspace/anycode": {
				IsRepository:  true,
				CurrentBranch: "main",
				Branches: []domain.GitBranch{
					{Name: "main", IsCurrent: true},
				},
			},
		},
	}
	service := New(repo, nil, inspector)
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return domain.ID("project-1"), nil }

	got, err := service.CreateProject(ctx, CreateProjectInput{Path: "/workspace/anycode"})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	if got.ID != "project-1" || got.Name != "anycode" || got.Path != "/workspace/anycode" {
		t.Fatalf("CreateProject() DTO = %#v", got)
	}
	if !got.IsGit || !got.GitState.IsRepository || got.GitState.CurrentBranch != "main" {
		t.Fatalf("CreateProject() GitState = %#v", got.GitState)
	}
	if len(repo.saved) != 1 {
		t.Fatalf("saved projects = %d", len(repo.saved))
	}
	saved := repo.saved[0]
	if saved.Name != "anycode" || !saved.IsGit || saved.CreatedAt.IsZero() || saved.UpdatedAt.IsZero() {
		t.Fatalf("saved project = %#v", saved)
	}
}

func TestCreateProjectRejectsEmptyPath(t *testing.T) {
	service := New(newFakeRepository(), nil, &fakeGitInspector{})

	_, err := service.CreateProject(context.Background(), CreateProjectInput{Path: "   "})
	if err == nil {
		t.Fatal("CreateProject() expected error for empty path")
	}
}

func TestListProjectsAddsGitStateAndKeepsDetectErrors(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.projects = []domain.Project{
		{ID: "git", Name: "git", Path: domain.ProjectPath{Value: "/repo"}, IsGit: true},
		{ID: "plain", Name: "plain", Path: domain.ProjectPath{Value: "/plain"}, IsGit: false},
		{ID: "broken", Name: "broken", Path: domain.ProjectPath{Value: "/broken"}, IsGit: true},
	}
	inspector := &fakeGitInspector{
		states: map[string]domain.GitState{
			"/repo":  {IsRepository: true, CurrentBranch: "main"},
			"/plain": {IsRepository: false, ErrorCode: "not_git_repository"},
			"/broken": {
				ErrorCode: "permission_denied",
			},
		},
		errs: map[string]error{
			"/broken": errors.New("permission denied"),
		},
	}
	service := New(repo, nil, inspector)

	got, err := service.ListProjects(ctx)
	if err != nil {
		t.Fatalf("ListProjects() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("ListProjects() len = %d", len(got))
	}
	if !got[0].GitState.IsRepository || got[0].GitState.CurrentBranch != "main" {
		t.Fatalf("git project state = %#v", got[0].GitState)
	}
	if got[1].GitState.IsRepository || got[1].GitState.ErrorCode != "not_git_repository" {
		t.Fatalf("non-git project state = %#v", got[1].GitState)
	}
	if got[2].GitState.ErrorCode != "permission_denied" || got[2].GitState.ErrorMessage != "permission denied" {
		t.Fatalf("broken project state = %#v", got[2].GitState)
	}
}

func TestBrowseDirectoryReturnsListing(t *testing.T) {
	ctx := context.Background()
	browser := &fakeDirectoryBrowser{
		listing: domain.DirectoryListing{
			Path:   "/workspace",
			Parent: "/",
			Entries: []domain.DirectoryEntry{
				{Name: "anycode", Path: "/workspace/anycode", IsDir: true, IsGit: true, CanRead: true},
			},
		},
	}
	service := New(newFakeRepository(), browser, &fakeGitInspector{})

	got, err := service.BrowseDirectory(ctx, BrowseDirectoryInput{Path: "/workspace"})
	if err != nil {
		t.Fatalf("BrowseDirectory() error = %v", err)
	}
	if browser.path != "/workspace" {
		t.Fatalf("browser path = %q", browser.path)
	}
	want := DirectoryPageDTO{
		Path:    "/workspace",
		Parent:  "/",
		Entries: browser.listing.Entries,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BrowseDirectory() = %#v, want %#v", got, want)
	}
}

func TestSetDefaultWorkflowUpdatesAndReturnsProject(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.projects = []domain.Project{
		{ID: "project-1", Name: "AnyCode", Path: domain.ProjectPath{Value: "/repo"}, IsGit: true},
	}
	repo.reindex()
	service := New(repo, nil, &fakeGitInspector{
		states: map[string]domain.GitState{
			"/repo": {IsRepository: true, CurrentBranch: "dev"},
		},
	})

	got, err := service.SetDefaultWorkflow(ctx, SetDefaultWorkflowInput{
		ProjectID:  "project-1",
		WorkflowID: "workflow-1",
	})
	if err != nil {
		t.Fatalf("SetDefaultWorkflow() error = %v", err)
	}
	if got.DefaultWorkflowID == nil || *got.DefaultWorkflowID != "workflow-1" {
		t.Fatalf("DefaultWorkflowID = %#v", got.DefaultWorkflowID)
	}
	if got.GitState.CurrentBranch != "dev" {
		t.Fatalf("GitState = %#v", got.GitState)
	}
}

type fakeRepository struct {
	saved    []domain.Project
	projects []domain.Project
	byID     map[domain.ID]domain.Project
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{byID: map[domain.ID]domain.Project{}}
}

func (r *fakeRepository) Save(_ context.Context, project domain.Project) error {
	r.saved = append(r.saved, project)
	r.projects = append(r.projects, project)
	r.byID[project.ID] = project
	return nil
}

func (r *fakeRepository) Find(_ context.Context, id domain.ID) (domain.Project, error) {
	project, ok := r.byID[id]
	if !ok {
		return domain.Project{}, errors.New("not found")
	}
	return project, nil
}

func (r *fakeRepository) List(_ context.Context) ([]domain.Project, error) {
	return append([]domain.Project(nil), r.projects...), nil
}

func (r *fakeRepository) UpdateDefaultWorkflow(_ context.Context, id domain.ID, workflowID domain.WorkflowDefinitionID) error {
	project, ok := r.byID[id]
	if !ok {
		return errors.New("not found")
	}
	project.DefaultWorkflowID = &workflowID
	r.byID[id] = project
	for i := range r.projects {
		if r.projects[i].ID == id {
			r.projects[i] = project
			break
		}
	}
	return nil
}

func (r *fakeRepository) reindex() {
	r.byID = map[domain.ID]domain.Project{}
	for _, project := range r.projects {
		r.byID[project.ID] = project
	}
}

type fakeDirectoryBrowser struct {
	path    string
	listing domain.DirectoryListing
	err     error
}

func (b *fakeDirectoryBrowser) List(_ context.Context, path string) (domain.DirectoryListing, error) {
	b.path = path
	if b.err != nil {
		return domain.DirectoryListing{}, b.err
	}
	return b.listing, nil
}

type fakeGitInspector struct {
	states map[string]domain.GitState
	errs   map[string]error
}

func (g *fakeGitInspector) Detect(_ context.Context, path string) (domain.GitState, error) {
	return g.states[path], g.errs[path]
}

func (g *fakeGitInspector) Branches(context.Context, string) ([]domain.GitBranch, error) {
	return nil, errors.New("unexpected Branches call")
}

func (g *fakeGitInspector) HeadCommit(context.Context, string, string) (string, error) {
	return "", errors.New("unexpected HeadCommit call")
}
