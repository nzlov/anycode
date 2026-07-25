package entstore

import (
	"context"
	"path/filepath"
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
	if got.AgentMaxConcurrent != 5 || got.BackgroundType != setting.BackgroundTypeImage || got.SolidTheme != setting.SolidThemeIndigo || got.BackgroundMask != 42 || got.WallpaperColorScheme != setting.WallpaperColorSchemeRainbow || got.WallpaperID != "wallpaper-id" || got.WallpaperFilename != "山水.png" || got.WallpaperMimeType != "image/png" {
		t.Fatalf("saved system configuration = %#v", got)
	}
	max, err := store.Settings().MaxConcurrentAgents(ctx)
	if err != nil || max != 5 {
		t.Fatalf("MaxConcurrentAgents() = %d, %v", max, err)
	}
	if err := store.Settings().UpdateAgentMaxConcurrent(ctx, 6); err != nil {
		t.Fatalf("UpdateAgentMaxConcurrent() error = %v", err)
	}
	got, err = store.Settings().GetSystemConfiguration(ctx)
	if err != nil || got.AgentMaxConcurrent != 6 || got.BackgroundType != setting.BackgroundTypeImage || got.WallpaperID != "wallpaper-id" {
		t.Fatalf("configuration after focused concurrency update = %#v, %v", got, err)
	}
}
