package codexcli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestProbeReadsVersionAndCapabilities(t *testing.T) {
	bin := fakeCodex(t, `#!/bin/sh
case "$*" in
  "--version") echo "codex 1.2.3"; exit 0 ;;
  "exec --help") echo "exec help"; exit 0 ;;
  "exec resume --help") echo "resume help"; exit 0 ;;
esac
echo "unexpected $*" >&2
exit 2
`)

	got, err := New(bin).Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != "codex 1.2.3" {
		t.Fatalf("Version = %q", got.Version)
	}
	if !got.SupportsExec || !got.SupportsResume {
		t.Fatalf("capabilities = %+v", got)
	}
}

func TestProbeReturnsStructuredVersionError(t *testing.T) {
	bin := fakeCodex(t, `#!/bin/sh
echo "bad version" >&2
exit 42
`)

	_, err := New(bin).Probe(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	var probeErr *ProbeError
	if !errors.As(err, &probeErr) {
		t.Fatalf("expected ProbeError, got %T", err)
	}
	if probeErr.Code != "version_failed" {
		t.Fatalf("Code = %q", probeErr.Code)
	}
}

func TestNewUsesCODEXBIN(t *testing.T) {
	t.Setenv("CODEX_BIN", "/custom/codex")
	if got := New("").Bin(); got != "/custom/codex" {
		t.Fatalf("Bin() = %q", got)
	}
}

func fakeCodex(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake is not portable on windows")
	}
	path := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
