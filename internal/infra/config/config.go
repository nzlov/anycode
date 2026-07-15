package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	AccessKey               string
	HTTPAddr                string
	DataDir                 string
	CodexBin                string
	AgentMaxConcurrent      int
	TursoDatabaseURL        string
	TursoAuthToken          string
	ArtifactMaxFileBytes    int64
	ArtifactMaxSessionBytes int64
	ArtifactPreviewMaxBytes int64
	PlaywrightMCPBin        string
	ChromiumBin             string
}

func LoadFromEnv() (Config, error) {
	maxFile, err := envPositiveInt64OrDefault("ANYCODE_ARTIFACT_MAX_FILE_BYTES", 512<<20)
	if err != nil {
		return Config{}, err
	}
	maxSession, err := envPositiveInt64OrDefault("ANYCODE_ARTIFACT_MAX_SESSION_BYTES", 10<<30)
	if err != nil {
		return Config{}, err
	}
	previewMax, err := envPositiveInt64OrDefault("ANYCODE_ARTIFACT_PREVIEW_MAX_BYTES", 128<<20)
	if err != nil {
		return Config{}, err
	}
	if maxSession < maxFile {
		return Config{}, fmt.Errorf("ANYCODE_ARTIFACT_MAX_SESSION_BYTES must be greater than or equal to ANYCODE_ARTIFACT_MAX_FILE_BYTES")
	}
	if previewMax > maxFile {
		return Config{}, fmt.Errorf("ANYCODE_ARTIFACT_PREVIEW_MAX_BYTES must be less than or equal to ANYCODE_ARTIFACT_MAX_FILE_BYTES")
	}
	return Config{
		AccessKey:               os.Getenv("ANYCODE_ACCESS_KEY"),
		HTTPAddr:                envOrDefault("ANYCODE_HTTP_ADDR", ":8080"),
		DataDir:                 envOrDefault("ANYCODE_DATA_DIR", "./data"),
		CodexBin:                envOrDefault("CODEX_BIN", "codex"),
		AgentMaxConcurrent:      envIntOrDefault("ANYCODE_AGENT_MAX_CONCURRENT", 1),
		TursoDatabaseURL:        os.Getenv("TURSO_DATABASE_URL"),
		TursoAuthToken:          os.Getenv("TURSO_AUTH_TOKEN"),
		ArtifactMaxFileBytes:    maxFile,
		ArtifactMaxSessionBytes: maxSession,
		ArtifactPreviewMaxBytes: previewMax,
		PlaywrightMCPBin:        strings.TrimSpace(os.Getenv("PLAYWRIGHT_MCP_BIN")),
		ChromiumBin:             envOrDefault("CHROMIUM_BIN", "/usr/bin/chromium"),
	}, nil
}

func envPositiveInt64OrDefault(key string, fallback int64) (int64, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive base-10 integer", key)
	}
	return parsed, nil
}

func envIntOrDefault(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envOrDefault(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
