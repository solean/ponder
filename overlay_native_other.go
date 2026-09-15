//go:build !darwin || !cgo

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// Only the macOS build can bind the overlay to the game's window: there it
// follows Arena's frontmost state and window frame. Elsewhere the overlay
// covers the primary screen for as long as a match is live.
var overlayAttached bool

func overlayTargetRunning(string) bool { return true }

func attachOverlayWindow(window application.Window, _ string) (configured bool, level int64, behavior uint64) {
	if window == nil {
		return false, 0, 0
	}
	if screen := application.Get().Screen.GetPrimary(); screen != nil {
		window.SetBounds(screen.Bounds)
	}
	window.Show()
	overlayAttached = true
	return true, 0, 0
}

func syncOverlayWindow(window application.Window) bool {
	return window != nil && overlayAttached
}

func detachOverlayWindow(window application.Window) {
	overlayAttached = false
	if window != nil {
		window.Hide()
	}
}

func overlayPointerPosition(window application.Window) (x, y float64, supported bool) {
	return 0, 0, false
}
