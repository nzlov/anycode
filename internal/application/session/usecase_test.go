package session

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	projectdomain "github.com/nzlov/anycode/internal/domain/project"
	domain "github.com/nzlov/anycode/internal/domain/session"
)

func TestCreateSessionDefaultsModeAndSavesRequestedConfig(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := New(repo, newFakeProjectRepository("project-1"))
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "session-1", nil }

	got, err := service.CreateSession(ctx, CreateSessionInput{
		ProjectID:   "project-1",
		Requirement: "  implement app session  ",
		Config: domain.Config{
			CodexModel:      "gpt-5.4-mini",
			ReasoningEffort: "medium",
			PermissionMode:  "workspace-write",
		},
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if got.ID != "session-1" || got.ProjectID != "project-1" || got.Requirement != "implement app session" {
		t.Fatalf("CreateSession() DTO = %#v", got)
	}
	if got.Mode != domain.ModeChat || got.Status != domain.StatusCreated {
		t.Fatalf("CreateSession() mode/status = %q/%q", got.Mode, got.Status)
	}
	if len(repo.saved) != 1 {
		t.Fatalf("saved sessions = %d", len(repo.saved))
	}
	saved := repo.saved[0]
	if saved.Status != domain.StatusCreated || saved.Mode != domain.ModeChat {
		t.Fatalf("saved session status/mode = %q/%q", saved.Status, saved.Mode)
	}
	if !reflect.DeepEqual(saved.Config, got.Config) {
		t.Fatalf("saved config = %#v, want %#v", saved.Config, got.Config)
	}
	if saved.LastRunAt != nil || saved.CodexSessionID != "" || saved.WorktreePath != "" {
		t.Fatalf("CreateSession() should not start codex or create runtime fields: %#v", saved)
	}
}

func TestCreateSessionValidatesProjectAndRequirement(t *testing.T) {
	service := New(newFakeRepository(), newFakeProjectRepository("project-1"))

	if _, err := service.CreateSession(context.Background(), CreateSessionInput{Requirement: "body"}); err == nil {
		t.Fatal("CreateSession() expected project id error")
	}
	if _, err := service.CreateSession(context.Background(), CreateSessionInput{ProjectID: "project-1", Requirement: "   "}); err == nil {
		t.Fatal("CreateSession() expected requirement error")
	}
	if _, err := service.CreateSession(context.Background(), CreateSessionInput{ProjectID: "missing", Requirement: "body"}); err == nil {
		t.Fatal("CreateSession() expected missing project error")
	}
}

func TestCreateSessionArchivesStagedAttachments(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.stagedAttachments["staged-1"] = domain.StagedAttachment{
		ID:        "staged-1",
		Filename:  "note.txt",
		Path:      "/attachments/staged/staged-1/note.txt",
		MimeType:  "text/plain",
		Size:      5,
		CreatedAt: time.Unix(9, 0).UTC(),
	}
	files := newFakeAttachmentStore()
	service := New(repo, newFakeProjectRepository("project-1"), WithAttachments(repo, files))
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "session-1", nil }

	if _, err := service.CreateSession(ctx, CreateSessionInput{
		ProjectID:           "project-1",
		Requirement:         "use attachment",
		StagedAttachmentIDs: []domain.StagedAttachmentID{"staged-1"},
	}); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if _, ok := repo.stagedAttachments["staged-1"]; ok {
		t.Fatal("staged attachment metadata was not deleted")
	}
	attachment, ok := repo.sessionAttachments["staged-1"]
	if !ok {
		t.Fatal("session attachment metadata was not saved")
	}
	if attachment.SessionID != "session-1" || attachment.Filename != "note.txt" {
		t.Fatalf("session attachment = %#v", attachment)
	}
	if !files.promoted["staged-1"] {
		t.Fatal("staged attachment file was not promoted")
	}
}

func TestCreateSessionMarksFailedWhenAttachmentArchiveFails(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.stagedAttachments["staged-1"] = domain.StagedAttachment{
		ID:       "staged-1",
		Filename: "note.txt",
	}
	files := newFakeAttachmentStore()
	files.promoteErr = errors.New("disk failed")
	service := New(repo, newFakeProjectRepository("project-1"), WithAttachments(repo, files))
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "session-1", nil }

	if _, err := service.CreateSession(ctx, CreateSessionInput{
		ProjectID:           "project-1",
		Requirement:         "use attachment",
		StagedAttachmentIDs: []domain.StagedAttachmentID{"staged-1"},
	}); err == nil {
		t.Fatal("CreateSession() expected attachment archive error")
	}
	got, err := repo.Find(ctx, "session-1")
	if err != nil {
		t.Fatalf("Find() session after archive failure: %v", err)
	}
	if got.Status != domain.StatusFailed {
		t.Fatalf("session status after archive failure = %q", got.Status)
	}
}

