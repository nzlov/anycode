package setting

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"testing"
	"time"

	"github.com/nzlov/anycode/internal/application/apperror"
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
	_, err = service.UpdateAppearanceSettings(context.Background(), UpdateAppearanceSettingsInput{
		BackgroundType:       domain.BackgroundTypeSolid,
		SolidTheme:           domain.SolidThemeAzure,
		WallpaperColorScheme: "unknown",
	})
	assertAppError(t, err, apperror.CodeValidationFailed)
}

func TestUploadAppearanceWallpaperStoresImageAndSelectsIt(t *testing.T) {
	repo := &fakeRepository{configuration: domain.DefaultSystemConfiguration()}
	wallpapers := &fakeWallpaperStore{files: map[string][]byte{}}
	service := New(repo, wallpapers)
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
	service := New(&fakeRepository{configuration: domain.DefaultSystemConfiguration()}, &fakeWallpaperStore{})
	data := []byte("not an image")
	_, err := service.UploadAppearanceWallpaper(context.Background(), UploadAppearanceWallpaperInput{
		Filename: "note.txt",
		Size:     int64(len(data)),
		Reader:   bytes.NewReader(data),
	})
	assertAppError(t, err, apperror.CodeValidationFailed)
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

	_, err := service.CreateQuickCommand(context.Background(), CreateQuickCommandInput{Content: "   "})
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
	deleteErr     error
	configuration domain.SystemConfiguration
}

type fakeWallpaperStore struct {
	files map[string][]byte
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

func (r *fakeRepository) Create(_ context.Context, command domain.QuickCommand) error {
	r.commands = append(r.commands, command)
	return nil
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
