package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/solean/ponder/internal/api"
	"github.com/solean/ponder/internal/appstate"
	"github.com/solean/ponder/internal/db"
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

func TestShutdownWaitsForLiveLoopBeforeClosingDatabase(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dir := t.TempDir()
		dbPath := filepath.Join(dir, "ponder.db")
		database, err := db.Open(dbPath)
		if err != nil {
			t.Fatal(err)
		}
		defer database.Close()
		if err := db.Init(t.Context(), database); err != nil {
			t.Fatal(err)
		}
		logPath := filepath.Join(dir, "Player.log")
		if err := os.WriteFile(logPath, []byte("Arena log line\n"), 0o600); err != nil {
			t.Fatal(err)
		}

		tickStarted := make(chan struct{})
		finishTick := make(chan struct{})
		service, err := appstate.NewService(appstate.Options{
			Store:              db.NewStore(database),
			DBPath:             dbPath,
			SupportDir:         dir,
			DefaultLogPath:     logPath,
			DefaultPrevLogPath: filepath.Join(dir, "Player-prev.log"),
			LiveDataChanged: func() {
				close(tickStarted)
				<-finishTick
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		defer service.ShutdownLive()
		if _, err := service.StartLive(); err != nil {
			t.Fatal(err)
		}
		select {
		case <-tickStarted:
		case <-time.After(5 * time.Second):
			close(finishTick)
			t.Fatal("live tick did not start")
		}

		background, cancel := context.WithCancel(t.Context())
		app := &App{database: database, runtimeService: service, cancel: cancel}
		shutdownDone := make(chan struct{})
		go func() {
			app.shutdown()
			close(shutdownDone)
		}()
		<-background.Done()
		synctest.Wait()
		if err := database.Ping(); err != nil {
			t.Errorf("database closed before live loop exited: %v", err)
		}
		close(finishTick)
		<-shutdownDone
		if err := database.Ping(); err == nil {
			t.Error("database is still open after shutdown")
		}
		if status, err := service.StartLive(); err == nil || status.LiveRunning {
			t.Errorf("tracking could restart after shutdown: running=%t, error=%v", status.LiveRunning, err)
		}
	})
}

func TestAPIMiddlewareStartupStatus(t *testing.T) {
	for _, tc := range []struct{ name, startupErr, status string }{
		{"initializing", "", "starting"},
		{"failed", "initialize database: disk full", "failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := &App{startupErr: tc.startupErr}
			handler := app.APIMiddleware(http.NotFoundHandler())
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/health", nil))
			if response.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d", response.Code)
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("startup response must not be cached")
			}
			var body map[string]string
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body["status"] != tc.status {
				t.Fatalf("body = %v", body)
			}
			if tc.startupErr != "" && body["error"] != tc.startupErr {
				t.Fatalf("error = %q", body["error"])
			}
			// The same middleware starts serving requests as soon as setup finishes.
			app.apiHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
			response = httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/health", nil))
			if response.Code != http.StatusOK {
				t.Fatalf("ready status = %d", response.Code)
			}
		})
	}
}

func TestAPIMiddlewareAllowsNativeDevelopmentHealthRequest(t *testing.T) {
	app := &App{}
	server := api.NewServer(nil, "", nil)
	server.SetDesktop(app)
	app.apiHandler = server.Handler()
	handler := app.APIMiddleware(http.NotFoundHandler())
	req := httptest.NewRequest(http.MethodGet, "wails://localhost:9245/api/health", nil)
	req.Header.Set("Referer", "wails://localhost:9245/")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("native development health status=%d; body=%s", response.Code, response.Body.String())
	}
}
