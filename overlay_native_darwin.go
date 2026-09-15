//go:build darwin && cgo

package main

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework ApplicationServices
#include <stdlib.h>
#include "overlay_native_darwin.h"
*/
import "C"

import (
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// overlayTargetRunning reports whether the tracked game is running at all. The
// overlay detaches when it is not, which also restores the Dock icon that the
// accessory activation policy hides.
func overlayTargetRunning(targetBundleID string) (running bool) {
	cBundleID := C.CString(targetBundleID)
	defer C.free(unsafe.Pointer(cBundleID))
	application.InvokeSync(func() {
		running = bool(C.ponderOverlayTargetRunning(cBundleID))
	})
	return running
}

// attachOverlayWindow configures the overlay window and binds it to the target
// application. The window is only ordered in while that application is
// frontmost, so attaching does not necessarily make the overlay visible.
func attachOverlayWindow(
	window application.Window,
	targetBundleID string,
) (configured bool, level int64, behavior uint64) {
	if window == nil {
		return false, 0, 0
	}
	cBundleID := C.CString(targetBundleID)
	defer C.free(unsafe.Pointer(cBundleID))
	var nativeLevel C.int64_t
	var nativeBehavior C.uint64_t
	application.InvokeSync(func() {
		configured = bool(C.ponderOverlayAttach(
			window.NativeWindow(),
			cBundleID,
			&nativeLevel,
			&nativeBehavior,
		))
	})
	return configured, int64(nativeLevel), uint64(nativeBehavior)
}

// syncOverlayWindow re-evaluates target focus and geometry, returning whether
// the overlay is on screen afterwards. Focus changes are handled natively as
// they happen; this only catches a game window that moved or changed display.
func syncOverlayWindow(window application.Window) (visible bool) {
	if window == nil {
		return false
	}
	application.InvokeSync(func() {
		visible = bool(C.ponderOverlaySync())
	})
	return visible
}

func detachOverlayWindow(window application.Window) {
	application.InvokeSync(func() {
		var nativeWindow unsafe.Pointer
		if window != nil {
			nativeWindow = window.NativeWindow()
		}
		C.ponderOverlayDetach(nativeWindow)
	})
}

func overlayPointerPosition(window application.Window) (x, y float64, supported bool) {
	if window == nil {
		return 0, 0, false
	}
	var nativeX, nativeY C.double
	application.InvokeSync(func() {
		supported = bool(C.ponderOverlayPointerPosition(window.NativeWindow(), &nativeX, &nativeY))
	})
	return float64(nativeX), float64(nativeY), supported
}
