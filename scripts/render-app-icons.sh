#!/usr/bin/env bash
set -euo pipefail

# Run on macOS with Icon Composer installed alongside Xcode. This generates
# native renders for the Dock picker, its thumbnails, and the white fallback.
root=$(cd "$(dirname "$0")/.." && pwd)
renderer="${ICON_COMPOSER_TOOL:-/Applications/Xcode.app/Contents/Applications/Icon Composer.app/Contents/Executables/ictool}"
preview_dir="$root/build/icon-source/app-icon-previews"
thumbnail_dir="$root/web/src/assets/app-icons"
mkdir -p "$preview_dir" "$thumbnail_dir"

for appearance in white blue charcoal; do
  source="$root/build/icon-source/Ponder.icon"
  rendition=Default
  if [[ "$appearance" == blue ]]; then
    source="$root/build/icon-source/Ponder-blue.icon"
  elif [[ "$appearance" == charcoal ]]; then
    rendition=Dark
  fi
  "$renderer" "$source" --export-image \
    --output-file "$preview_dir/$appearance.png" \
    --platform macOS --rendition "$rendition" --width 1024 --height 1024 --scale 1
  output="$root/build/appicon-$appearance-macos.png"
  if [[ "$appearance" == white ]]; then
    output="$root/build/appicon-macos.png"
    cp "$preview_dir/$appearance.png" "$root/build/appicon.png"
    cp "$preview_dir/$appearance.png" "$root/build/icon-source/Ponder-default-preview.png"
  fi
  sips -z 824 824 "$preview_dir/$appearance.png" --out "$output" >/dev/null
  sips -p 1024 1024 "$output" >/dev/null
  sips -z 128 128 "$preview_dir/$appearance.png" --out "$thumbnail_dir/$appearance.png" >/dev/null
done
