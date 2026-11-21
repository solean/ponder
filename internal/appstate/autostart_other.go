//go:build !darwin

package appstate

import "fmt"

func GetAutostartStatus() AutostartStatus {
	return AutostartStatus{Supported: false, Note: "launch at login is only implemented on macOS"}
}

func SetAutostart(enabled bool) (AutostartStatus, error) {
	return GetAutostartStatus(), fmt.Errorf("launch at login is not supported on this platform")
}

func RefreshAutostart() error { return nil }
