package appstate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// arenaLogHeader is the start of Player.log as Arena 2026.11 writes it on
// macOS, trimmed for brevity.
func arenaLogHeader(detailedLogs string) string {
	return fmt.Sprintf(`Input System module state changed to: Initialized.
[Physics::Module] Initialized fallback backend.
Initialize engine version: 6000.3.14f1 (d68c3f99a318)
UnloadTime: 2.143459 ms
DETAILED LOGS: %s
UnityEngine.DebugLogHandler:Internal_Log(LogType, LogOption, String, Object)
Core.Shared.Code.Utilities.LoggingUtils:Initialize()

Startup Timestamp: 11/6/2026 7:53:59 PM
`, detailedLogs)
}

func TestScanDetailedLogs(t *testing.T) {
	tests := []struct {
		name string
		log  string
		want string
	}{
		{"enabled", arenaLogHeader("ENABLED"), DetailedLogsEnabled},
		{"disabled", arenaLogHeader("DISABLED"), DetailedLogsDisabled},
		{"windows line endings", strings.ReplaceAll(arenaLogHeader("DISABLED"), "\n", "\r\n"), DetailedLogsDisabled},
		{"header not written yet", "Input System module state changed to: Initialized.\n", ""},
		{"unrecognized value", arenaLogHeader("MAYBE"), ""},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := scanDetailedLogs(strings.NewReader(tt.log)); got != tt.want {
				t.Fatalf("scanDetailedLogs = %q; want %q", got, tt.want)
			}
		})
	}
}

func TestDetectDetailedLogsIgnoresLaterMentions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Player.log")
	// Only the header counts; a line far past the scan limit never flips it.
	log := arenaLogHeader("ENABLED") +
		strings.Repeat("x", detailedLogsScanLimit) + "\nDETAILED LOGS: DISABLED\n"
	if err := os.WriteFile(path, []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := detectDetailedLogs(path); got != DetailedLogsEnabled {
		t.Fatalf("detectDetailedLogs = %q; want %q", got, DetailedLogsEnabled)
	}
	if got := detectDetailedLogs(filepath.Join(t.TempDir(), "missing.log")); got != "" {
		t.Fatalf("missing log = %q; want empty", got)
	}
}
