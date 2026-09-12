//go:build !darwin || !cgo

package main

import "github.com/wailsapp/wails/v3/pkg/application"

func showOverlayWindow(window application.Window) (configured bool, level int64, behavior uint64) {
	if window == nil {
		return false, 0, 0
	}
	if screen := application.Get().Screen.GetPrimary(); screen != nil {
		window.SetBounds(screen.Bounds)
	}
	window.Show()
	return true, 0, 0
}
func hideOverlayWindow(window application.Window) {
	if window != nil {
		window.Hide()
	}
}

func overlayPointerPosition(window application.Window) (x, y float64, supported bool) {
	return 0, 0, false
}
