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
		AgentMaxConcurrent: row.AgentMaxConcurrent,
		AgentWritableRoots: append([]string(nil), row.AgentWritableRoots...),
		SendShortcut:       setting.SendShortcut(row.SendShortcut),
		Codex:              setting.CodexConfiguration{ContextWindow: row.CodexContextWindow},
		MindMap: setting.MindMapConfiguration{
			Enabled: row.MindMapEnabled, Mode: setting.MindMapMode(row.MindMapMode), Layout: setting.MindMapLayout(row.MindMapLayout), Model: row.MindMapModel,
			ReasoningEffort: row.MindMapReasoningEffort, MaxConcurrent: row.MindMapMaxConcurrent,
		},
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
		SetAgentMaxConcurrent(configuration.AgentMaxConcurrent).
		SetAgentWritableRoots(configuration.AgentWritableRoots).
		SetSendShortcut(string(configuration.SendShortcut)).
		SetCodexContextWindow(configuration.Codex.ContextWindow).
		SetMindMapEnabled(configuration.MindMap.Enabled).
		SetMindMapMode(string(configuration.MindMap.Mode)).
		SetMindMapLayout(string(configuration.MindMap.Layout)).
		SetMindMapModel(configuration.MindMap.Model).
		SetMindMapReasoningEffort(configuration.MindMap.ReasoningEffort).
		SetMindMapMaxConcurrent(configuration.MindMap.MaxConcurrent).
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
		SetAgentMaxConcurrent(configuration.AgentMaxConcurrent).
		SetAgentWritableRoots(configuration.AgentWritableRoots).
		SetSendShortcut(string(configuration.SendShortcut)).
		SetCodexContextWindow(configuration.Codex.ContextWindow).
		SetMindMapEnabled(configuration.MindMap.Enabled).
		SetMindMapMode(string(configuration.MindMap.Mode)).
		SetMindMapLayout(string(configuration.MindMap.Layout)).
		SetMindMapModel(configuration.MindMap.Model).
		SetMindMapReasoningEffort(configuration.MindMap.ReasoningEffort).
		SetMindMapMaxConcurrent(configuration.MindMap.MaxConcurrent).
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

func (r *SettingRepository) MaxConcurrentAgents(ctx context.Context) (int, error) {
	configuration, err := r.GetSystemConfiguration(ctx)
	if err != nil {
		return 0, err
	}
	return configuration.AgentMaxConcurrent, nil
}

func (r *SettingRepository) AgentWritableRoots(ctx context.Context) ([]string, error) {
	configuration, err := r.GetSystemConfiguration(ctx)
	if err != nil {
		return nil, err
	}
	return append([]string(nil), configuration.AgentWritableRoots...), nil
}

func (r *SettingRepository) UpdateGeneralSettings(ctx context.Context, max int, writableRoots []string) error {
	if err := r.client.SystemConfiguration.UpdateOneID(globalSystemConfigurationID).
		SetAgentMaxConcurrent(max).
		SetAgentWritableRoots(writableRoots).
		Exec(ctx); err == nil {
		return nil
	} else if !ent.IsNotFound(err) {
		return fmt.Errorf("update agent concurrency limit: %w", err)
	}
	configuration := setting.DefaultSystemConfiguration()
	configuration.AgentMaxConcurrent = max
	configuration.AgentWritableRoots = append([]string(nil), writableRoots...)
	return r.SaveSystemConfiguration(ctx, configuration)
}

func (r *SettingRepository) MindMapConfiguration(ctx context.Context) (setting.MindMapConfiguration, error) {
	configuration, err := r.GetSystemConfiguration(ctx)
	if err != nil {
		return setting.MindMapConfiguration{}, err
	}
	return configuration.MindMap, nil
}
