#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

cd "$REPO_ROOT"

if ! command -v air &>/dev/null; then
  echo "air not found. Install it with:" >&2
  echo "  go install github.com/air-verse/air@latest" >&2
  echo "and ensure \$(go env GOPATH)/bin is on your PATH." >&2
  exit 1
fi

exec air -c .air.toml "$@"
