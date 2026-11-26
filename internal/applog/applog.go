// Package applog writes the desktop app's logs to a size-capped file, so a
// build launched from Finder (which has no visible stderr) leaves something to
// diagnose.
package applog

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
)

const (
	// FileName is the active log file inside the log directory.
	FileName = "ponder.log"
	// BackupName is the previous log file, replaced on each rotation.
	BackupName = "ponder.log.1"
	// DefaultMaxBytes caps each file, so the logs use at most twice this.
	DefaultMaxBytes = 5 << 20
)

// DefaultDir is the macOS per-user log directory for the app,
// ~/Library/Logs/<appName>.
func DefaultDir(appName string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home dir: %w", err)
	}
	return filepath.Join(home, "Library", "Logs", appName), nil
}

// Writer appends to <dir>/ponder.log. A write that would take the file past
// maxBytes first renames it to ponder.log.1, replacing the older backup, and
// starts a new file. It is safe for concurrent use.
//
// Go runtime crash output (unrecovered panics, fatal errors) also goes to the
// active file, so a crash in any goroutine is recorded.
type Writer struct {
	mu       sync.Mutex
	dir      string
	maxBytes int64
	file     *os.File
	size     int64
}

// Open creates dir if needed and opens the active log file for appending.
// maxBytes <= 0 means DefaultMaxBytes.
func Open(dir string, maxBytes int64) (*Writer, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}
	w := &Writer{dir: dir, maxBytes: maxBytes}
	if err := w.openFile(); err != nil {
		return nil, err
	}
	return w, nil
}

// Path is the active log file.
func (w *Writer) Path() string {
	return filepath.Join(w.dir, FileName)
}

func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return 0, os.ErrClosed
	}
	// An empty file always takes the write, so one oversized entry is kept
	// rather than rotating forever.
	if w.size > 0 && w.size+int64(len(p)) > w.maxBytes {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

// Close closes the active file. Later writes fail with os.ErrClosed.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func (w *Writer) rotate() error {
	if err := w.file.Close(); err != nil {
		return fmt.Errorf("close log file: %w", err)
	}
	w.file = nil
	backup := filepath.Join(w.dir, BackupName)
	if err := os.Rename(w.Path(), backup); err != nil && !errors.Is(err, os.ErrNotExist) {
		// Keep logging to the old file rather than losing output.
		_ = w.openFile()
		return fmt.Errorf("rotate log file: %w", err)
	}
	return w.openFile()
}

func (w *Writer) openFile() error {
	file, err := os.OpenFile(w.Path(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return fmt.Errorf("stat log file: %w", err)
	}
	w.file = file
	w.size = info.Size()
	// The runtime keeps its own descriptor, so point it at each new file.
	_ = debug.SetCrashOutput(file, debug.CrashOptions{})
	return nil
}
