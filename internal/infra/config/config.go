package config

import "os"

type Config struct {
	AccessKey        string
	HTTPAddr         string
	DataDir          string
	CodexBin         string
	TursoDatabaseURL string
	TursoAuthToken   string
}

func LoadFromEnv() Config {
	return Config{
		AccessKey:        os.Getenv("ANYCODE_ACCESS_KEY"),
		HTTPAddr:         envOrDefault("ANYCODE_HTTP_ADDR", ":8080"),
		DataDir:          envOrDefault("ANYCODE_DATA_DIR", "./data"),
		CodexBin:         envOrDefault("CODEX_BIN", "codex"),
		TursoDatabaseURL: os.Getenv("TURSO_DATABASE_URL"),
		TursoAuthToken:   os.Getenv("TURSO_AUTH_TOKEN"),
	}
}

func envOrDefault(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
