package entstore

import (
	"context"
	"fmt"

	"github.com/nzlov/anycode/internal/domain/setting"
	"github.com/nzlov/anycode/internal/infra/entstore/ent"
)

const globalSystemConfigurationID = "global"

func (r *SettingRepository) GetSystemConfiguration(ctx context.Context) (setting.SystemConfiguration, error) {
	row, err := r.client.SystemConfiguration.Get(ctx, globalSystemConfigurationID)
	if ent.IsNotFound(err) {
		return setting.DefaultSystemConfiguration(), nil
	}
	if err != nil {
		return setting.SystemConfiguration{}, fmt.Errorf("get system configuration: %w", err)
	}
	return setting.SystemConfiguration{
		WallpaperColorScheme: setting.WallpaperColorScheme(row.WallpaperColorScheme),
	}, nil
}

func (r *SettingRepository) SaveSystemConfiguration(ctx context.Context, configuration setting.SystemConfiguration) error {
	_, err := r.client.SystemConfiguration.UpdateOneID(globalSystemConfigurationID).
		SetWallpaperColorScheme(string(configuration.WallpaperColorScheme)).
		Save(ctx)
	if err == nil {
		return nil
	}
	if !ent.IsNotFound(err) {
		return fmt.Errorf("update system configuration: %w", err)
	}
	if _, err := r.client.SystemConfiguration.Create().
		SetID(globalSystemConfigurationID).
		SetWallpaperColorScheme(string(configuration.WallpaperColorScheme)).
		Save(ctx); err != nil {
		return fmt.Errorf("create system configuration: %w", err)
	}
	return nil
}
