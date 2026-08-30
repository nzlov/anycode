package setting

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"slices"
	"testing"
	"time"

	"github.com/nzlov/anycode/internal/application/apperror"
	processdomain "github.com/nzlov/anycode/internal/domain/process"
	domain "github.com/nzlov/anycode/internal/domain/setting"
)

func TestCreateQuickCommandAllowsDuplicateContent(t *testing.T) {
	repo := &fakeRepository{}
	service := New(repo)
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	ids := []domain.QuickCommandID{"command-1", "command-2"}
	service.generateID = func() (domain.QuickCommandID, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}

	first, err := service.CreateQuickCommand(context.Background(), CreateQuickCommandInput{Content: "检查测试"})
	if err != nil {
		t.Fatalf("CreateQuickCommand() first error = %v", err)
	}
	second, err := service.CreateQuickCommand(context.Background(), CreateQuickCommandInput{Content: "检查测试"})
	if err != nil {
		t.Fatalf("CreateQuickCommand() second error = %v", err)
	}
	if first.ID == second.ID || first.Content != second.Content || len(repo.commands) != 2 {
		t.Fatalf("duplicate commands = first:%#v second:%#v stored:%#v", first, second, repo.commands)
	}
}

func TestQuickCommandUseCasePreservesProjectScope(t *testing.T) {
	projectID := domain.QuickCommandProjectID("project-1")
	repo := &fakeRepository{}
	service := New(repo)

	created, err := service.CreateQuickCommand(context.Background(), CreateQuickCommandInput{
		ProjectID: &projectID,
		Content:   "项目指令",
	})
	if err != nil || created.ProjectID == nil || *created.ProjectID != projectID {
		t.Fatalf("CreateQuickCommand() = %#v, %v", created, err)
	}
	_, err = service.ListQuickCommands(context.Background(), ListQuickCommandsInput{
		ProjectID:     &projectID,
		IncludeGlobal: true,
	})
	if err != nil || repo.listQuery.ProjectID == nil || *repo.listQuery.ProjectID != projectID || !repo.listQuery.IncludeGlobal {
		t.Fatalf("ListQuickCommands() query = %#v, %v", repo.listQuery, err)
	}
}

func TestUpdateQuickCommandTrimsContentAndPreservesIdentity(t *testing.T) {
	createdAt := time.Unix(10, 0).UTC()
	repo := &fakeRepository{commands: []domain.QuickCommand{{ID: "command-1", Content: "检查测试", CreatedAt: createdAt}}}
	service := New(repo)

	updated, err := service.UpdateQuickCommand(context.Background(), UpdateQuickCommandInput{ID: "command-1", Content: "  总结变更  "})
	if err != nil {
		t.Fatalf("UpdateQuickCommand() error = %v", err)
	}
	if updated.ID != "command-1" || updated.Content != "总结变更" || !updated.CreatedAt.Equal(createdAt) {
		t.Fatalf("UpdateQuickCommand() = %#v", updated)
	}
}

func TestAppearanceSettingsDefaultUpdateAndValidation(t *testing.T) {
	repo := &fakeRepository{configuration: domain.DefaultSystemConfiguration()}
	service := New(repo)

	got, err := service.GetAppearanceSettings(context.Background())
	if err != nil || got.WallpaperColorScheme != domain.WallpaperColorSchemeContent {
		t.Fatalf("GetAppearanceSettings() = %#v, %v", got, err)
	}
	got, err = service.UpdateAppearanceSettings(context.Background(), UpdateAppearanceSettingsInput{
		BackgroundType:       domain.BackgroundTypeSolid,
		SolidTheme:           domain.SolidThemeAzure,
		BackgroundMask:       35,
		WallpaperColorScheme: domain.WallpaperColorSchemeRainbow,
	})
	if err != nil || got.BackgroundType != domain.BackgroundTypeSolid || got.SolidTheme != domain.SolidThemeAzure || got.BackgroundMask != 35 || got.WallpaperColorScheme != domain.WallpaperColorSchemeRainbow || repo.configuration.WallpaperColorScheme != domain.WallpaperColorSchemeRainbow {
		t.Fatalf("UpdateAppearanceSettings() = %#v, %v; stored %#v", got, err, repo.configuration)
	}
	got, err = service.UpdateAppearanceSettings(context.Background(), UpdateAppearanceSettingsInput{
		BackgroundType:       domain.BackgroundTypeNASA,
		SolidTheme:           domain.SolidThemeAzure,
		BackgroundMask:       35,
		WallpaperColorScheme: domain.WallpaperColorSchemeRainbow,
	})
	if err != nil || got.BackgroundType != domain.BackgroundTypeNASA || repo.configuration.BackgroundType != domain.BackgroundTypeNASA {
		t.Fatalf("UpdateAppearanceSettings(NASA) = %#v, %v; stored %#v", got, err, repo.configuration)
	}
	_, err = service.UpdateAppearanceSettings(context.Background(), UpdateAppearanceSettingsInput{
		BackgroundType:       domain.BackgroundTypeSolid,
		SolidTheme:           domain.SolidThemeAzure,
		WallpaperColorScheme: "unknown",
	})
	assertAppError(t, err, apperror.CodeValidationFailed)
}

