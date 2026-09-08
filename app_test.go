package main

import (
	"path/filepath"
	"testing"
)

func TestDesktopDatabasePath(t *testing.T) {
	supportDir := filepath.Join(t.TempDir(), "support")

	t.Run("application support default", func(t *testing.T) {
		t.Setenv(desktopDBEnvVar, " \t")
		want := filepath.Join(supportDir, "ponder.db")
		if got := desktopDatabasePath(supportDir); got != want {
			t.Fatalf("desktopDatabasePath() = %q, want %q", got, want)
		}
	})

	t.Run("explicit development database", func(t *testing.T) {
		explicit := filepath.Join(t.TempDir(), "existing data", "ponder.db")
		t.Setenv(desktopDBEnvVar, "  "+explicit+"  ")
		if got := desktopDatabasePath(supportDir); got != explicit {
			t.Fatalf("desktopDatabasePath() = %q, want %q", got, explicit)
		}
	})
}
