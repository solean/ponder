package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// newRuntimeTestService returns a service whose logs live in a temp dir, plus
// the database behind it so tests can break it.
func newRuntimeTestService(t *testing.T) (*appstate.Service, *sql.DB, string) {
	t.Helper()
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
	logPath := filepath.Join(dir, "Player.log")
	service, err := appstate.NewService(appstate.Options{
		Store:               db.NewStore(database),
		DBPath:              dbPath,
		SupportDir:          filepath.Join(dir, "support"),
		DefaultLogPath:      logPath,
		DefaultPollInterval: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.ShutdownLive)
	return service, database, dir
}

func setLogPath(t *testing.T, service *appstate.Service, path string) {
	t.Helper()
	config := service.Config()
	config.LogPath = path
	if _, err := service.UpdateConfig(config); err != nil {
		t.Fatal(err)
	}
}

func postRuntime(service *appstate.Service, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	NewServer(nil, "", service).Handler().ServeHTTP(rec,
		localRequest(http.MethodPost, path, strings.NewReader(`{}`)))
	return rec
}

func TestRuntimeImportReportsFixableErrorsAsBadRequest(t *testing.T) {
	t.Parallel()

	t.Run("live tracking running", func(t *testing.T) {
		t.Parallel()
		service, _, _ := newRuntimeTestService(t)
		if _, err := service.StartLive(); err != nil {
			t.Fatal(err)
		}
		rec := postRuntime(service, "/api/runtime/import")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
		}
		if got := errorBody(t, rec); got != appstate.ErrLiveTrackingRunning.Error() {
			t.Fatalf("error = %q", got)
		}
	})

	t.Run("missing custom log", func(t *testing.T) {
		t.Parallel()
		service, _, dir := newRuntimeTestService(t)
		missing := filepath.Join(dir, "missing.log")
		setLogPath(t, service, missing)
		rec := postRuntime(service, "/api/runtime/import")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
		}
		if got, want := errorBody(t, rec), "log file not found: "+missing; got != want {
			t.Fatalf("error = %q, want %q", got, want)
		}
	})

	t.Run("custom log is a directory", func(t *testing.T) {
		t.Parallel()
		service, _, dir := newRuntimeTestService(t)
		setLogPath(t, service, dir)
		rec := postRuntime(service, "/api/runtime/import")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
		}
		if got, want := errorBody(t, rec), "log path is a directory: "+dir; got != want {
			t.Fatalf("error = %q, want %q", got, want)
		}
	})
}

func TestRuntimeImportHidesInternalFailures(t *testing.T) {
	t.Parallel()

	service, database, dir := newRuntimeTestService(t)
	logPath := filepath.Join(dir, "Player.log")
	if err := os.WriteFile(logPath, []byte("[UnityCrossThreadLogger]hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	setLogPath(t, service, logPath)
	// Every query now fails with "sql: database is closed".
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	rec := postRuntime(service, "/api/runtime/import")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: %s", rec.Code, rec.Body.String())
	}
	if got, want := errorBody(t, rec), "Couldn't import the logs."; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

func TestRuntimeLiveStartReportsFixableErrorsAsBadRequest(t *testing.T) {
	t.Parallel()

	service, _, dir := newRuntimeTestService(t)
	setLogPath(t, service, dir)
	rec := postRuntime(service, "/api/runtime/live/start")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if got, want := errorBody(t, rec), "log path is a directory: "+dir; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

func TestRuntimeLiveStartHidesInternalFailures(t *testing.T) {
	t.Parallel()

	service, _, _ := newRuntimeTestService(t)
	service.ShutdownLive()
	rec := postRuntime(service, "/api/runtime/live/start")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: %s", rec.Code, rec.Body.String())
	}
	if got, want := errorBody(t, rec), "Couldn't start live tracking."; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}
