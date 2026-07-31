#!/bin/sh
set -eu

update_packages() {
  set -f
  set -- ${ANYCODE_EXTRA_PACKAGES:-}
  for package do
    case "$package" in
      *[!a-zA-Z0-9@._+-]*)
        echo "invalid package name in ANYCODE_EXTRA_PACKAGES: $package" >&2
        return 1
        ;;
    esac
  done

  if [ -n "${PACMAN_MIRROR:-}" ]; then
    printf 'Server = %s\n' "$PACMAN_MIRROR" | tee /etc/pacman.d/mirrorlist >/dev/null
  fi

  pacman -Syu --noconfirm
  [ "$#" -eq 0 ] || pacman -S --noconfirm --needed -- "$@"
  pacman -Scc --noconfirm
}

run_init_script() {
  [ -n "${ANYCODE_INIT_SCRIPT:-}" ] || return 0
  /bin/sh -c "$ANYCODE_INIT_SCRIPT"
}

if [ "$(id -u)" = "0" ]; then
  update_packages
  install -d -o anycode -g anycode /app /home/anycode/.anycode /home/anycode/.codex
  run_init_script
  chown -R anycode:anycode /home/anycode
  exec setpriv --reuid="${ANYCODE_UID:-1000}" --regid="${ANYCODE_GID:-1000}" --init-groups -- "$@"
fi

[ -z "${ANYCODE_EXTRA_PACKAGES:-}" ] || {
  echo "ANYCODE_EXTRA_PACKAGES requires the container to start as root" >&2
  exit 1
}

[ -z "${ANYCODE_INIT_SCRIPT:-}" ] || {
  echo "ANYCODE_INIT_SCRIPT requires the container to start as root" >&2
  exit 1
}

exec "$@"