func TestGeneralSettingsDefaultUpdateAndValidation(t *testing.T) {
	repo := &fakeRepository{configuration: domain.DefaultSystemConfiguration()}
	changed := 0
	service := New(repo, WithConcurrencyLimitChanged(func() { changed++ }))

	got, err := service.GetGeneralSettings(context.Background())
	if err != nil || got.AgentMaxConcurrent != 2 || len(got.AgentWritableRoots) != 0 || got.SendShortcut != domain.SendShortcutShiftEnter || got.MindMapLayout != domain.MindMapLayoutRadial {
		t.Fatalf("GetGeneralSettings() = %#v, %v", got, err)
	}
	got, err = service.UpdateGeneralSettings(context.Background(), UpdateGeneralSettingsInput{
		AgentMaxConcurrent: 4,
		AgentWritableRoots: []string{" /home/anycode/.cache/go-build ", "/home/anycode/go", "/home/anycode/go"},
		SendShortcut:       domain.SendShortcutEnter,
		MindMapLayout:      domain.MindMapLayoutNested,
	})
	wantRoots := []string{"/home/anycode/.cache/go-build", "/home/anycode/go"}
	if err != nil || got.AgentMaxConcurrent != 4 || got.SendShortcut != domain.SendShortcutEnter || got.MindMapLayout != domain.MindMapLayoutNested || !slices.Equal(got.AgentWritableRoots, wantRoots) || !slices.Equal(repo.configuration.AgentWritableRoots, wantRoots) || repo.configuration.SendShortcut != domain.SendShortcutEnter || repo.configuration.MindMap.Layout != domain.MindMapLayoutNested || changed != 1 {
		t.Fatalf("UpdateGeneralSettings() = %#v, %v; stored=%#v changed=%d", got, err, repo.configuration, changed)
	}
	_, err = service.UpdateGeneralSettings(context.Background(), UpdateGeneralSettingsInput{AgentMaxConcurrent: 0})
	assertAppError(t, err, apperror.CodeValidationFailed)
	if changed != 1 {
		t.Fatalf("invalid update callback count = %d", changed)
	}
	_, err = service.UpdateGeneralSettings(context.Background(), UpdateGeneralSettingsInput{AgentMaxConcurrent: 2, AgentWritableRoots: []string{"relative/path"}})
	assertAppError(t, err, apperror.CodeValidationFailed)
	_, err = service.UpdateGeneralSettings(context.Background(), UpdateGeneralSettingsInput{AgentMaxConcurrent: 2, AgentWritableRoots: []string{"/"}})
	assertAppError(t, err, apperror.CodeValidationFailed)
	_, err = service.UpdateGeneralSettings(context.Background(), UpdateGeneralSettingsInput{AgentMaxConcurrent: 2, SendShortcut: "space"})
	assertAppError(t, err, apperror.CodeValidationFailed)
	_, err = service.UpdateGeneralSettings(context.Background(), UpdateGeneralSettingsInput{AgentMaxConcurrent: 2, MindMapLayout: "grid"})
	assertAppError(t, err, apperror.CodeValidationFailed)
}

