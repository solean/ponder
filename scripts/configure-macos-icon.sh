#!/usr/bin/env bash
set -euo pipefail

# Run after copying Info.plist/resources and before signing the bundle.
bundle=${1:?Expected app bundle path}
plist="$bundle/Contents/Info.plist"
resources="$bundle/Contents/Resources"
digest=$(shasum -a 256 "$resources/icons.icns" | cut -c 1-16)
icon_name="PonderIcon-$digest"
find "$resources" -maxdepth 1 -type f -name 'PonderIcon-*.icns' ! -name "$icon_name.icns" -delete
cp "$resources/icons.icns" "$resources/$icon_name.icns"
/usr/libexec/PlistBuddy -c "Set :CFBundleIconFile $icon_name" "$plist"

# An asset-catalog name is only valid when the catalog is actually bundled.
if [[ ! -f "$resources/Assets.car" ]]; then
  if /usr/libexec/PlistBuddy -c 'Print :CFBundleIconName' "$plist" >/dev/null 2>&1; then
    /usr/libexec/PlistBuddy -c 'Delete :CFBundleIconName' "$plist"
  fi
fi
