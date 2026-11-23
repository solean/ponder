package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/solean/ponder/internal/appstate"
	"github.com/solean/ponder/internal/db"
)

func errorBody(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v: %s", err, rec.Body.String())
	}
	return body.Error
}

func TestInternalErrorsHideDetailsFromUsers(t *testing.T) {
	t.Parallel()

	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Init(context.Background(), database); err != nil {
		t.Fatalf("init db: %v", err)
	}
	handler := NewServer(db.NewStore(database), "", nil).Handler()
	// Every query now fails with "sql: database is closed".
	if err := database.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, localRequest(http.MethodGet, "/api/overview", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: %s", rec.Code, rec.Body.String())
	}
	if got, want := errorBody(t, rec), "Couldn't load the overview."; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

type failingDesktop struct{ testDesktop }

func (*failingDesktop) OpenURL(string) error {
	return errors.New("exec: /usr/bin/open: permission denied")
}

func TestDesktopFailuresHideDetailsFromUsers(t *testing.T) {
	server := NewServer(nil, "", &appstate.Service{})
	server.SetDesktop(&failingDesktop{})

	rec := httptest.NewRecorder()
	server.handleRuntimeOpenURL(rec, localRequest(http.MethodPost, "/api/runtime/open-url",
		strings.NewReader(`{"url":"https://example.com"}`)))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	got := errorBody(t, rec)
	if got != "Couldn't open the link." || strings.Contains(got, "/usr/bin") {
		t.Fatalf("error = %q", got)
	}
}

func TestMissingDeckIsNotFound(t *testing.T) {
	t.Parallel()

	handler := NewServer(seedMatchListStore(t, 1), "", nil).Handler()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, localRequest(http.MethodGet, "/api/decks/999999", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
	if got := errorBody(t, rec); got != "deck not found" {
		t.Fatalf("error = %q, want %q", got, "deck not found")
	}
}

func TestInvalidAppIconIsBadRequest(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.Init(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	store := db.NewStore(database)
	service, err := appstate.NewService(appstate.Options{Store: store, DBPath: dbPath, SupportDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	config := service.Config()
	config.AppIcon = "purple"
	body, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	NewServer(store, "", service).Handler().ServeHTTP(rec,
		localRequest(http.MethodPost, "/api/runtime/config", strings.NewReader(string(body))))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if got := errorBody(t, rec); got != appstate.ErrInvalidAppIcon.Error() {
		t.Fatalf("error = %q", got)
	}
}

func TestAIStatusReportsUsageFailureWithoutDetails(t *testing.T) {
	t.Parallel()

	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.Init(context.Background(), database); err != nil {
		t.Fatalf("init db: %v", err)
	}
	// Any failure in the usage summary; a missing table is the easiest.
	if _, err := database.Exec(`DROP TABLE ai_usage_events`); err != nil {
		t.Fatalf("drop table: %v", err)
	}

	rec := httptest.NewRecorder()
	NewServer(db.NewStore(database), "", nil).Handler().ServeHTTP(rec, localRequest(http.MethodGet, "/api/ai/status", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := body["usage"]; ok {
		t.Fatalf("usage should be left out when the summary fails: %s", body["usage"])
	}
	var usageError string
	if err := json.Unmarshal(body["usageError"], &usageError); err != nil || usageError != "Couldn't load token usage." {
		t.Fatalf("usageError = %s, want %q", body["usageError"], "Couldn't load token usage.")
	}
	if strings.Contains(rec.Body.String(), "ai_usage_events") {
		t.Fatalf("response leaks the database error: %s", rec.Body.String())
	}
}

func TestAIStatusIncludesUsageWhenItLoads(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	NewServer(seedMatchListStore(t, 1), "", nil).Handler().ServeHTTP(rec, localRequest(http.MethodGet, "/api/ai/status", nil))

	var body struct {
		Usage *struct {
			Providers []json.RawMessage `json:"providers"`
		} `json:"usage"`
		UsageError string `json:"usageError"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Usage == nil || body.Usage.Providers == nil || body.UsageError != "" {
		t.Fatalf("want usage with a providers list and no error, got %s", rec.Body.String())
	}
}
