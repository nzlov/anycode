# Deploy AnyCode as a systemd user service

[中文](systemd.md) | [English](systemd.en.md)

GitHub Releases provide Linux binary archives as well as Debian, Red Hat, and Arch Linux packages. They all contain the `anycode` executable with the web UI embedded, a systemd user unit, and an environment-file example.

AnyCode runs as the current login user and directly reuses that user's `HOME`, `~/.codex` credentials, and project access. No additional system account is created. The Codex CLI is still required on the host. `cloudflared` is needed only for Cloudflare quick tunnels.

## Supported platforms

- Linux x86-64: `amd64`
- Linux ARM64: `arm64`
- A distribution that uses systemd and glibc (the packages do not support musl)

Distribution packages use their native architecture names:

| Platform | Debian  | Red Hat   | Arch Linux |
| -------- | ------- | --------- | ---------- |
| x86-64   | `amd64` | `x86_64`  | `x86_64`   |
| ARM64    | `arm64` | `aarch64` | `aarch64`  |

The `aarch64` package targets Arch Linux ARM; the official Arch Linux repositories themselves support only x86-64.

## Install a distribution package

The examples below use `1.2.3` and x86-64. Select the command group for your system; do not install multiple formats.

Debian or Ubuntu:

```bash
VERSION=1.2.3
ARCH=amd64
curl -fLO "https://github.com/nzlov/anycode/releases/download/v${VERSION}/anycode_${VERSION}_${ARCH}.deb"
curl -fLO "https://github.com/nzlov/anycode/releases/download/v${VERSION}/checksums.txt"
sha256sum --check --ignore-missing checksums.txt
sudo apt install "./anycode_${VERSION}_${ARCH}.deb"
```

RHEL, Fedora, or another RPM-based system:

```bash
VERSION=1.2.3
ARCH=x86_64
curl -fLO "https://github.com/nzlov/anycode/releases/download/v${VERSION}/anycode-${VERSION}-1.${ARCH}.rpm"
curl -fLO "https://github.com/nzlov/anycode/releases/download/v${VERSION}/checksums.txt"
sha256sum --check --ignore-missing checksums.txt
sudo dnf install "./anycode-${VERSION}-1.${ARCH}.rpm"
```

Arch Linux or Arch Linux ARM:

```bash
VERSION=1.2.3
ARCH=x86_64
curl -fLO "https://github.com/nzlov/anycode/releases/download/v${VERSION}/anycode-${VERSION}-1-${ARCH}.pkg.tar.zst"
curl -fLO "https://github.com/nzlov/anycode/releases/download/v${VERSION}/checksums.txt"
sha256sum --check --ignore-missing checksums.txt
sudo pacman -U "./anycode-${VERSION}-1-${ARCH}.pkg.tar.zst"
```

Distribution packages install the Git and systemd dependencies and place these files under package-manager control:

- `/usr/bin/anycode`
- `/usr/lib/systemd/user/anycode.service`
- `/usr/share/anycode/anycode.env.example`

Distribution packages attached to a Release are not signed with distribution repository keys; verify downloads with `checksums.txt` from the same Release.

Create a private configuration for the current user:

```bash
install -d -m 0700 ~/.config/anycode
install -m 0600 /usr/share/anycode/anycode.env.example ~/.config/anycode/anycode.env
```

Package installation and upgrades do not overwrite `~/.config/anycode/anycode.env` or automatically start a service for any user.

## Install the binary archive

The example below uses `v1.2.3` and `amd64`. Replace both values with the target Release and host architecture.

```bash
VERSION=v1.2.3
ARCH=amd64
curl -fLO "https://github.com/nzlov/anycode/releases/download/${VERSION}/anycode-${VERSION}-linux-${ARCH}.tar.gz"
curl -fLO "https://github.com/nzlov/anycode/releases/download/${VERSION}/checksums.txt"
sha256sum --check --ignore-missing checksums.txt
tar -xzf "anycode-${VERSION}-linux-${ARCH}.tar.gz"
cd "anycode-${VERSION}-linux-${ARCH}"
```

Install the executable, then install the unit and configuration for the current user:

```bash
sudo install -m 0755 anycode /usr/bin/anycode
install -D -m 0644 systemd/anycode.service ~/.config/systemd/user/anycode.service
install -D -m 0600 systemd/anycode.env.example ~/.config/anycode/anycode.env
```

## Configure Codex and AnyCode

First verify that the current user is signed in to Codex:

```bash
codex login --device-auth
codex login status
command -v codex
```

The user service searches `~/.local/bin`, `/usr/local/bin`, and `/usr/bin` by default. If `command -v codex` reports another directory, put its absolute path in `CODEX_BIN` inside `~/.config/anycode/anycode.env`.

Edit `~/.config/anycode/anycode.env` and replace at least `ANYCODE_ACCESS_KEY`. The environment file is not a shell script: use plain `KEY=value` assignments without `export`, command substitution, or references to other variables.

The service stores its database, attachments, artifacts, and worktrees in `~/.local/share/anycode` by default. Projects can be located anywhere accessible to the current user; add them in the web UI using their real host paths.

The service listens only on `127.0.0.1:8080` by default. For remote access, keep the loopback listener and expose it through a trusted TLS reverse proxy. AnyCode inherits the current user's file permissions, so its access key grants the ability to read those directories and execute project commands; it does not provide transport encryption.

## Start and inspect the service

```bash
systemctl --user daemon-reload
systemctl --user enable --now anycode
systemctl --user status anycode
curl --fail http://127.0.0.1:8080/healthz
```

Follow the logs with:

```bash
journalctl --user -u anycode -f
```

If startup logs contain `probe codex cli`, check `CODEX_BIN` and the current user's `codex login status`.

A user service normally starts after that user logs in. To run it after boot without an interactive login, explicitly enable lingering:

```bash
sudo loginctl enable-linger "$USER"
```

Run `sudo loginctl disable-linger "$USER"` when it is no longer needed. Lingering lets the user's systemd manager continue running while logged out, so enable it only for a trusted user.

## Upgrade

Back up `~/.local/share/anycode` and `~/.config/anycode` before upgrading.

When using a distribution package, download and verify the new Release, then run the corresponding `apt install`, `dnf install`, or `pacman -U` command again. The user running the service must then load the new unit and restart:

```bash
systemctl --user daemon-reload
systemctl --user restart anycode
systemctl --user status anycode
```

When using the binary archive, replace `/usr/bin/anycode` first. If the unit changed, copy it to `~/.config/systemd/user/anycode.service`, then run the user-service commands above. The environment-file example never overwrites the existing configuration automatically.

## Uninstall

First stop and disable the service as every user currently running AnyCode:

```bash
systemctl --user disable --now anycode
```

When using a distribution package, select the command for the current system:

```bash
sudo apt remove anycode
sudo dnf remove anycode
sudo pacman -R anycode
```

When using the binary archive:

```bash
rm ~/.config/systemd/user/anycode.service
systemctl --user daemon-reload
sudo rm /usr/bin/anycode
```

Uninstallation does not automatically delete `~/.config/anycode`, `~/.local/share/anycode`, or project directories. Remove them separately only after confirming that they are no longer needed.