func TestListSessionsReturnsCardsPage(t *testing.T) {
	ctx := context.Background()
	projectID := domain.ProjectID("project-1")
	repo := newFakeRepository()
	repo.listSessions = []domain.Session{
		{ID: "created", ProjectID: projectID, Requirement: "created card", Status: domain.StatusCreated},
		{ID: "running", ProjectID: projectID, Requirement: "running card", Status: domain.StatusRunning},
	}
	repo.listTotal = 7
	repo.sessionAttachments["attachment-1"] = domain.SessionAttachment{
		ID:        "attachment-1",
		SessionID: "created",
		Filename:  "note.txt",
	}
	service := New(repo, newFakeProjectRepository("project-1"), WithAttachments(repo, newFakeAttachmentStore()))

	got, err := service.ListSessions(ctx, ListSessionsInput{
		ProjectID: &projectID,
		Filter:    "card",
		Sort:      "updated_desc",
	})
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if got.Page != 1 || got.PageSize != 20 || got.Total != 7 {
		t.Fatalf("ListSessions() page = %#v", got)
	}
	if repo.lastListQuery.ProjectID == nil || *repo.lastListQuery.ProjectID != projectID {
		t.Fatalf("ListSessions() query project = %#v", repo.lastListQuery.ProjectID)
	}
	if got.Items[0].RequirementSummary != "created card" {
		t.Fatalf("RequirementSummary = %q", got.Items[0].RequirementSummary)
	}
	if len(got.Items[0].Attachments) != 1 || got.Items[0].Attachments[0].Filename != "note.txt" {
		t.Fatalf("ListSessions() attachments = %#v", got.Items[0].Attachments)
	}
	if !slices.Equal(got.Items[0].AvailableActions, []string{"run", "close"}) {
		t.Fatalf("created actions = %#v", got.Items[0].AvailableActions)
	}
	if !slices.Equal(got.Items[1].AvailableActions, []string{"stop"}) {
		t.Fatalf("running actions = %#v", got.Items[1].AvailableActions)
	}
}

func TestGetSessionReturnsDetailWithResumeAction(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:             "session-1",
		ProjectID:      "project-1",
		Requirement:    "resume this",
		Status:         domain.StatusStopped,
		CodexSessionID: "codex-1",
	}
	repo.appends = []domain.PromptAppend{
		{ID: "append-1", SessionID: "session-1", Body: "extra context", CreatedAt: time.Unix(11, 0).UTC()},
	}
	repo.sessionAttachments["attachment-1"] = domain.SessionAttachment{
		ID:        "attachment-1",
		SessionID: "session-1",
		Filename:  "note.txt",
		MimeType:  "text/plain",
		Size:      5,
	}
	service := New(repo, newFakeProjectRepository("project-1"), WithAttachments(repo, newFakeAttachmentStore()))

	got, err := service.GetSession(ctx, "session-1")
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if !got.CanResume {
		t.Fatal("GetSession() CanResume = false")
	}
	if len(got.Attachments) != 1 || len(got.PromptAppends) != 1 {
		t.Fatalf("GetSession() detail collections, got attachments=%d appends=%d", len(got.Attachments), len(got.PromptAppends))
	}
	if got.Attachments[0].Filename != "note.txt" {
		t.Fatalf("GetSession() attachments = %#v", got.Attachments)
	}
	if got.PromptAppends[0].Body != "extra context" {
		t.Fatalf("GetSession() prompt appends = %#v", got.PromptAppends)
	}
	if !slices.Equal(got.AvailableActions, []string{"run", "resume", "close"}) {
		t.Fatalf("GetSession() actions = %#v", got.AvailableActions)
	}
}

