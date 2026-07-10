package config

import "testing"

func TestLoadFromEnvReadsAccessKeyAndDefaults(t *testing.T) {
	t.Setenv("ANYCODE_ACCESS_KEY", "secret")
	t.Setenv("ANYCODE_HTTP_ADDR", "")
	t.Setenv("ANYCODE_DATA_DIR", "")
	t.Setenv("CODEX_BIN", "")
	t.Setenv("ANYCODE_AGENT_MAX_CONCURRENT", "")

	got := LoadFromEnv()
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
}

func TestLoadFromEnvReadsAgentMaxConcurrent(t *testing.T) {
	t.Setenv("ANYCODE_AGENT_MAX_CONCURRENT", "3")

	got := LoadFromEnv()
	if got.AgentMaxConcurrent != 3 {
		t.Fatalf("AgentMaxConcurrent = %d", got.AgentMaxConcurrent)
	}
}