func TestCodexSettingsSaveAppliesChangedContextWindow(t *testing.T) {
	repo := &fakeRepository{configuration: domain.DefaultSystemConfiguration()}
	applied := []int{}
	service := New(repo, WithCodexSettingsChanged(func(_ context.Context, contextWindow int) error {
		applied = append(applied, contextWindow)
		return nil
	}))

	got, err := service.GetCodexSettings(context.Background())
	if err != nil || got.ContextWindow != nil {
		t.Fatalf("GetCodexSettings() = %#v, %v", got, err)
	}
	contextWindow := 200_000
	got, err = service.UpdateCodexSettings(context.Background(), UpdateCodexSettingsInput{ContextWindow: &contextWindow})
	if err != nil || got.ContextWindow == nil || *got.ContextWindow != contextWindow || repo.configuration.Codex.ContextWindow != contextWindow || !slices.Equal(applied, []int{contextWindow}) {
		t.Fatalf("UpdateCodexSettings() = %#v, %v; stored=%#v applied=%#v", got, err, repo.configuration.Codex, applied)
	}
	if _, err := service.UpdateCodexSettings(context.Background(), UpdateCodexSettingsInput{ContextWindow: &contextWindow}); err != nil || len(applied) != 1 {
		t.Fatalf("unchanged UpdateCodexSettings() error = %v, applied=%#v", err, applied)
	}
	got, err = service.UpdateCodexSettings(context.Background(), UpdateCodexSettingsInput{})
	if err != nil || got.ContextWindow != nil || repo.configuration.Codex.ContextWindow != 0 || !slices.Equal(applied, []int{contextWindow, 0}) {
		t.Fatalf("default UpdateCodexSettings() = %#v, %v; stored=%#v applied=%#v", got, err, repo.configuration.Codex, applied)
	}
	invalid := 0
	_, err = service.UpdateCodexSettings(context.Background(), UpdateCodexSettingsInput{ContextWindow: &invalid})
	assertAppError(t, err, apperror.CodeValidationFailed)
}

