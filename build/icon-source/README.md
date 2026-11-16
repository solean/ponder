# Ponder app icon

`Ponder.icon` is the editable Icon Composer source, with cobalt (`#2864EB`) orb vector artwork on white. The orb group uses 0.25 translucency, 0.3 neutral shadow opacity, and specular highlights. `orbs.svg` is the original mint vector construction. Preview PNGs are for visual comparison; `Ponder-default-preview.png` reflects the current icon, and the other earlier previews are historical.

`../appicon.png` is the checked-in rendered fallback. After changing the design, export its Default appearance at 1024 pixels from Icon Composer and update this PNG. The current fallback was rendered with the bundled Icon Composer `ictool`:

```sh
"/Applications/Xcode.app/Contents/Applications/Icon Composer.app/Contents/Executables/ictool" \
  "$PWD/build/icon-source/Ponder.icon" --export-image \
  --output-file "$PWD/build/appicon.png" \
  --platform macOS --rendition Default --width 1024 --height 1024 --scale 1
```

`blue-white-previews/` contains the color studies and native cobalt appearance previews. The comparison sheet uses illustrative SVG shading; the `01-cobalt-native.png` and `cobalt-*.png` previews use Apple's renderer.

## Settings icon choices

Settings → Application → App icon offers White, Blue, and Charcoal on macOS. The choice is stored as `appIcon` in the runtime config and restored on launch. Missing or unknown saved values default to White.

Run `bash scripts/render-app-icons.sh` to regenerate all three Dock PNGs and the small settings thumbnails. White uses the Default rendition of `Ponder.icon`; Blue uses `Ponder-blue.icon`; Charcoal uses the Dark rendition of `Ponder.icon`. The full-size renders are saved in `app-icon-previews/`.

The running app sets `NSApplication.applicationIconImage` to a rendered PNG, so these selections have Icon Composer's rendered glass treatment, not a live layered material. They change the Dock and app switcher while Ponder is running; Finder and the closed app retain the packaged white layered icon. Changing this preference never rewrites the signed app bundle.

The macOS fallback is `../appicon-macos.png`: 824-pixel artwork centered on a transparent 1024-pixel canvas. This outer margin prevents the full-bleed export from receiving an extra pale tile in the Dock. After updating `appicon.png`, regenerate it from the repository root:

```sh
sips -z 824 824 build/appicon.png --out build/appicon-macos.png
sips -p 1024 1024 build/appicon-macos.png
```

Windows uses the original PNG; macOS uses the padded PNG when the layered asset compiler is unavailable.

Run `wails3 task common:generate:icons` from the repository root. The task always runs once per build so installing Xcode enables layered compilation even if the PNG has not changed. The task invokes Xcode's `actool` through `xcrun` and compiles `Ponder.icon` into `build/darwin/Assets.car` and `icons.icns`; without full Xcode it generates the static `.icns` fallback from the PNG. Icon Composer alone does not provide `actool`.

Both development and production macOS bundles use `CFBundleIconName=Ponder` for the layered asset and `CFBundleIconFile=icons` for the fallback. The existing bundle tasks copy Assets.car when present and sign after installing resources.

To package an already-built binary: `wails3 task darwin:create:app:bundle`. To rebuild and package production: `wails3 package GOOS=darwin`.
