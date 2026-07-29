#!/bin/sh
set -eu

install_extra_packages() {
  [ -n "${ANYCODE_EXTRA_PACKAGES:-}" ] || return 0

  set -f
  set -- $ANYCODE_EXTRA_PACKAGES
  needs_install=0
  for package do
    case "$package" in
      *[!a-zA-Z0-9@._+-]*)
        echo "invalid package name in ANYCODE_EXTRA_PACKAGES: $package" >&2
        return 1
        ;;
    esac
    if ! pacman -Q "$package" >/dev/null 2>&1; then
      needs_install=1
    fi
  done

  [ "$needs_install" -eq 1 ] || return 0
  pacman -Syu --noconfirm --needed -- "$@"
  pacman -Scc --noconfirm
}

if [ "$(id -u)" = "0" ]; then
  install_extra_packages
  install -d -o anycode -g anycode /app /home/anycode/.anycode /home/anycode/.codex
  chown -R anycode:anycode /home/anycode
  exec setpriv --reuid="${ANYCODE_UID:-1000}" --regid="${ANYCODE_GID:-1000}" --init-groups -- "$@"
fi

[ -z "${ANYCODE_EXTRA_PACKAGES:-}" ] || {
  echo "ANYCODE_EXTRA_PACKAGES requires the container to start as root" >&2
  exit 1
}

exec "$@"
