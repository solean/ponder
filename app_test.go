package main

import (
	"path/filepath"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
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

func TestOverlayPointInHUDOnlyCapturesPanelEdges(t *testing.T) {
	t.Parallel()

	bounds := application.Rect{X: 100, Y: 50, Width: 1440, Height: 900}
	tests := []struct {
		name string
		x, y float64
		want bool
	}{
		{name: "left panel", x: 250, y: 200, want: true},
		{name: "right panel", x: 1300, y: 200, want: true},
		{name: "transparent center", x: 820, y: 200, want: false},
		{name: "above window", x: 250, y: 20, want: false},
		{name: "outside right edge", x: 1600, y: 200, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := overlayPointInHUD(bounds, test.x, test.y); got != test.want {
				t.Fatalf("overlayPointInHUD(%v, %v) = %v, want %v", test.x, test.y, got, test.want)
			}
		})
	}
}
