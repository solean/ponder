//go:build !production

package main

import "log/slog"

// setupLogging leaves development builds logging to the terminal. The nil
// logger keeps Wails' own colored dev logger.
func setupLogging() *slog.Logger {
	return nil
}
