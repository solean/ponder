//go:build darwin

package appstate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLaunchAgentPlistPassesLoginFlag(t *testing.T) {
	plist := launchAgentPlist("/Applications/Ponder & Co.app/Contents/MacOS/Ponder")
	if !strings.Contains(plist, "<string>/Applications/Ponder &amp; Co.app/Contents/MacOS/Ponder</string>\n\t\t<string>"+LoginLaunchFlag+"</string>") {
		t.Fatalf("plist does not pass the login flag after the executable:\n%s", plist)
	}
}

func TestLaunchedAtLogin(t *testing.T) {
	if LaunchedAtLogin(nil) {
		t.Fatal("no args reported as a login launch")
	}
	if LaunchedAtLogin([]string{"--version"}) {
		t.Fatal("unrelated flag reported as a login launch")
	}
	if !LaunchedAtLogin([]string{LoginLaunchFlag}) {
		t.Fatal("login flag not detected")
	}
}

func TestRefreshLaunchAgent(t *testing.T) {
	const exe = "/Applications/Ponder.app/Contents/MacOS/Ponder"
	legacy := strings.Replace(launchAgentPlist(exe), "\t\t<string>"+LoginLaunchFlag+"</string>\n", "", 1)

	t.Run("rewrites legacy agent for this binary", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "agent.plist")
		writeFile(t, path, legacy)
		if err := refreshLaunchAgent(path, exe); err != nil {
			t.Fatal(err)
		}
		if got := readFile(t, path); got != launchAgentPlist(exe) {
			t.Fatalf("agent not rewritten:\n%s", got)
		}
	})

	t.Run("leaves agent for another binary", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "agent.plist")
		writeFile(t, path, legacy)
		if err := refreshLaunchAgent(path, "/Users/me/dev/ponder/bin/Ponder"); err != nil {
			t.Fatal(err)
		}
		if got := readFile(t, path); got != legacy {
			t.Fatalf("agent for another binary was changed:\n%s", got)
		}
	})

	t.Run("missing agent stays missing", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "agent.plist")
		if err := refreshLaunchAgent(path, exe); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("agent created when none existed: %v", err)
		}
	})
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
