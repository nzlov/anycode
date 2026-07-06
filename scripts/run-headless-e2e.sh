#!/usr/bin/env bash
set -euo pipefail

export ANYCODE_ACCESS_KEY="${ANYCODE_ACCESS_KEY:-test}"
export ANYCODE_CODEX_HOME="${ANYCODE_CODEX_HOME:-$HOME/.codex}"
export ANYCODE_E2E_SCREENSHOT_DIR="${ANYCODE_E2E_SCREENSHOT_DIR:-/tmp/anycode-headless}"

node scripts/headless-e2e.mjs --manage-docker "$@"
