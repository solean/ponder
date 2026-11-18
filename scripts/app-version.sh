#!/usr/bin/env bash
# Prints the app version: `info.version` in build/config.yml. That value is the
# single source of truth. The Taskfiles stamp it into the Go binary, the
# Info.plist, and the HTTP User-Agent.
set -euo pipefail

config="${1:-$(dirname "$0")/../build/config.yml}"

version=$(awk '
  /^info:/ { in_info = 1; next }
  in_info && /^[^[:space:]#]/ { exit }
  in_info && $1 == "version:" {
    sub(/^[[:space:]]*version:[[:space:]]*/, "")
    sub(/[[:space:]]+#.*$/, "")
    gsub(/["\047]/, "")
    print
    exit
  }
' "$config")

if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "app-version: info.version in $config must be x.y.z, got '${version}'" >&2
  exit 1
fi
printf '%s\n' "$version"
