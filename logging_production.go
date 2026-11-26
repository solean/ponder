//go:build production

package main

import (
	"io"
	"log"
	"log/slog"
	"os"

	"github.com/solean/ponder/internal/applog"
	"github.com/solean/ponder/internal/version"
)

// setupLogging sends the packaged app's logs to ~/Library/Logs/Ponder, since an
// app launched from Finder has no visible stderr. Output still goes to stderr
// too, for a binary run from a terminal. Wails discards its own messages in
// production builds unless given a logger, so its warnings and errors (panics
// included) go to the same file. Writes are unbuffered and the file stays open
// until the process exits, so nothing is lost at shutdown.
func setupLogging() *slog.Logger {
	dir, err := applog.DefaultDir(appDisplayName)
	if err == nil {
		var file *applog.Writer
		if file, err = applog.Open(dir, 0); err == nil {
			out := io.MultiWriter(file, os.Stderr)
			log.SetOutput(out)
			log.Printf("%s %s starting; logging to %s", appDisplayName, version.Version, file.Path())
			return slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: slog.LevelWarn}))
		}
	}
	log.Printf("file logging unavailable, logging to stderr only: %v", err)
	return nil
}
