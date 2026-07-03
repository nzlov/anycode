package config

import "testing"

func TestLoadFromEnvReadsAccessKeyAndDefaults(t *testing.T) {
	t.Setenv("ANYCODE_ACCESS_KEY", "secret")
	t.Setenv("ANYCODE_HTTP_ADDR", "")
	t.Setenv("ANYCODE_DATA_DIR", "")
	t.Setenv("CODEX_BIN", "")

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
}
