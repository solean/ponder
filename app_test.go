package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/solean/ponder/internal/api"
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
