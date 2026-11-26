//go:build production

package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupLoggingWritesToLibraryLogs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	prevOut, prevFlags := log.Writer(), log.Flags()
	t.Cleanup(func() {
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	})

	wailsLogger := setupLogging()
	if wailsLogger == nil {
		t.Fatal("setupLogging returned no Wails logger")
	}
	log.Printf("app message")
	wailsLogger.Info("wails info")
	wailsLogger.Error("wails error")

	data, err := os.ReadFile(filepath.Join(home, "Library", "Logs", appDisplayName, "ponder.log"))
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	got := string(data)
	for _, want := range []string{"starting; logging to", "app message", "wails error"} {
		if !strings.Contains(got, want) {
			t.Errorf("log file is missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "wails info") {
		t.Errorf("log file has Wails info noise:\n%s", got)
	}
}
