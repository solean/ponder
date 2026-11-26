package applog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestWriterAppendsToExistingFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Logs", "Ponder")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("earlier\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	w, err := Open(dir, 1024)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer w.Close()
	if _, err := w.Write([]byte("later\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if got := readFile(t, w.Path()); got != "earlier\nlater\n" {
		t.Fatalf("log = %q", got)
	}
}

func TestWriterCreatesMissingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b")
	w, err := Open(dir, 0)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer w.Close()
	if w.Path() != filepath.Join(dir, FileName) {
		t.Fatalf("Path = %q", w.Path())
	}
	if _, err := os.Stat(w.Path()); err != nil {
		t.Fatalf("log file not created: %v", err)
	}
}

func TestWriterRotatesAtCap(t *testing.T) {
	dir := t.TempDir()
	w, err := Open(dir, 10)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer w.Close()

	for _, line := range []string{"first\n", "second\n", "third\n"} {
		if _, err := w.Write([]byte(line)); err != nil {
			t.Fatalf("Write %q: %v", line, err)
		}
	}

	// "first\n" + "second\n" is over 10 bytes, so each later line starts a
	// new file and only one backup is kept.
	if got := readFile(t, w.Path()); got != "third\n" {
		t.Fatalf("active log = %q", got)
	}
	if got := readFile(t, filepath.Join(dir, BackupName)); got != "second\n" {
		t.Fatalf("backup log = %q", got)
	}
}

func TestWriterRotatesOversizedExistingFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(strings.Repeat("x", 20)), 0o644); err != nil {
		t.Fatal(err)
	}
	w, err := Open(dir, 10)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer w.Close()
	if _, err := w.Write([]byte("new\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := readFile(t, w.Path()); got != "new\n" {
		t.Fatalf("active log = %q", got)
	}
	if got := readFile(t, filepath.Join(dir, BackupName)); got != strings.Repeat("x", 20) {
		t.Fatalf("backup log = %q", got)
	}
}

func TestWriterKeepsOversizedEntryInEmptyFile(t *testing.T) {
	dir := t.TempDir()
	w, err := Open(dir, 4)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer w.Close()
	if _, err := w.Write([]byte("much too long\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := readFile(t, w.Path()); got != "much too long\n" {
		t.Fatalf("active log = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, BackupName)); !os.IsNotExist(err) {
		t.Fatalf("unexpected backup: %v", err)
	}
}

func TestWriterFailsAfterClose(t *testing.T) {
	w, err := Open(t.TempDir(), 0)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := w.Write([]byte("x")); err == nil {
		t.Fatal("Write after Close succeeded")
	}
}
