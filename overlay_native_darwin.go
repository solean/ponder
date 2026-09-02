//go:build darwin && cgo

package main

/*
#cgo LDFLAGS: -framework Cocoa -framework ApplicationServices
#include "overlay_native_darwin.h"
*/
import "C"

import (
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func showOverlayWindow(window application.Window) (configured bool, level int64, behavior uint64) {
	if window == nil {
		return false, 0, 0
	}
	var nativeLevel C.int64_t
	var nativeBehavior C.uint64_t
	application.InvokeSync(func() {
		configured = bool(C.ponderShowOverlayWindowInactive(
			window.NativeWindow(),
			&nativeLevel,
			&nativeBehavior,
		))
	})
	return configured, int64(nativeLevel), uint64(nativeBehavior)
}

func hideOverlayWindow(window application.Window) {
	application.InvokeSync(func() {
		var nativeWindow unsafe.Pointer
		if window != nil {
			nativeWindow = window.NativeWindow()
		}
		C.ponderHideOverlayWindow(nativeWindow)
	})
}

func overlayPointerPosition() (x, y float64, supported bool) {
	var nativeX, nativeY C.double
	if !bool(C.ponderOverlayPointerPosition(&nativeX, &nativeY)) {
		return 0, 0, false
	}
	return float64(nativeX), float64(nativeY), true
}
