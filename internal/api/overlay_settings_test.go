package api

import (
	"context"
	"encoding/json"
	"github.com/solean/ponder/internal/appstate"
	"github.com/solean/ponder/internal/db"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestOverlaySettingsEndpoint(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.Init(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	service, err := appstate.NewService(appstate.Options{Store: db.NewStore(database), DBPath: dbPath, SupportDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(db.NewStore(database), "", service)
	read := httptest.NewRecorder()
	server.handleOverlaySettings(read, httptest.NewRequest(http.MethodGet, "/api/overlay/settings", nil))
	var settings appstate.OverlaySettings
	if err := json.Unmarshal(read.Body.Bytes(), &settings); err != nil {
		t.Fatal(err)
	}
	if settings != appstate.DefaultOverlaySettings() {
		t.Fatalf("defaults = %+v", settings)
	}
	settings.ShowOpponent = false
	settings.HoverDelayMs = 500
	payload, _ := json.Marshal(settings)
	update := httptest.NewRecorder()
	updateReq := httptest.NewRequest(http.MethodPost, "/api/overlay/settings", strings.NewReader(string(payload)))
	updateReq.Header.Set("Content-Type", "application/json")
	server.handleOverlaySettings(update, updateReq)
	if update.Code != http.StatusOK || service.OverlaySettings() != settings {
		t.Fatalf("update %d: %s", update.Code, update.Body.String())
	}
	bad := httptest.NewRecorder()
	badReq := httptest.NewRequest(http.MethodPost, "/api/overlay/settings", strings.NewReader(`{"opacity":2}`))
	badReq.Header.Set("Content-Type", "application/json")
	server.handleOverlaySettings(bad, badReq)
	if bad.Code != http.StatusBadRequest || service.OverlaySettings() != settings {
		t.Fatalf("invalid update %d: %s", bad.Code, bad.Body.String())
	}
}

func TestOverlaySettingsUnavailable(t *testing.T) {
	response := httptest.NewRecorder()
	(&Server{}).handleOverlaySettings(response, httptest.NewRequest(http.MethodGet, "/api/overlay/settings", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d", response.Code)
	}
}
