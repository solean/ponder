package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/solean/ponder/internal/appstate"
)

func TestRevealablePath(t *testing.T) {
	status := appstate.Status{
		DBPath:             "/support/ponder.db",
		SupportDir:         "/support",
		ConfigPath:         "/support/config.json",
		ActiveLogPath:      "/logs/Player.log",
		DefaultLogPath:     "/logs/Player.log",
		DefaultPrevLogPath: "/logs/Player-prev.log",
	}

	allowed := []string{
		"/support/ponder.db",
		"/support",
		"/support/",                // trailing slash cleans to the allowed dir
		"/logs/../logs/Player.log", // cleans to an allowed path
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
		"/support/ponder.db-wal",
		"support/ponder.db",      // relative form of an allowed path
		"/logs",                  // parent of an allowed file, not itself listed
		"/support/../etc/passwd", // cleans outside the allowed set
	}
	for _, path := range denied {
		if revealablePath(status, path) {
			t.Errorf("expected %q to be rejected", path)
		}
	}
}

type testDesktop struct {
	openedURL string
}

func (d *testDesktop) PickLogFile() (string, error) { return "", nil }
func (d *testDesktop) RevealPath(string) error      { return nil }
func (d *testDesktop) OpenURL(rawURL string) error {
	d.openedURL = rawURL
	return nil
}

func TestRuntimeOpenURLUsesDesktopBrowser(t *testing.T) {
	desktop := &testDesktop{}
	server := NewServer(nil, "", &appstate.Service{})
	server.SetDesktop(desktop)

	req := httptest.NewRequest(http.MethodPost, "/api/runtime/open-url", strings.NewReader(
		`{"url":"https://mtgarena-support.wizards.com/hc/en-us/articles/360000726823-Creating-Log-Files-on-PC-Mac-Steam"}`,
	))
	rec := httptest.NewRecorder()
	server.handleRuntimeOpenURL(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("open URL status = %d, want %d", rec.Code, http.StatusOK)
	}
	if desktop.openedURL == "" {
		t.Fatal("desktop browser was not asked to open the URL")
	}
}

func TestRuntimeOpenURLRejectsNonHTTPURLs(t *testing.T) {
	desktop := &testDesktop{}
	server := NewServer(nil, "", &appstate.Service{})
	server.SetDesktop(desktop)

	req := httptest.NewRequest(http.MethodPost, "/api/runtime/open-url", strings.NewReader(
		`{"url":"file:///etc/passwd"}`,
	))
	rec := httptest.NewRecorder()
	server.handleRuntimeOpenURL(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("open URL status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if desktop.openedURL != "" {
		t.Fatal("desktop browser should not open a non-HTTP URL")
	}
}

func TestRevealablePathIgnoresEmptyAllowedEntries(t *testing.T) {
	// PreviousLogPath is often ""; an empty allowed entry must not match anything.
	status := appstate.Status{DBPath: "/support/ponder.db"}
	if revealablePath(status, "") {
		t.Error("empty request must be rejected even when allowed list has empty entries")
	}
	if revealablePath(status, ".") {
		t.Error("'.' must not match empty allowed entries after cleaning")
	}
}
