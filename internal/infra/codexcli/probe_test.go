package codexcli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/nzlov/anycode/internal/domain/process"
)

func TestProbeReadsVersionAndCapabilities(t *testing.T) {
	bin := fakeCodex(t, `#!/bin/sh
case "$*" in
  "--version") echo "codex 1.2.3"; exit 0 ;;
  "exec --help") echo "exec help"; exit 0 ;;
  "exec --help --strict-config -c mcp_servers.anycode.tool_timeout_sec=86400") echo "exec timeout config help"; exit 0 ;;
  "exec resume --help") echo "resume help"; exit 0 ;;
  "debug models") echo '{"models":[{"slug":"gpt-5.6-sol","display_name":"GPT-5.6-Sol","default_reasoning_level":"low","supported_reasoning_levels":[{"effort":"low","description":"Fast"},{"effort":"high","description":"Deep"}],"visibility":"list","priority":1}]}'; exit 0 ;;
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
	if !got.SupportsExec || !got.SupportsResume || !got.SupportsMCPToolTimeout {
		t.Fatalf("capabilities = %+v", got)
	}
}

func TestProbeReadsModelCatalog(t *testing.T) {
	bin := fakeCodex(t, `#!/bin/sh
case "$*" in
  "--version") echo "codex 1.2.3"; exit 0 ;;
  "exec --help") echo "exec help"; exit 0 ;;
  "exec --help --strict-config -c mcp_servers.anycode.tool_timeout_sec=86400") echo "exec timeout config help"; exit 0 ;;
  "exec resume --help") echo "resume help"; exit 0 ;;
  "debug models") cat <<'JSON'
{"models":[
  {"slug":"hidden-model","display_name":"Hidden","default_reasoning_level":"low","supported_reasoning_levels":[{"effort":"low","description":"Fast"}],"visibility":"hidden","priority":0},
  {"slug":"gpt-5.6-sol","display_name":"GPT-5.6-Sol","default_reasoning_level":"low","supported_reasoning_levels":[{"effort":"low","description":"Fast responses"},{"effort":"ultra","description":"Delegated maximum"}],"visibility":"list","priority":1}
]}
JSON
    exit 0 ;;
esac
echo "unexpected $*" >&2
exit 2
`)

	got, err := New(bin).Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Models) != 1 {
		t.Fatalf("Models = %+v", got.Models)
	}
	model := got.Models[0]
	if model.Slug != "gpt-5.6-sol" || model.DisplayName != "GPT-5.6-Sol" || model.DefaultReasoningLevel != "low" {
		t.Fatalf("model = %+v", model)
	}
	if len(model.SupportedReasoningLevels) != 2 || model.SupportedReasoningLevels[1].Effort != "ultra" {
		t.Fatalf("reasoning levels = %+v", model.SupportedReasoningLevels)
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

func TestProbeDisablesMCPToolTimeoutWhenStrictConfigRejectsIt(t *testing.T) {
	bin := fakeCodex(t, `#!/bin/sh
case "$*" in
  "--version") echo "codex 1.2.3"; exit 0 ;;
  "exec --help") echo "exec help"; exit 0 ;;
  "exec resume --help") echo "resume help"; exit 0 ;;
  "debug models") echo '{"models":[{"slug":"gpt-test","visibility":"list"}]}' ; exit 0 ;;
  "exec --help --strict-config -c mcp_servers.anycode.tool_timeout_sec=86400") echo "unknown field" >&2; exit 2 ;;
esac
exit 2
`)
	client := New(bin, WithMCP("http://127.0.0.1:8080", "secret"))
	capabilities, err := client.Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if capabilities.SupportsMCPToolTimeout {
		t.Fatal("tool timeout capability = true")
	}
	args := strings.Join(client.buildStartArgs(process.CodexStartInput{SessionID: "session-1"}), " ")
	if strings.Contains(args, "tool_timeout_sec") {
		t.Fatalf("unsupported timeout was injected: %s", args)
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
