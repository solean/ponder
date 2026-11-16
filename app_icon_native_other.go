//go:build !darwin || !cgo

package main

import "fmt"

const appIconSupported = false

func setAppIcon(string) error {
	return fmt.Errorf("app icon selection is only available in the Mac app")
}
