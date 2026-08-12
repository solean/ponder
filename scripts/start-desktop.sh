#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if ! command -v wails3 &>/dev/null; then
  echo "wails3 not found. Install it with:" >&2
  echo "  go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.5" >&2
  echo "and ensure \$(go env GOPATH)/bin is on your PATH." >&2
  exit 1
fi

cd "$REPO_ROOT"
export PONDER_DB_PATH="$REPO_ROOT/data/ponder.db"
exec wails3 dev "$@"
