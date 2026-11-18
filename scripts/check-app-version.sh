#!/usr/bin/env bash
# Fails unless an .app bundle carries the expected version in its Info.plist
# and in the version stamped into its Go binary. create:app:bundle runs this
# before signing, so a release can't ship with mismatched versions.
set -euo pipefail

if [[ $# -ne 3 ]]; then
  echo "usage: $0 <path/to/App.app> <executable name> <expected version>" >&2
  exit 2
fi
bundle=$1
executable=$2
expected=$3
plist="$bundle/Contents/Info.plist"
binary="$bundle/Contents/MacOS/$executable"

status=0
for key in CFBundleShortVersionString CFBundleVersion; do
  got=$(plutil -extract "$key" raw -o - "$plist" 2>/dev/null) || got=""
  if [[ "$got" != "$expected" ]]; then
    echo "check-app-version: $key in $plist is '${got}', expected '$expected'" >&2
    status=1
  fi
done

# The Taskfile build stamps the version with -X at link time. -trimpath keeps
# -ldflags out of the build info, so ask the binary itself.
stamped=$("$binary" --version 2>/dev/null || true)
if [[ "$stamped" != "$expected" ]]; then
  echo "check-app-version: $binary is stamped '${stamped}', expected '$expected'" >&2
  status=1
fi

exit $status
