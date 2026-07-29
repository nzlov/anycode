package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDockerEntrypointInstallsExtraPackagesBeforeDroppingPrivileges(t *testing.T) {
	log, output, err := runDockerEntrypoint(t, "jq python")
	if err != nil {
		t.Fatalf("entrypoint failed: %v\n%s", err, output)
	}

	install := "pacman -Syu --noconfirm --needed -- jq python"
	dropPrivileges := "setpriv --reuid=1234 --regid=1235 --init-groups -- anycode"
	if !strings.Contains(log, install) || !strings.Contains(log, dropPrivileges) {
		t.Fatalf("entrypoint log = %q", log)
	}
	if strings.Index(log, install) > strings.Index(log, dropPrivileges) {
		t.Fatalf("packages installed after dropping privileges: %q", log)
	}
}

func TestDockerEntrypointRejectsInvalidExtraPackageName(t *testing.T) {
	log, output, err := runDockerEntrypoint(t, "jq;touch")
	if err == nil {
		t.Fatalf("entrypoint accepted invalid package name; log = %q", log)
	}
	if !strings.Contains(output, "invalid package name") {
		t.Fatalf("entrypoint output = %q", output)
	}
	if strings.Contains(log, "pacman -Syu") || strings.Contains(log, "setpriv") {
		t.Fatalf("entrypoint continued after invalid package name: %q", log)
	}
}

func runDockerEntrypoint(t *testing.T, packages string) (string, string, error) {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "commands.log")
	writeEntrypointStub(t, dir, "id", "printf '0\\n'")
	writeEntrypointStub(t, dir, "install", "printf 'install %s\\n' \"$*\" >> \"$ENTRYPOINT_TEST_LOG\"")
	writeEntrypointStub(t, dir, "chown", "printf 'chown %s\\n' \"$*\" >> \"$ENTRYPOINT_TEST_LOG\"")
	writeEntrypointStub(t, dir, "setpriv", "printf 'setpriv %s\\n' \"$*\" >> \"$ENTRYPOINT_TEST_LOG\"")
	writeEntrypointStub(t, dir, "pacman", `
printf 'pacman %s\n' "$*" >> "$ENTRYPOINT_TEST_LOG"
if [ "${1:-}" = "-Q" ]; then
  exit 1
fi`)

	command := exec.Command("sh", filepath.Join("..", "docker-entrypoint.sh"), "anycode")
	command.Env = []string{
		"PATH=" + dir,
		"ENTRYPOINT_TEST_LOG=" + logPath,
		"ANYCODE_EXTRA_PACKAGES=" + packages,
		"ANYCODE_UID=1234",
		"ANYCODE_GID=1235",
	}
	output, err := command.CombinedOutput()
	log, readErr := os.ReadFile(logPath)
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatal(readErr)
	}
	return string(log), string(output), err
}

func writeEntrypointStub(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}
