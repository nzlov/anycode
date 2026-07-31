package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDockerEntrypointInstallsExtraPackagesBeforeDroppingPrivileges(t *testing.T) {
	log, output, err := runDockerEntrypoint(t, "0", "jq python", "", "")
	if err != nil {
		t.Fatalf("entrypoint failed: %v\n%s", err, output)
	}

	update := "pacman -Syu --noconfirm"
	install := "pacman -S --noconfirm --needed -- jq python"
	clean := "pacman -Scc --noconfirm"
	dropPrivileges := "setpriv --reuid=1234 --regid=1235 --init-groups -- anycode"
	for _, entry := range []string{update, install, clean, dropPrivileges} {
		if !strings.Contains(log, entry) {
			t.Fatalf("entrypoint log = %q, want %q", log, entry)
		}
	}
	if strings.Index(log, update) > strings.Index(log, install) ||
		strings.Index(log, install) > strings.Index(log, clean) ||
		strings.Index(log, clean) > strings.Index(log, dropPrivileges) {
		t.Fatalf("unexpected package update order: %q", log)
	}
}

func TestDockerEntrypointUpdatesPackagesWithoutExtras(t *testing.T) {
	log, output, err := runDockerEntrypoint(t, "0", "", "", "")
	if err != nil {
		t.Fatalf("entrypoint failed: %v\n%s", err, output)
	}

	update := "pacman -Syu --noconfirm"
	clean := "pacman -Scc --noconfirm"
	dropPrivileges := "setpriv --reuid=1234 --regid=1235 --init-groups -- anycode"
	for _, entry := range []string{update, clean, dropPrivileges} {
		if !strings.Contains(log, entry) {
			t.Fatalf("entrypoint log = %q, want %q", log, entry)
		}
	}
	if strings.Index(log, update) > strings.Index(log, clean) || strings.Index(log, clean) > strings.Index(log, dropPrivileges) {
		t.Fatalf("unexpected package update order: %q", log)
	}
}

func TestDockerEntrypointConfiguresPacmanMirrorBeforeUpdatingPackages(t *testing.T) {
	log, output, err := runDockerEntrypoint(t, "0", "", "", "https://mirror.example.org/$repo/os/$arch")
	if err != nil {
		t.Fatalf("entrypoint failed: %v\n%s", err, output)
	}

	mirror := "tee /etc/pacman.d/mirrorlist <Server = https://mirror.example.org/$repo/os/$arch>"
	update := "pacman -Syu --noconfirm"
	for _, entry := range []string{mirror, update} {
		if !strings.Contains(log, entry) {
			t.Fatalf("entrypoint log = %q, want %q", log, entry)
		}
	}
	if strings.Index(log, mirror) > strings.Index(log, update) {
		t.Fatalf("pacman mirror was configured after package update: %q", log)
	}
}

func TestDockerEntrypointRejectsInvalidExtraPackageName(t *testing.T) {
	log, output, err := runDockerEntrypoint(t, "0", "jq;touch", "", "")
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

func TestDockerEntrypointRunsInitScriptAsRootBeforeDroppingPrivileges(t *testing.T) {
	initScript := `printf 'init uid=%s\n' "$(id -u)" >> "$ENTRYPOINT_TEST_LOG"`
	log, output, err := runDockerEntrypoint(t, "0", "jq", initScript, "")
	if err != nil {
		t.Fatalf("entrypoint failed: %v\n%s", err, output)
	}

	update := "pacman -Syu --noconfirm"
	install := "pacman -S --noconfirm --needed -- jq"
	init := "init uid=0"
	dropPrivileges := "setpriv --reuid=1234 --regid=1235 --init-groups -- anycode"
	for _, entry := range []string{update, install, init, dropPrivileges} {
		if !strings.Contains(log, entry) {
			t.Fatalf("entrypoint log = %q, want %q", log, entry)
		}
	}
	if strings.Index(log, update) > strings.Index(log, install) ||
		strings.Index(log, install) > strings.Index(log, init) ||
		strings.Index(log, init) > strings.Index(log, dropPrivileges) {
		t.Fatalf("unexpected initialization order: %q", log)
	}
}

func TestDockerEntrypointStopsWhenInitScriptFails(t *testing.T) {
	log, _, err := runDockerEntrypoint(t, "0", "", "printf 'init failed\\n' >&2; exit 23", "")
	if err == nil {
		t.Fatalf("entrypoint ignored init script failure; log = %q", log)
	}
	if strings.Contains(log, "setpriv") {
		t.Fatalf("entrypoint dropped privileges after init script failure: %q", log)
	}
}

func TestDockerEntrypointRejectsInitScriptWithoutRoot(t *testing.T) {
	log, output, err := runDockerEntrypoint(t, "1000", "", "printf 'unexpected\\n' >> \"$ENTRYPOINT_TEST_LOG\"", "")
	if err == nil {
		t.Fatalf("entrypoint accepted init script without root; log = %q", log)
	}
	if !strings.Contains(output, "ANYCODE_INIT_SCRIPT requires the container to start as root") {
		t.Fatalf("entrypoint output = %q", output)
	}
	if strings.Contains(log, "unexpected") {
		t.Fatalf("entrypoint ran init script without root: %q", log)
	}
}

func runDockerEntrypoint(t *testing.T, uid string, packages string, initScript string, pacmanMirror string) (string, string, error) {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "commands.log")
	writeEntrypointStub(t, dir, "id", "printf '%s\\n' \"$ENTRYPOINT_TEST_UID\"")
	writeEntrypointStub(t, dir, "install", "printf 'install %s\\n' \"$*\" >> \"$ENTRYPOINT_TEST_LOG\"")
	writeEntrypointStub(t, dir, "chown", "printf 'chown %s\\n' \"$*\" >> \"$ENTRYPOINT_TEST_LOG\"")
	writeEntrypointStub(t, dir, "setpriv", "printf 'setpriv %s\\n' \"$*\" >> \"$ENTRYPOINT_TEST_LOG\"")
	writeEntrypointStub(t, dir, "pacman", `printf 'pacman %s\n' "$*" >> "$ENTRYPOINT_TEST_LOG"`)
	writeEntrypointStub(t, dir, "tee", `IFS= read -r input; printf 'tee %s <%s>\n' "$*" "$input" >> "$ENTRYPOINT_TEST_LOG"`)

	command := exec.Command("sh", filepath.Join("..", "docker-entrypoint.sh"), "anycode")
	command.Env = []string{
		"PATH=" + dir,
		"ENTRYPOINT_TEST_LOG=" + logPath,
		"ENTRYPOINT_TEST_UID=" + uid,
		"ANYCODE_EXTRA_PACKAGES=" + packages,
		"ANYCODE_INIT_SCRIPT=" + initScript,
		"PACMAN_MIRROR=" + pacmanMirror,
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
