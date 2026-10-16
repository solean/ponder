//go:build darwin && cgo

package main

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>
#include "menu_bar_darwin.h"
*/
import "C"

import "unsafe"

// Ponder owns its NSStatusItem instead of using the Wails system tray: the
// dropdown's header is a custom view (title, status, and an NSSwitch), which
// Wails menus cannot host.
const menuBarSupported = true

// menuBarCreate installs the status item. Main thread only. The native side
// copies the icon bytes before returning; render picks one per tracking state.
func menuBarCreate(onIcon, offIcon []byte, title, openLabel, quitLabel string) {
	cTitle, cOpen, cQuit := C.CString(title), C.CString(openLabel), C.CString(quitLabel)
	defer C.free(unsafe.Pointer(cTitle))
	defer C.free(unsafe.Pointer(cOpen))
	defer C.free(unsafe.Pointer(cQuit))
	C.ponderMenuBarCreate(
		unsafe.Pointer(&onIcon[0]), C.int(len(onIcon)),
		unsafe.Pointer(&offIcon[0]), C.int(len(offIcon)),
		cTitle, cOpen, cQuit,
	)
}

// menuBarRender updates the status item in place. Main thread only.
func menuBarRender(s menuBarStatus) {
	cStatus, cActivity := C.CString(s.status), C.CString(s.activity)
	cIndicator, cTooltip := C.CString(s.indicator), C.CString(s.tooltip())
	defer C.free(unsafe.Pointer(cStatus))
	defer C.free(unsafe.Pointer(cActivity))
	defer C.free(unsafe.Pointer(cIndicator))
	defer C.free(unsafe.Pointer(cTooltip))
	C.ponderMenuBarRender(cStatus, cActivity, C.bool(s.on), C.bool(s.canToggle), cIndicator, cTooltip)
}

// Native actions arrive on the main thread. Handlers may block (StopLive
// waits for the tailer) or call application.InvokeSync, so run them elsewhere.

//export ponderMenuBarDidToggle
func ponderMenuBarDidToggle(on C.bool) { go menuBarActions.toggle(bool(on)) }

//export ponderMenuBarDidOpen
func ponderMenuBarDidOpen() { go menuBarActions.open() }

//export ponderMenuBarDidQuit
func ponderMenuBarDidQuit() { go menuBarActions.quit() }