func TestAppendPromptValidatesAndPersists(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := New(repo, newFakeProjectRepository("project-1"))
	service.now = func() time.Time { return time.Unix(20, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "append-1", nil }

	got, err := service.AppendPrompt(ctx, AppendPromptInput{
		SessionID: "session-1",
		Body:      "  continue with tests  ",
	})
	if err != nil {
		t.Fatalf("AppendPrompt() error = %v", err)
	}
	if got.ID != "append-1" || got.SessionID != "session-1" || got.Body != "continue with tests" {
		t.Fatalf("AppendPrompt() DTO = %#v", got)
	}
	if len(repo.appends) != 1 {
		t.Fatalf("appends = %d", len(repo.appends))
	}
	if repo.appends[0].Body != "continue with tests" {
		t.Fatalf("persisted append = %#v", repo.appends[0])
	}

	if _, err := service.AppendPrompt(ctx, AppendPromptInput{SessionID: "session-1", Body: "   "}); err == nil {
		t.Fatal("AppendPrompt() expected body error")
	}
}

func TestCloseSessionMarksClosedAndDefaultsReason(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Status:    domain.StatusCreated,
	}
	service := New(repo, newFakeProjectRepository("project-1"))
	service.now = func() time.Time { return time.Unix(30, 0).UTC() }

	got, err := service.CloseSession(ctx, CloseSessionInput{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("CloseSession() error = %v", err)
	}
	if got.Status != domain.StatusClosed {
		t.Fatalf("CloseSession() status = %q", got.Status)
	}
	saved := repo.sessions["session-1"]
	if saved.CloseReason == nil || *saved.CloseReason != domain.CloseReasonUserClosed {
		t.Fatalf("CloseSession() reason = %#v", saved.CloseReason)
	}
	if saved.ClosedAt == nil || !saved.ClosedAt.Equal(time.Unix(30, 0).UTC()) {
		t.Fatalf("CloseSession() ClosedAt = %#v", saved.ClosedAt)
	}
}

func TestLifecycleActionsDoNotMutateBeforeProcessAdapterIsWired(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Status:    domain.StatusCreated,
	}
	service := New(repo, newFakeProjectRepository("project-1"))

	if _, err := service.StartSession(ctx, "session-1"); !errors.Is(err, ErrProcessLifecycleNotWired) {
		t.Fatalf("StartSession() error = %v, want ErrProcessLifecycleNotWired", err)
	}
	if repo.sessions["session-1"].Status != domain.StatusCreated {
		t.Fatalf("StartSession() mutated status to %q", repo.sessions["session-1"].Status)
	}
}

