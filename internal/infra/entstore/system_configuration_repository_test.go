package entstore

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/nzlov/anycode/internal/domain/setting"
)

func TestSystemConfigurationRepositoryPersistsWallpaperColorScheme(t *testing.T) {
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
	if configuration.WallpaperColorScheme != setting.WallpaperColorSchemeContent {
		t.Fatalf("default system configuration = %#v", configuration)
	}

	configuration.WallpaperColorScheme = setting.WallpaperColorSchemeRainbow
	if err := store.Settings().SaveSystemConfiguration(ctx, configuration); err != nil {
		t.Fatalf("save system configuration: %v", err)
	}
	got, err := store.Settings().GetSystemConfiguration(ctx)
	if err != nil {
		t.Fatalf("get saved system configuration: %v", err)
	}
	if got.WallpaperColorScheme != setting.WallpaperColorSchemeRainbow {
		t.Fatalf("saved system configuration = %#v", got)
	}
}
