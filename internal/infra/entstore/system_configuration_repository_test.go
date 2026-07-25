package entstore

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"github.com/nzlov/anycode/internal/domain/setting"
)

func TestSystemConfigurationRepositoryPersistsAppearanceSettings(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate store: %v", err)
	}

	configuration, err := store.Settings().GetSystemConfiguration(ctx)
	if err != nil {
		t.Fatalf("get default system configuration: %v", err)
	}
	if configuration.AgentMaxConcurrent != 2 || configuration.BackgroundType != setting.BackgroundTypeBing || configuration.SolidTheme != setting.SolidThemeVermilion || configuration.WallpaperColorScheme != setting.WallpaperColorSchemeContent {
		t.Fatalf("default system configuration = %#v", configuration)
	}

	configuration.BackgroundType = setting.BackgroundTypeImage
	configuration.AgentMaxConcurrent = 5
	configuration.AgentWritableRoots = []string{"/home/anycode/.cache/go-build", "/home/anycode/go"}
	configuration.SolidTheme = setting.SolidThemeIndigo
	configuration.BackgroundMask = 42
	configuration.WallpaperColorScheme = setting.WallpaperColorSchemeRainbow
	configuration.WallpaperID = "wallpaper-id"
	configuration.WallpaperFilename = "山水.png"
	configuration.WallpaperMimeType = "image/png"
	if err := store.Settings().SaveSystemConfiguration(ctx, configuration); err != nil {
		t.Fatalf("save system configuration: %v", err)
	}
	got, err := store.Settings().GetSystemConfiguration(ctx)
	if err != nil {
		t.Fatalf("get saved system configuration: %v", err)
	}
	if got.AgentMaxConcurrent != 5 || !slices.Equal(got.AgentWritableRoots, configuration.AgentWritableRoots) || got.BackgroundType != setting.BackgroundTypeImage || got.SolidTheme != setting.SolidThemeIndigo || got.BackgroundMask != 42 || got.WallpaperColorScheme != setting.WallpaperColorSchemeRainbow || got.WallpaperID != "wallpaper-id" || got.WallpaperFilename != "山水.png" || got.WallpaperMimeType != "image/png" {
		t.Fatalf("saved system configuration = %#v", got)
	}
	max, err := store.Settings().MaxConcurrentAgents(ctx)
	if err != nil || max != 5 {
		t.Fatalf("MaxConcurrentAgents() = %d, %v", max, err)
	}
	updatedRoots := []string{"/var/cache/go-build"}
	if err := store.Settings().UpdateGeneralSettings(ctx, 6, updatedRoots); err != nil {
		t.Fatalf("UpdateGeneralSettings() error = %v", err)
	}
	got, err = store.Settings().GetSystemConfiguration(ctx)
	if err != nil || got.AgentMaxConcurrent != 6 || !slices.Equal(got.AgentWritableRoots, updatedRoots) || got.BackgroundType != setting.BackgroundTypeImage || got.WallpaperID != "wallpaper-id" {
		t.Fatalf("configuration after focused concurrency update = %#v, %v", got, err)
	}
	providedRoots, err := store.Settings().AgentWritableRoots(ctx)
	if err != nil || !slices.Equal(providedRoots, updatedRoots) {
		t.Fatalf("AgentWritableRoots() = %#v, %v", providedRoots, err)
	}
}

func TestSystemConfigurationMigrationAddsWritableRootsToExistingRow(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	if _, err := store.db.ExecContext(ctx, `
		CREATE TABLE system_configurations (
			id text NOT NULL PRIMARY KEY,
			agent_max_concurrent integer NOT NULL DEFAULT 2,
			wallpaper_color_scheme text NOT NULL,
			background_type text NOT NULL DEFAULT 'bing',
			solid_theme text NOT NULL DEFAULT 'vermilion',
			background_mask integer NOT NULL DEFAULT 0,
			wallpaper_id text NOT NULL DEFAULT '',
			wallpaper_filename text NOT NULL DEFAULT '',
			wallpaper_mime_type text NOT NULL DEFAULT '',
			updated_at datetime NOT NULL
		);
		INSERT INTO system_configurations (
			id, agent_max_concurrent, wallpaper_color_scheme, background_type, solid_theme,
			background_mask, wallpaper_id, wallpaper_filename, wallpaper_mime_type, updated_at
		) VALUES ('global', 3, 'content', 'bing', 'vermilion', 0, '', '', '', CURRENT_TIMESTAMP);
	`); err != nil {
		t.Fatalf("create previous system configuration schema: %v", err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate store: %v", err)
	}
	configuration, err := store.Settings().GetSystemConfiguration(ctx)
	if err != nil || configuration.AgentMaxConcurrent != 3 || len(configuration.AgentWritableRoots) != 0 {
		t.Fatalf("migrated system configuration = %#v, %v", configuration, err)
	}
}
