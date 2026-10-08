//go:build !darwin || production

package main

// Packaged macOS apps use the bundle's layered Icon Composer artwork.
var developmentAppIcon []byte
