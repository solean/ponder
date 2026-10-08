//go:build darwin && !production

package main

import _ "embed"

// Set the development Dock icon explicitly because macOS can retain an old
// icon while the development bundle is rebuilt in place.
//
//go:embed build/appicon-macos.png
var developmentAppIcon []byte
