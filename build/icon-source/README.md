# Ponder app icon

`Ponder.icon` is the editable Icon Composer source, with the mint orb vector artwork embedded in its Assets directory. `orbs.svg` is the original vector construction. Preview PNGs are for visual comparison.

`../appicon.png` is the checked-in rendered fallback. After changing the design, export its Default appearance at 1024 pixels from Icon Composer and update this PNG. Use the same design generation as the approved preview (27 for the current design).

The macOS fallback is `../appicon-macos.png`: 824-pixel artwork centered on a transparent 1024-pixel canvas. This outer margin prevents the full-bleed export from receiving an extra pale tile in the Dock. After updating `appicon.png`, regenerate it from the repository root:

```sh
sips -z 824 824 build/appicon.png --out build/appicon-macos.png
sips -p 1024 1024 build/appicon-macos.png
```

Windows uses the original PNG; macOS uses the padded PNG when the layered asset compiler is unavailable.

Run `wails3 task common:generate:icons` from the repository root. The task always runs once per build so installing Xcode enables layered compilation even if the PNG has not changed. The task invokes Xcode's `actool` through `xcrun` and compiles `Ponder.icon` into `build/darwin/Assets.car` and `icons.icns`; without full Xcode it generates the static `.icns` fallback from the PNG. Icon Composer alone does not provide `actool`.

Both development and production macOS bundles use `CFBundleIconName=Ponder` for the layered asset and `CFBundleIconFile=icons` for the fallback. The existing bundle tasks copy Assets.car when present and sign after installing resources.

To package an already-built binary: `wails3 task darwin:create:app:bundle`. To rebuild and package production: `wails3 package GOOS=darwin`.
