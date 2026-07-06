#!/bin/sh
set -eu

if [ "$(id -u)" = "0" ]; then
  install -d -o anycode -g anycode /app /data /home/anycode/.codex
  chown -R anycode:anycode /data /home/anycode
  exec setpriv --reuid="${ANYCODE_UID:-1000}" --regid="${ANYCODE_GID:-1000}" --init-groups -- "$@"
fi

exec "$@"
