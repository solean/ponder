package api

import (
	"testing"

	"github.com/cschnabel/mtgdata/internal/appstate"
)

func TestRevealablePath(t *testing.T) {
	status := appstate.Status{
		DBPath:             "/support/mtgdata.db",
		SupportDir:         "/support",
		ConfigPath:         "/support/config.json",
		ActiveLogPath:      "/logs/Player.log",
		DefaultLogPath:     "/logs/Player.log",
		DefaultPrevLogPath: "/logs/Player-prev.log",
	}

	allowed := []string{
		"/support/mtgdata.db",
		"/support",
		"/support/",                     // trailing slash cleans to the allowed dir
		"/logs/../logs/Player.log",      // cleans to an allowed path
		"/support/config.json",
		"/logs/Player-prev.log",
	}
	for _, path := range allowed {
		if !revealablePath(status, path) {
			t.Errorf("expected %q to be revealable", path)
		}
	}

	denied := []string{
		"",
		"   ",
		"/etc/passwd",
		"/support/other.db",
		"/support/mtgdata.db-wal",
		"support/mtgdata.db",       // relative form of an allowed path
		"/logs",                    // parent of an allowed file, not itself listed
		"/support/../etc/passwd",   // cleans outside the allowed set
	}
	for _, path := range denied {
		if revealablePath(status, path) {
			t.Errorf("expected %q to be rejected", path)
		}
	}
}

func TestRevealablePathIgnoresEmptyAllowedEntries(t *testing.T) {
	// PreviousLogPath is often ""; an empty allowed entry must not match anything.
	status := appstate.Status{DBPath: "/support/mtgdata.db"}
	if revealablePath(status, "") {
		t.Error("empty request must be rejected even when allowed list has empty entries")
	}
	if revealablePath(status, ".") {
		t.Error("'.' must not match empty allowed entries after cleaning")
	}
}
