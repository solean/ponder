//go:build darwin && cgo

package main

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#include <stdbool.h>
bool ponderSetAppIcon(const void *bytes, int length);
*/
import "C"

import (
	_ "embed"
	"fmt"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const appIconSupported = true

// These are padded Icon Composer renders, matching the system Dock margins.
//
//go:embed build/appicon-macos.png
var whiteAppIcon []byte

//go:embed build/appicon-blue-macos.png
var blueAppIcon []byte

//go:embed build/appicon-charcoal-macos.png
var charcoalAppIcon []byte

func setAppIcon(appearance string) error {
	var data []byte
	switch appearance {
	case "white":
		data = whiteAppIcon
	case "blue":
		data = blueAppIcon
	case "charcoal":
		data = charcoalAppIcon
	default:
		return fmt.Errorf("unknown app icon: %s", appearance)
	}
	var applied bool
	application.InvokeSync(func() {
		applied = bool(C.ponderSetAppIcon(unsafe.Pointer(&data[0]), C.int(len(data))))
	})
	if !applied {
		return fmt.Errorf("could not load the %s app icon", appearance)
	}
	return nil
}
