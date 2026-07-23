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
		BackgroundType:       setting.BackgroundType(row.BackgroundType),
		SolidTheme:           setting.SolidTheme(row.SolidTheme),
		BackgroundMask:       row.BackgroundMask,
		WallpaperColorScheme: setting.WallpaperColorScheme(row.WallpaperColorScheme),
		WallpaperID:          row.WallpaperID,
		WallpaperFilename:    row.WallpaperFilename,
		WallpaperMimeType:    row.WallpaperMimeType,
	}, nil
}

func (r *SettingRepository) SaveSystemConfiguration(ctx context.Context, configuration setting.SystemConfiguration) error {
	_, err := r.client.SystemConfiguration.UpdateOneID(globalSystemConfigurationID).
		SetBackgroundType(string(configuration.BackgroundType)).
		SetSolidTheme(string(configuration.SolidTheme)).
		SetBackgroundMask(configuration.BackgroundMask).
		SetWallpaperColorScheme(string(configuration.WallpaperColorScheme)).
		SetWallpaperID(configuration.WallpaperID).
		SetWallpaperFilename(configuration.WallpaperFilename).
		SetWallpaperMimeType(configuration.WallpaperMimeType).
		Save(ctx)
	if err == nil {
		return nil
	}
	if !ent.IsNotFound(err) {
		return fmt.Errorf("update system configuration: %w", err)
	}
	if _, err := r.client.SystemConfiguration.Create().
		SetID(globalSystemConfigurationID).
		SetBackgroundType(string(configuration.BackgroundType)).
		SetSolidTheme(string(configuration.SolidTheme)).
		SetBackgroundMask(configuration.BackgroundMask).
		SetWallpaperColorScheme(string(configuration.WallpaperColorScheme)).
		SetWallpaperID(configuration.WallpaperID).
		SetWallpaperFilename(configuration.WallpaperFilename).
		SetWallpaperMimeType(configuration.WallpaperMimeType).
		Save(ctx); err != nil {
		return fmt.Errorf("create system configuration: %w", err)
	}
	return nil
}