func TestAvailableActionsByStatus(t *testing.T) {
	tests := []struct {
		name    string
		session domain.Session
		want    []string
	}{
		{
			name:    "created",
			session: domain.Session{Status: domain.StatusCreated},
			want:    []string{"run", "close"},
		},
		{
			name:    "running",
			session: domain.Session{Status: domain.StatusRunning},
			want:    []string{"stop"},
		},
		{
			name:    "stopped resumable",
			session: domain.Session{Status: domain.StatusStopped, CodexSessionID: "codex-1"},
			want:    []string{"run", "resume", "close"},
		},
		{
			name:    "resume failed",
			session: domain.Session{Status: domain.StatusResumeFailed, CodexSessionID: "codex-1"},
			want:    []string{"run", "resume", "stop", "close"},
		},
		{
			name:    "closed",
			session: domain.Session{Status: domain.StatusClosed},
			want:    []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := availableActions(tt.session)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("availableActions() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

type fakeRepository struct {
	saved              []domain.Session
	sessions           map[domain.ID]domain.Session
	listSessions       []domain.Session
	listTotal          int
	lastListQuery      domain.ListQuery
	appends            []domain.PromptAppend
	stagedAttachments  map[domain.StagedAttachmentID]domain.StagedAttachment
	sessionAttachments map[domain.SessionAttachmentID]domain.SessionAttachment
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		sessions:           map[domain.ID]domain.Session{},
		stagedAttachments:  map[domain.StagedAttachmentID]domain.StagedAttachment{},
		sessionAttachments: map[domain.SessionAttachmentID]domain.SessionAttachment{},
	}
}

func (r *fakeRepository) Save(_ context.Context, session domain.Session) error {
	r.saved = append(r.saved, session)
	r.sessions[session.ID] = session
	return nil
}

func (r *fakeRepository) Find(_ context.Context, id domain.ID) (domain.Session, error) {
	session, ok := r.sessions[id]
	if !ok {
		return domain.Session{}, errors.New("not found")
	}
	return session, nil
}

func (r *fakeRepository) ListCards(_ context.Context, query domain.ListQuery) ([]domain.Session, int, error) {
	r.lastListQuery = query
	return append([]domain.Session(nil), r.listSessions...), r.listTotal, nil
}

func (r *fakeRepository) LastConfigForProject(context.Context, domain.ProjectID) (domain.Config, bool, error) {
	return domain.Config{}, false, errors.New("unexpected LastConfigForProject call")
}

func (r *fakeRepository) AppendPrompt(_ context.Context, promptAppend domain.PromptAppend) error {
	r.appends = append(r.appends, promptAppend)
	return nil
}

func (r *fakeRepository) ListPromptAppends(_ context.Context, sessionID domain.ID) ([]domain.PromptAppend, error) {
	appends := make([]domain.PromptAppend, 0, len(r.appends))
	for _, promptAppend := range r.appends {
		if promptAppend.SessionID == sessionID {
			appends = append(appends, promptAppend)
		}
	}
	return appends, nil
}

func (r *fakeRepository) AddMergeRecord(context.Context, domain.MergeRecord) error {
	return errors.New("unexpected AddMergeRecord call")
}

func (r *fakeRepository) SaveStagedAttachment(_ context.Context, attachment domain.StagedAttachment) error {
	r.stagedAttachments[attachment.ID] = attachment
	return nil
}

func (r *fakeRepository) FindStagedAttachment(_ context.Context, id domain.StagedAttachmentID) (domain.StagedAttachment, error) {
	attachment, ok := r.stagedAttachments[id]
	if !ok {
		return domain.StagedAttachment{}, errors.New("not found")
	}
	return attachment, nil
}

func (r *fakeRepository) DeleteStagedAttachment(_ context.Context, id domain.StagedAttachmentID) error {
	delete(r.stagedAttachments, id)
	return nil
}

func (r *fakeRepository) SaveSessionAttachment(_ context.Context, attachment domain.SessionAttachment) error {
	r.sessionAttachments[attachment.ID] = attachment
	return nil
}

func (r *fakeRepository) FindSessionAttachment(_ context.Context, id domain.SessionAttachmentID) (domain.SessionAttachment, error) {
	attachment, ok := r.sessionAttachments[id]
	if !ok {
		return domain.SessionAttachment{}, errors.New("not found")
	}
	return attachment, nil
}

func (r *fakeRepository) ListSessionAttachments(_ context.Context, sessionID domain.ID) ([]domain.SessionAttachment, error) {
	attachments := make([]domain.SessionAttachment, 0, len(r.sessionAttachments))
	for _, attachment := range r.sessionAttachments {
		if attachment.SessionID == sessionID {
			attachments = append(attachments, attachment)
		}
	}
	return attachments, nil
}

func (r *fakeRepository) DeleteSessionAttachment(_ context.Context, id domain.SessionAttachmentID) error {
	delete(r.sessionAttachments, id)
	return nil
}

type fakeAttachmentStore struct {
	promoted        map[domain.StagedAttachmentID]bool
	deletedSessions map[domain.SessionAttachmentID]bool
	promoteErr      error
}

func newFakeAttachmentStore() *fakeAttachmentStore {
	return &fakeAttachmentStore{
		promoted:        map[domain.StagedAttachmentID]bool{},
		deletedSessions: map[domain.SessionAttachmentID]bool{},
	}
}

func (s *fakeAttachmentStore) Stage(context.Context, domain.StageAttachmentInput) (domain.StagedAttachment, error) {
	return domain.StagedAttachment{}, errors.New("unexpected Stage call")
}

func (s *fakeAttachmentStore) Promote(_ context.Context, staged domain.StagedAttachment, sessionID domain.ID) (domain.SessionAttachment, error) {
	if s.promoteErr != nil {
		return domain.SessionAttachment{}, s.promoteErr
	}
	s.promoted[staged.ID] = true
	return domain.SessionAttachment{
		ID:          domain.SessionAttachmentID(staged.ID),
		SessionID:   sessionID,
		Kind:        "file",
		Filename:    staged.Filename,
		Path:        "/attachments/sessions/" + string(sessionID) + "/" + string(staged.ID) + "/" + staged.Filename,
		MimeType:    staged.MimeType,
		Size:        staged.Size,
		Previewable: staged.Previewable,
		CreatedAt:   time.Unix(11, 0).UTC(),
	}, nil
}

func (s *fakeAttachmentStore) DeleteStaged(context.Context, domain.StagedAttachmentID) error {
	return errors.New("unexpected DeleteStaged call")
}

func (s *fakeAttachmentStore) DeleteSession(_ context.Context, id domain.SessionAttachmentID) error {
	s.deletedSessions[id] = true
	return nil
}

func (s *fakeAttachmentStore) Open(context.Context, string) (domain.AttachmentStream, error) {
	return domain.AttachmentStream{}, errors.New("unexpected Open call")
}

type fakeProjectRepository struct {
	projects map[projectdomain.ID]projectdomain.Project
}

func newFakeProjectRepository(ids ...projectdomain.ID) *fakeProjectRepository {
	repo := &fakeProjectRepository{projects: map[projectdomain.ID]projectdomain.Project{}}
	for _, id := range ids {
		repo.projects[id] = projectdomain.Project{ID: id, Name: string(id)}
	}
	return repo
}

func (r *fakeProjectRepository) Save(context.Context, projectdomain.Project) error {
	return errors.New("unexpected project Save call")
}

func (r *fakeProjectRepository) Find(_ context.Context, id projectdomain.ID) (projectdomain.Project, error) {
	project, ok := r.projects[id]
	if !ok {
		return projectdomain.Project{}, errors.New("not found")
	}
	return project, nil
}

func (r *fakeProjectRepository) List(context.Context) ([]projectdomain.Project, error) {
	return nil, errors.New("unexpected project List call")
}

func (r *fakeProjectRepository) UpdateDefaultWorkflow(context.Context, projectdomain.ID, projectdomain.WorkflowDefinitionID) error {
	return errors.New("unexpected project UpdateDefaultWorkflow call")
}
