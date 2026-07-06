package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	AccessKey          string
	HTTPAddr           string
	DataDir            string
	CodexBin           string
	AgentMaxConcurrent int
	TursoDatabaseURL   string
	TursoAuthToken     string
}

func LoadFromEnv() Config {
	return Config{
		AccessKey:          os.Getenv("ANYCODE_ACCESS_KEY"),
		HTTPAddr:           envOrDefault("ANYCODE_HTTP_ADDR", ":8080"),
		DataDir:            envOrDefault("ANYCODE_DATA_DIR", "./data"),
		CodexBin:           envOrDefault("CODEX_BIN", "codex"),
		AgentMaxConcurrent: envIntOrDefault("ANYCODE_AGENT_MAX_CONCURRENT", 1),
		TursoDatabaseURL:   os.Getenv("TURSO_DATABASE_URL"),
		TursoAuthToken:     os.Getenv("TURSO_AUTH_TOKEN"),
	}
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
