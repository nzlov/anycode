package project

import (
	"context"
	"errors"
	"fmt"
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

func TestCreateProjectRestoresRemovedProjectByPath(t *testing.T) {
	ctx := context.Background()
	removedAt := time.Unix(5, 0).UTC()
	repo := newFakeRepository()
	repo.projects = []domain.Project{
		{
			ID:        "project-1",
			Name:      "Old",
			Path:      domain.ProjectPath{Value: "/workspace/anycode"},
			RemovedAt: &removedAt,
			CreatedAt: time.Unix(1, 0).UTC(),
		},
	}
	repo.reindex()
	service := New(repo, nil, &fakeGitInspector{
		states: map[string]domain.GitState{
			"/workspace/anycode": {IsRepository: true, CurrentBranch: "main"},
		},
	})
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	service.generateID = func() (domain.ID, error) {
		t.Fatal("generateID should not be called when restoring")
		return "", nil
	}

	got, err := service.CreateProject(ctx, CreateProjectInput{Path: "/workspace/anycode", Name: "AnyCode"})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	if got.ID != "project-1" || got.Name != "AnyCode" || got.RemovedAt != nil {
		t.Fatalf("restored project = %#v", got)
	}
	if repo.byID["project-1"].RemovedAt != nil {
		t.Fatalf("saved removedAt = %#v", repo.byID["project-1"].RemovedAt)
	}
}

func TestRemoveProjectHidesFromList(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.projects = []domain.Project{{ID: "project-1", Name: "AnyCode", Path: domain.ProjectPath{Value: "/repo"}}}
	repo.reindex()
	service := New(repo, nil, &fakeGitInspector{states: map[string]domain.GitState{"/repo": {IsRepository: true}}})
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }

	if err := service.RemoveProject(ctx, RemoveProjectInput{ProjectID: "project-1"}); err != nil {
		t.Fatalf("RemoveProject() error = %v", err)
	}
	got, err := service.ListProjects(ctx)
	if err != nil {
		t.Fatalf("ListProjects() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ListProjects() = %#v", got)
	}
}

func TestListProjectsDoesNotProbeGitState(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.projects = []domain.Project{
		{ID: "git", Name: "git", Path: domain.ProjectPath{Value: "/repo"}, IsGit: true},
		{ID: "plain", Name: "plain", Path: domain.ProjectPath{Value: "/plain"}, IsGit: false},
	}
	inspector := &fakeGitInspector{
		states: map[string]domain.GitState{
			"/repo":  {IsRepository: true, CurrentBranch: "main"},
			"/plain": {IsRepository: false, ErrorCode: "not_git_repository"},
		},
	}
	service := New(repo, nil, inspector)

	got, err := service.ListProjects(ctx)
	if err != nil {
		t.Fatalf("ListProjects() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListProjects() len = %d", len(got))
	}
	if inspector.detectCalls != 0 {
		t.Fatalf("Detect calls = %d, want 0", inspector.detectCalls)
	}
	if !reflect.DeepEqual(got[0].GitState, domain.GitState{}) || !reflect.DeepEqual(got[1].GitState, domain.GitState{}) {
		t.Fatalf("ListProjects() should not attach git state: %#v", got)
	}
}