func TestAsyncMindMapSettingsRequireAvailableModelAndReasoningEffort(t *testing.T) {
	repo := &fakeRepository{configuration: domain.DefaultSystemConfiguration()}
	changed := 0
	service := New(repo, WithMindMapSettings([]processdomain.CodexModel{{
		Slug: "gpt-mind-map", SupportedReasoningLevels: []processdomain.CodexReasoningLevel{{Effort: "high"}},
	}}, func() { changed++ }))
	input := UpdateGeneralSettingsInput{
		AgentMaxConcurrent: 2, MindMapEnabled: true, MindMapMode: domain.MindMapModeAsync,
		MindMapModel: "gpt-mind-map", MindMapReasoningEffort: "", MindMapMaxConcurrent: 3,
	}

	_, err := service.UpdateGeneralSettings(context.Background(), input)
	assertAppError(t, err, apperror.CodeValidationFailed)
	input.MindMapReasoningEffort = "unsupported"
	_, err = service.UpdateGeneralSettings(context.Background(), input)
	assertAppError(t, err, apperror.CodeValidationFailed)
	input.MindMapReasoningEffort = "high"
	got, err := service.UpdateGeneralSettings(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if got.MindMapMode != domain.MindMapModeAsync || got.MindMapModel != "gpt-mind-map" || got.MindMapReasoningEffort != "high" || got.MindMapMaxConcurrent != 3 {
		t.Fatalf("settings = %#v", got)
	}
	if changed != 1 || repo.configuration.MindMap.MaxConcurrent != 3 {
		t.Fatalf("callback = %d, stored = %#v", changed, repo.configuration.MindMap)
	}
}

func TestUploadAppearanceWallpaperStoresImageAndSelectsIt(t *testing.T) {
	repo := &fakeRepository{configuration: domain.DefaultSystemConfiguration()}
	wallpapers := &fakeWallpaperStore{files: map[string][]byte{}}
	service := New(repo, WithWallpaperStore(wallpapers))
	imageData := testPNG(t)

	got, err := service.UploadAppearanceWallpaper(context.Background(), UploadAppearanceWallpaperInput{
		Filename: "山水.png",
		Size:     int64(len(imageData)),
		Reader:   bytes.NewReader(imageData),
	})
	if err != nil {
		t.Fatalf("UploadAppearanceWallpaper() error = %v", err)
	}
	if got.BackgroundType != domain.BackgroundTypeImage || got.WallpaperID == "" || got.WallpaperFilename != "山水.png" {
		t.Fatalf("UploadAppearanceWallpaper() = %#v", got)
	}
	if len(wallpapers.files[got.WallpaperID]) != len(imageData) || repo.configuration.WallpaperMimeType != "image/png" {
		t.Fatalf("stored wallpaper = %#v config=%#v", wallpapers.files, repo.configuration)
	}
	stream, err := service.OpenAppearanceWallpaper(context.Background(), got.WallpaperID)
	if err != nil {
		t.Fatalf("OpenAppearanceWallpaper() error = %v", err)
	}
	defer stream.Reader.Close()
	opened, _ := io.ReadAll(stream.Reader)
	if !bytes.Equal(opened, imageData) || stream.MimeType != "image/png" {
		t.Fatalf("opened wallpaper = %d bytes %q", len(opened), stream.MimeType)
	}
}

func TestUploadAppearanceWallpaperRejectsNonImage(t *testing.T) {
	service := New(&fakeRepository{configuration: domain.DefaultSystemConfiguration()}, WithWallpaperStore(&fakeWallpaperStore{}))
	data := []byte("not an image")
	_, err := service.UploadAppearanceWallpaper(context.Background(), UploadAppearanceWallpaperInput{
		Filename: "note.txt",
		Size:     int64(len(data)),
		Reader:   bytes.NewReader(data),
	})
	assertAppError(t, err, apperror.CodeValidationFailed)
}

func TestOpenNASAWallpaperStreamsSourceImage(t *testing.T) {
	service := New(&fakeRepository{}, WithNASAWallpaperSource(fakeNASAWallpaperSource{}))
	stream, err := service.OpenNASAWallpaper(context.Background())
	if err != nil {
		t.Fatalf("OpenNASAWallpaper() error = %v", err)
	}
	defer stream.Reader.Close()
	data, err := io.ReadAll(stream.Reader)
	if err != nil || stream.MimeType != "image/jpeg" || string(data) != "nasa" {
		t.Fatalf("NASA wallpaper = type:%q data:%q error:%v", stream.MimeType, data, err)
	}
}

func TestDeleteQuickCommandUsesID(t *testing.T) {
	repo := &fakeRepository{commands: []domain.QuickCommand{
		{ID: "command-1", Content: "检查测试"},
		{ID: "command-2", Content: "检查测试"},
	}}
	service := New(repo)

	if err := service.DeleteQuickCommand(context.Background(), DeleteQuickCommandInput{ID: "command-1"}); err != nil {
		t.Fatalf("DeleteQuickCommand() error = %v", err)
	}
	if len(repo.commands) != 1 || repo.commands[0].ID != "command-2" {
		t.Fatalf("commands after delete = %#v", repo.commands)
	}
}

func TestListQuickCommandsNormalizesPagination(t *testing.T) {
	repo := &fakeRepository{page: domain.QuickCommandPage{
		Items:    []domain.QuickCommand{{ID: "command-1", Content: "检查测试"}},
		Page:     1,
		PageSize: 100,
		Total:    3,
	}}
	service := New(repo)

	page, err := service.ListQuickCommands(context.Background(), ListQuickCommandsInput{Page: -1, PageSize: 500})
	if err != nil {
		t.Fatalf("ListQuickCommands() error = %v", err)
	}
	if repo.listQuery.Page != 1 || repo.listQuery.PageSize != 100 {
		t.Fatalf("list query = %#v", repo.listQuery)
	}
	if page.Page != 1 || page.PageSize != 100 || page.Total != 3 || len(page.Items) != 1 {
		t.Fatalf("page = %#v", page)
	}
}

func TestQuickCommandValidationErrorsAreStructured(t *testing.T) {
	service := New(&fakeRepository{})
	emptyProjectID := domain.QuickCommandProjectID(" ")

	_, err := service.CreateQuickCommand(context.Background(), CreateQuickCommandInput{Content: "   "})
	assertAppError(t, err, apperror.CodeValidationFailed)
	_, err = service.CreateQuickCommand(context.Background(), CreateQuickCommandInput{ProjectID: &emptyProjectID, Content: "检查测试"})
	assertAppError(t, err, apperror.CodeValidationFailed)
	_, err = service.ListQuickCommands(context.Background(), ListQuickCommandsInput{ProjectID: &emptyProjectID})
	assertAppError(t, err, apperror.CodeValidationFailed)
	_, err = service.UpdateQuickCommand(context.Background(), UpdateQuickCommandInput{Content: "检查测试"})
	assertAppError(t, err, apperror.CodeValidationFailed)
	_, err = service.UpdateQuickCommand(context.Background(), UpdateQuickCommandInput{ID: "command-1", Content: "   "})
	assertAppError(t, err, apperror.CodeValidationFailed)
	err = service.DeleteQuickCommand(context.Background(), DeleteQuickCommandInput{})
	assertAppError(t, err, apperror.CodeValidationFailed)
}

func TestDeleteQuickCommandMapsNotFound(t *testing.T) {
	repo := &fakeRepository{deleteErr: domain.ErrQuickCommandNotFound}
	service := New(repo)

	err := service.DeleteQuickCommand(context.Background(), DeleteQuickCommandInput{ID: "missing"})
	assertAppError(t, err, apperror.CodeNotFound)
}

func TestUpdateQuickCommandMapsNotFound(t *testing.T) {
	repo := &fakeRepository{updateErr: domain.ErrQuickCommandNotFound}
	service := New(repo)

	_, err := service.UpdateQuickCommand(context.Background(), UpdateQuickCommandInput{ID: "missing", Content: "检查测试"})
	assertAppError(t, err, apperror.CodeNotFound)
}

func assertAppError(t *testing.T, err error, code string) {
	t.Helper()
	appErr, ok := apperror.From(err)
	if !ok || appErr.Code != code || appErr.Category != apperror.CategoryValidationError {
		t.Fatalf("error = %#v", err)
	}
}

type fakeRepository struct {
	commands      []domain.QuickCommand
	page          domain.QuickCommandPage
	listQuery     domain.QuickCommandQuery
	updateErr     error
	deleteErr     error
	configuration domain.SystemConfiguration
}

type fakeWallpaperStore struct {
	files map[string][]byte
}

type fakeNASAWallpaperSource struct{}

func (fakeNASAWallpaperSource) Open(context.Context) (domain.RemoteWallpaper, error) {
	return domain.RemoteWallpaper{
		MimeType: "image/jpeg",
		Reader:   io.NopCloser(bytes.NewReader([]byte("nasa"))),
	}, nil
}

func (s *fakeWallpaperStore) SaveWallpaper(_ context.Context, id string, reader io.Reader) error {
	if s.files == nil {
		s.files = map[string][]byte{}
	}
	data, err := io.ReadAll(reader)
	if err == nil {
		s.files[id] = data
	}
	return err
}

func (s *fakeWallpaperStore) OpenWallpaper(_ context.Context, id string) (io.ReadCloser, error) {
	data, ok := s.files[id]
	if !ok {
		return nil, io.EOF
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (s *fakeWallpaperStore) DeleteWallpaper(_ context.Context, id string) error {
	delete(s.files, id)
	return nil
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 210, G: 48, B: 35, A: 255})
	if err := png.Encode(&output, img); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func (r *fakeRepository) GetSystemConfiguration(_ context.Context) (domain.SystemConfiguration, error) {
	return r.configuration, nil
}

func (r *fakeRepository) SaveSystemConfiguration(_ context.Context, configuration domain.SystemConfiguration) error {
	r.configuration = configuration
	return nil
}

func (r *fakeRepository) UpdateGeneralSettings(_ context.Context, max int, writableRoots []string) error {
	r.configuration.AgentMaxConcurrent = max
	r.configuration.AgentWritableRoots = append([]string(nil), writableRoots...)
	return nil
}

func (r *fakeRepository) Create(_ context.Context, command domain.QuickCommand) error {
	r.commands = append(r.commands, command)
	return nil
}

func (r *fakeRepository) Update(_ context.Context, id domain.QuickCommandID, content string) (domain.QuickCommand, error) {
	if r.updateErr != nil {
		return domain.QuickCommand{}, r.updateErr
	}
	for index, command := range r.commands {
		if command.ID == id {
			r.commands[index].Content = content
			return r.commands[index], nil
		}
	}
	return domain.QuickCommand{}, domain.ErrQuickCommandNotFound
}

func (r *fakeRepository) List(_ context.Context, query domain.QuickCommandQuery) (domain.QuickCommandPage, error) {
	r.listQuery = query
	if r.page.PageSize != 0 {
		return r.page, nil
	}
	return domain.QuickCommandPage{
		Items:    append([]domain.QuickCommand(nil), r.commands...),
		Page:     query.Page,
		PageSize: query.PageSize,
		Total:    len(r.commands),
	}, nil
}

func (r *fakeRepository) Delete(_ context.Context, id domain.QuickCommandID) error {
	if r.deleteErr != nil {
		return r.deleteErr
	}
	for index, command := range r.commands {
		if command.ID == id {
			r.commands = append(r.commands[:index], r.commands[index+1:]...)
			return nil
		}
	}
	return nil
}
