package codexcli

import (
	"context"
	"fmt"
	"strings"

	"github.com/nzlov/anycode/internal/domain/process"
)

func (c *Client) Probe(ctx context.Context) (process.CodexCapabilities, error) {
	runtime, err := c.appServer(ctx)
	if err != nil {
		return process.CodexCapabilities{}, &ProbeError{Code: "app_server_failed", Bin: c.Bin(), Args: []string{"app-server", "--stdio"}, Err: err}
	}
	models, err := runtime.models(ctx)
	if err != nil {
		return process.CodexCapabilities{}, &ProbeError{Code: "models_failed", Bin: c.Bin(), Err: err}
	}
	status, imageGeneration, err := runtime.feature(ctx, "image_generation")
	if err != nil {
		return process.CodexCapabilities{}, &ProbeError{Code: "features_failed", Bin: c.Bin(), Err: err}
	}
	return process.CodexCapabilities{
		Version: runtime.userAgent, SupportsAppServer: true,
		SupportsImageGeneration: imageGeneration, ImageGenerationStatus: status, Models: models,
	}, nil
}

func (r *appServerRuntime) models(ctx context.Context) ([]process.CodexModel, error) {
	var response struct {
		Data []struct {
			ID                     string `json:"id"`
			Model                  string `json:"model"`
			DisplayName            string `json:"displayName"`
			DefaultReasoningEffort string `json:"defaultReasoningEffort"`
			Hidden                 bool   `json:"hidden"`
			Supported              []struct {
				Effort      string `json:"reasoningEffort"`
				Description string `json:"description"`
			} `json:"supportedReasoningEfforts"`
		} `json:"data"`
	}
	if err := r.request(ctx, "model/list", map[string]any{"includeHidden": false, "limit": 100}, &response); err != nil {
		return nil, fmt.Errorf("list codex models: %w", err)
	}
	models := make([]process.CodexModel, 0, len(response.Data))
	for _, item := range response.Data {
		if item.Hidden {
			continue
		}
		slug := strings.TrimSpace(item.Model)
		if slug == "" {
			slug = strings.TrimSpace(item.ID)
		}
		if slug == "" {
			continue
		}
		levels := make([]process.CodexReasoningLevel, 0, len(item.Supported))
		for _, level := range item.Supported {
			if level.Effort != "" {
				levels = append(levels, process.CodexReasoningLevel{Effort: level.Effort, Description: level.Description})
			}
		}
		models = append(models, process.CodexModel{
			Slug: slug, DisplayName: item.DisplayName, DefaultReasoningLevel: item.DefaultReasoningEffort, SupportedReasoningLevels: levels,
		})
	}
	return models, nil
}

func (r *appServerRuntime) feature(ctx context.Context, name string) (string, bool, error) {
	var response struct {
		Data []struct {
			Name    string `json:"name"`
			Stage   string `json:"stage"`
			Enabled bool   `json:"enabled"`
		} `json:"data"`
	}
	if err := r.request(ctx, "experimentalFeature/list", map[string]any{"limit": 100}, &response); err != nil {
		return "", false, fmt.Errorf("list codex features: %w", err)
	}
	for _, feature := range response.Data {
		if feature.Name == name {
			return feature.Stage, feature.Enabled, nil
		}
	}
	return "unavailable", false, nil
}