func TestProjectGitStateCachesUntilRefresh(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.projects = []domain.Project{
		{ID: "project-1", Name: "git", Path: domain.ProjectPath{Value: "/repo"}, IsGit: true},
	}
	repo.reindex()
	inspector := &fakeGitInspector{
		stateSeq: map[string][]domain.GitState{
			"/repo": {
				{IsRepository: true, CurrentBranch: "main", Branches: []domain.GitBranch{{Name: "main", IsCurrent: true}}},
				{IsRepository: true, CurrentBranch: "feature", Branches: []domain.GitBranch{{Name: "feature", IsCurrent: true}}},
			},
		},
	}
	service := New(repo, nil, inspector)

	first, err := service.ProjectGitState(ctx, ProjectGitStateInput{ProjectID: "project-1"})
	if err != nil {
		t.Fatalf("ProjectGitState() first error = %v", err)
	}
	second, err := service.ProjectGitState(ctx, ProjectGitStateInput{ProjectID: "project-1"})
	if err != nil {
		t.Fatalf("ProjectGitState() second error = %v", err)
	}
	refreshed, err := service.ProjectGitState(ctx, ProjectGitStateInput{ProjectID: "project-1", Refresh: true})
	if err != nil {
		t.Fatalf("ProjectGitState() refresh error = %v", err)
	}

	if first.CurrentBranch != "main" || second.CurrentBranch != "main" || refreshed.CurrentBranch != "feature" {
		t.Fatalf("states = first:%#v second:%#v refreshed:%#v", first, second, refreshed)
	}
	if inspector.detectCalls != 2 {
		t.Fatalf("Detect calls = %d, want 2", inspector.detectCalls)
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

func TestUpdateProjectSettingsPreservesRawWorktreeInitCommand(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.projects = []domain.Project{
		{ID: "project-1", Name: "AnyCode", Path: domain.ProjectPath{Value: "/repo"}, IsGit: true, UpdatedAt: time.Unix(1, 0).UTC()},
	}
	repo.reindex()
	service := New(repo, nil, &fakeGitInspector{states: map[string]domain.GitState{"/repo": {IsRepository: true}}})
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	command := "  echo first\nprintf 'second\\n'\n\n"

	got, err := service.UpdateProjectSettings(ctx, UpdateProjectSettingsInput{
		ProjectID:           "project-1",
		WorktreeInitCommand: command,
	})
	if err != nil {
		t.Fatalf("UpdateProjectSettings() error = %v", err)
	}
	if got.WorktreeInitCommand != command {
		t.Fatalf("WorktreeInitCommand = %q, want %q", got.WorktreeInitCommand, command)
	}
	saved := repo.byID["project-1"]
	if saved.WorktreeInitCommand != command || !saved.UpdatedAt.Equal(time.Unix(10, 0).UTC()) {
		t.Fatalf("saved project = %#v", saved)
	}
}

func TestUpdateProjectSettingsSavesBlankAndClearedCommands(t *testing.T) {
	for _, command := range []string{" \n\t", ""} {
		t.Run(fmt.Sprintf("command_%q", command), func(t *testing.T) {
			repo := newFakeRepository()
			repo.projects = []domain.Project{{
				ID:                  "project-1",
				Path:                domain.ProjectPath{Value: "/repo"},
				WorktreeInitCommand: "old command",
			}}
			repo.reindex()
			service := New(repo, nil, &fakeGitInspector{states: map[string]domain.GitState{"/repo": {}}})

			got, err := service.UpdateProjectSettings(context.Background(), UpdateProjectSettingsInput{
				ProjectID:           "project-1",
				WorktreeInitCommand: command,
			})
			if err != nil {
				t.Fatalf("UpdateProjectSettings() error = %v", err)
			}
			if got.WorktreeInitCommand != command || repo.byID["project-1"].WorktreeInitCommand != command {
				t.Fatalf("saved command = DTO:%q repository:%q", got.WorktreeInitCommand, repo.byID["project-1"].WorktreeInitCommand)
			}
		})
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
	replaced := false
	for i := range r.projects {
		if r.projects[i].ID == project.ID {
			r.projects[i] = project
			replaced = true
			break
		}
	}
	if !replaced {
		r.projects = append(r.projects, project)
	}
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

func (r *fakeRepository) FindByPath(_ context.Context, path string) (domain.Project, bool, error) {
	for _, project := range r.projects {
		if project.Path.Value == path {
			return project, true, nil
		}
	}
	return domain.Project{}, false, nil
}

func (r *fakeRepository) List(_ context.Context) ([]domain.Project, error) {
	projects := []domain.Project{}
	for _, project := range r.projects {
		if project.RemovedAt == nil {
			projects = append(projects, project)
		}
	}
	return projects, nil
}

func (r *fakeRepository) Remove(_ context.Context, id domain.ID, removedAt time.Time) error {
	project, ok := r.byID[id]
	if !ok {
		return errors.New("not found")
	}
	project.RemovedAt = &removedAt
	r.byID[id] = project
	for i := range r.projects {
		if r.projects[i].ID == id {
			r.projects[i] = project
			break
		}
	}
	return nil
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
	states      map[string]domain.GitState
	stateSeq    map[string][]domain.GitState
	errs        map[string]error
	detectCalls int
}

func (g *fakeGitInspector) Detect(_ context.Context, path string) (domain.GitState, error) {
	g.detectCalls++
	if len(g.stateSeq[path]) > 0 {
		state := g.stateSeq[path][0]
		g.stateSeq[path] = g.stateSeq[path][1:]
		return state, nil
	}
	return g.states[path], g.errs[path]
}

func (g *fakeGitInspector) Branches(context.Context, string) ([]domain.GitBranch, error) {
	return nil, errors.New("unexpected Branches call")
}

func (g *fakeGitInspector) HeadCommit(context.Context, string, string) (string, error) {
	return "", errors.New("unexpected HeadCommit call")
}
