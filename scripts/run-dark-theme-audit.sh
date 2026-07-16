#!/usr/bin/env bash
set -euo pipefail

: "${ANYCODE_ARTIFACT_DIR:?set ANYCODE_ARTIFACT_DIR}"

export ANYCODE_ACCESS_KEY="${ANYCODE_ACCESS_KEY:-test}"
export ANYCODE_CODEX_HOME="${ANYCODE_CODEX_HOME:-$HOME/.codex}"

node scripts/headless-e2e.mjs --manage-docker --dark-theme-audit "$@"
