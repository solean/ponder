//go:build !darwin || production

package main

// Packaged macOS apps launch with the bundle's layered artwork. Startup then
// restores the user's selected Dock icon after loading their preferences.
var developmentAppIcon []byte
