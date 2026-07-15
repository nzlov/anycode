package config

import (
	"strings"
	"testing"
)

func TestLoadFromEnvReadsAccessKeyAndDefaults(t *testing.T) {
	t.Setenv("ANYCODE_ACCESS_KEY", "secret")
	t.Setenv("ANYCODE_HTTP_ADDR", "")
	t.Setenv("ANYCODE_DATA_DIR", "")
	t.Setenv("CODEX_BIN", "")
	t.Setenv("ANYCODE_AGENT_MAX_CONCURRENT", "")

	got, err := LoadFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessKey != "secret" {
		t.Fatalf("AccessKey = %q", got.AccessKey)
	}
	if got.HTTPAddr != ":8080" {
		t.Fatalf("HTTPAddr = %q", got.HTTPAddr)
	}
	if got.DataDir != "./data" {
		t.Fatalf("DataDir = %q", got.DataDir)
	}
	if got.CodexBin != "codex" {
		t.Fatalf("CodexBin = %q", got.CodexBin)
	}
	if got.AgentMaxConcurrent != 1 {
		t.Fatalf("AgentMaxConcurrent = %d", got.AgentMaxConcurrent)
	}
	if got.ArtifactMaxFileBytes != 512<<20 || got.ArtifactMaxSessionBytes != 10<<30 || got.ArtifactPreviewMaxBytes != 128<<20 {
		t.Fatalf("artifact limits = %d/%d/%d", got.ArtifactMaxFileBytes, got.ArtifactMaxSessionBytes, got.ArtifactPreviewMaxBytes)
	}
}

func TestLoadFromEnvReadsAgentMaxConcurrent(t *testing.T) {
	t.Setenv("ANYCODE_AGENT_MAX_CONCURRENT", "3")

	got, err := LoadFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if got.AgentMaxConcurrent != 3 {
		t.Fatalf("AgentMaxConcurrent = %d", got.AgentMaxConcurrent)
	}
}

func TestLoadFromEnvReadsArtifactLimits(t *testing.T) {
	t.Setenv("ANYCODE_ARTIFACT_MAX_FILE_BYTES", "100")
	t.Setenv("ANYCODE_ARTIFACT_MAX_SESSION_BYTES", "200")
	t.Setenv("ANYCODE_ARTIFACT_PREVIEW_MAX_BYTES", "50")
	got, err := LoadFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if got.ArtifactMaxFileBytes != 100 || got.ArtifactMaxSessionBytes != 200 || got.ArtifactPreviewMaxBytes != 50 {
		t.Fatalf("artifact limits = %#v", got)
	}
}

func TestLoadFromEnvRejectsInvalidArtifactLimits(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		value   string
		message string
	}{
		{"invalid", "ANYCODE_ARTIFACT_MAX_FILE_BYTES", "many", "positive base-10 integer"},
		{"zero", "ANYCODE_ARTIFACT_MAX_FILE_BYTES", "0", "positive base-10 integer"},
		{"overflow", "ANYCODE_ARTIFACT_MAX_FILE_BYTES", "999999999999999999999", "positive base-10 integer"},
		{"session below file", "ANYCODE_ARTIFACT_MAX_SESSION_BYTES", "1", "greater than or equal"},
		{"preview above file", "ANYCODE_ARTIFACT_PREVIEW_MAX_BYTES", "1073741824", "less than or equal"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(test.key, test.value)
			_, err := LoadFromEnv()
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("LoadFromEnv() error = %v", err)
			}
		})
	}
}
