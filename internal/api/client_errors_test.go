package api

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prevOut, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	})
	return &buf
}

func TestClientErrorWritesToLog(t *testing.T) {
	logged := captureLog(t)
	server := NewServer(nil, "", nil)

	req := localRequest(http.MethodPost, "/api/client-error", strings.NewReader(
		`{"scope":"page","message":"boom","stack":"Error: boom\n    at Deck","componentStack":"    at DeckPage"}`,
	))
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	want := "frontend error (page): boom\nError: boom\n    at Deck\ncomponent stack:\n    at DeckPage\n"
	if got := logged.String(); got != want {
		t.Fatalf("log = %q, want %q", got, want)
	}
}

func TestClientErrorCapsFields(t *testing.T) {
	logged := captureLog(t)
	server := NewServer(nil, "", nil)

	body := `{"message":"` + strings.Repeat("x", clientErrorFieldLimit+100) + `"}`
	req := localRequest(http.MethodPost, "/api/client-error", strings.NewReader(body))
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	got := logged.String()
	if !strings.HasPrefix(got, "frontend error (app): xxx") || !strings.HasSuffix(got, "x…\n") {
		t.Fatalf("log = %.80q…", got)
	}
	if len(got) > clientErrorFieldLimit+100 {
		t.Fatalf("log entry is %d bytes, want it capped", len(got))
	}
}

func TestClientErrorRejectsGet(t *testing.T) {
	server := NewServer(nil, "", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, localRequest(http.MethodGet, "/api/client-error", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}
