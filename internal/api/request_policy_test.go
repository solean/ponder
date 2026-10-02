package api

import (
	"context"
	"github.com/solean/ponder/internal/appstate"
	"github.com/solean/ponder/internal/db"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type recordingDesktop struct{ opened, picked int }

func (d *recordingDesktop) OpenURL(string) error         { d.opened++; return nil }
func (d *recordingDesktop) PickLogFile() (string, error) { d.picked++; return "", nil }
func (d *recordingDesktop) RevealPath(string) error      { return nil }

func TestUntrustedOriginMutationsHaveNoSideEffects(t *testing.T) {
	for _, origin := range []string{"https://attacker.example", "null", "http://localhost.attacker.example", "http://localhost:5173/path", "http://user@localhost:5173"} {
		for _, endpoint := range []string{"open-url", "pick-log"} {
			t.Run(origin+"/"+endpoint, func(t *testing.T) {
				desktop := &recordingDesktop{}
				server := NewServer(nil, "", &appstate.Service{})
				server.SetDesktop(desktop)
				body := ""
				if endpoint == "open-url" {
					body = `{"url":"https://example.org"}`
				}
				req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/runtime/"+endpoint, strings.NewReader(body))
				req.Header.Set("Origin", origin)
				req.Header.Set("Content-Type", "text/plain")
				rec := httptest.NewRecorder()
				server.Handler().ServeHTTP(rec, req)
				if rec.Code != http.StatusForbidden {
					t.Errorf("status=%d, want 403; body=%s", rec.Code, rec.Body.String())
				}
				if desktop.opened != 0 || desktop.picked != 0 {
					t.Errorf("native actions ran: opened=%d picked=%d", desktop.opened, desktop.picked)
				}
			})
		}
	}
}

func TestJSONMutationsRejectUnsupportedMediaType(t *testing.T) {
	for _, mediaType := range []string{"", "text/plain", "application/x-www-form-urlencoded", "application/json; invalid"} {
		t.Run(mediaType, func(t *testing.T) {
			desktop := &recordingDesktop{}
			server := NewServer(nil, "", &appstate.Service{})
			server.SetDesktop(desktop)
			req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/runtime/open-url", strings.NewReader(`{"url":"https://example.org"}`))
			req.Header.Set("Origin", "http://127.0.0.1:8080")
			req.Header.Set("Content-Type", mediaType)
			rec := httptest.NewRecorder()
			server.Handler().ServeHTTP(rec, req)
			if rec.Code != http.StatusUnsupportedMediaType {
				t.Errorf("status=%d, want 415", rec.Code)
			}
			if desktop.opened != 0 {
				t.Error("native action ran")
			}
		})
	}
}

func TestRequestPolicyHostsAndOrigins(t *testing.T) {
	for _, tc := range []struct {
		name, addr, host, origin, referer, site string
		desktop                                 bool
		want                                    int
	}{
		{name: "CLI same origin", addr: "127.0.0.1:8080", host: "127.0.0.1:8080", origin: "http://127.0.0.1:8080", want: 200},
		{name: "IPv6 loopback", addr: "[::1]:8080", host: "[::1]:8080", origin: "http://[::1]:8080", want: 200},
		{name: "loopback alias", addr: "127.0.0.1:8080", host: "127.0.0.2:8080", origin: "http://127.0.0.2:8080", want: 200},
		{name: "Vite cross origin", host: "127.0.0.1:8080", origin: "http://localhost:5173", want: 200},
		{name: "custom dev port", host: "localhost:8080", origin: "https://127.0.0.1:9245", want: 200},
		{name: "Vite proxy referer", host: "127.0.0.1:8080", referer: "http://localhost:5174/settings", want: 200},
		{name: "non-browser client", host: "localhost:8080", want: 200},
		{name: "native macOS headers", desktop: true, host: "localhost", referer: "wails://localhost/", want: 200},
		{name: "native macOS dev referer", desktop: true, host: "localhost:9245", referer: "wails://localhost:9245/", want: 200},
		{name: "native macOS dev origin", desktop: true, host: "localhost:9245", origin: "wails://localhost:9245", want: 200},
		{name: "native custom scheme wrong port", desktop: true, host: "localhost:9245", origin: "wails://localhost:9246", want: 403},
		{name: "native custom scheme", desktop: true, host: "localhost", origin: "wails://localhost", want: 200},
		{name: "native Linux", desktop: true, host: "wails", origin: "wails://wails", want: 200},
		{name: "native Windows", desktop: true, host: "wails.localhost", origin: "http://wails.localhost", want: 200},
		{name: "native null rejected", desktop: true, host: "localhost", origin: "null", want: 403},
		{name: "custom scheme on HTTP rejected", addr: "127.0.0.1:39123", host: "localhost", origin: "wails://localhost", want: 403},
		{name: "Wails host on HTTP rejected", addr: "127.0.0.1:39123", host: "wails.localhost", origin: "http://wails.localhost", want: 403},
		{name: "DNS rebinding host", host: "attacker.example:8080", origin: "http://localhost:5173", want: 403},
		{name: "loopback denies remote IP", addr: "127.0.0.1:8080", host: "192.168.1.2:8080", want: 403},
		{name: "configured remote IP", addr: "192.168.1.2:8080", host: "192.168.1.2:8080", origin: "http://192.168.1.2:8080", want: 200},
		{name: "remote host wrong port", addr: "192.168.1.2:8080", host: "192.168.1.2:8080", origin: "http://192.168.1.2:8081", want: 403},
		{name: "remote host wrong scheme", addr: "192.168.1.2:8080", host: "192.168.1.2:8080", origin: "https://192.168.1.2:8080", want: 403},
		{name: "remote bind unrelated IP", addr: "192.168.1.2:8080", host: "192.168.1.3:8080", want: 403},
		{name: "wildcard remote same origin", addr: ":8080", host: "192.168.1.3:8080", origin: "http://192.168.1.3:8080", want: 200},
		{name: "IPv6 wildcard", addr: "[::]:8080", host: "[2001:db8::1]:8080", origin: "http://[2001:db8::1]:8080", want: 200},
		{name: "explicit hostname", addr: "ponder.example:8080", host: "ponder.example:8080", origin: "http://ponder.example:8080", want: 200},
		{name: "default port normalization", addr: "192.168.1.2:80", host: "192.168.1.2:80", origin: "http://192.168.1.2", want: 200},
		{name: "cross site without origin", host: "localhost:8080", site: "cross-site", want: 403},
		{name: "cross site with trusted referer", host: "localhost:8080", site: "cross-site", referer: "http://localhost:8080/", want: 403},
		{name: "bracketed hostname", host: "[localhost]:8080", want: 403},
		{name: "untrusted referer", host: "localhost:8080", referer: "https://attacker.example/page", want: 403},
		{name: "untrusted origin beats trusted referer", host: "localhost:8080", origin: "https://attacker.example", referer: "http://localhost:8080/", want: 403},
		{name: "userinfo host", host: "attacker@localhost:8080", want: 403},
		{name: "invalid host port", host: "localhost:65536", want: 403},
		{name: "empty host port", host: "localhost:", want: 403},
		{name: "unbracketed IPv6", host: "::1", want: 403},
		{name: "trailing slash origin", host: "localhost:8080", origin: "http://localhost:5173/", want: 403},
		{name: "query origin", host: "localhost:8080", origin: "http://localhost:5173?", want: 403},
		{name: "fragment origin", host: "localhost:8080", origin: "http://localhost:5173#", want: 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := listenerPolicy(tc.addr)
			p.desktop = tc.desktop
			calls := 0
			handler := p.wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls++; w.WriteHeader(200) }))
			req := httptest.NewRequest(http.MethodPost, "http://localhost/api/test", nil)
			req.Host = tc.host
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.referer != "" {
				req.Header.Set("Referer", tc.referer)
			}
			if tc.site != "" {
				req.Header.Set("Sec-Fetch-Site", tc.site)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status=%d, want %d; %s", rec.Code, tc.want, rec.Body.String())
			}
			if (calls == 1) != (tc.want == 200) {
				t.Fatalf("handler calls=%d", calls)
			}
			if tc.want == 403 && rec.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Fatal("rejected request granted CORS access")
			}
		})
	}
}

func TestPreflightPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, origin, method, headers string
		want                          int
	}{
		{"trusted", "http://localhost:5173", "POST", "content-type", 204},
		{"untrusted", "https://attacker.example", "POST", "Content-Type", 403},
		{"opaque", "null", "POST", "Content-Type", 403},
		{"unsupported method", "http://localhost:5173", "DELETE", "", 403},
		{"unsupported header", "http://localhost:5173", "POST", "Content-Type, X-Evil", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := NewServer(nil, "", nil).Handler()
			req := httptest.NewRequest(http.MethodOptions, "http://127.0.0.1:8080/api/runtime/live/start", nil)
			req.Header.Set("Origin", tc.origin)
			req.Header.Set("Access-Control-Request-Method", tc.method)
			req.Header.Set("Access-Control-Request-Headers", tc.headers)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status=%d,want %d", rec.Code, tc.want)
			}
			if tc.want == 403 && rec.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Fatal("rejected preflight granted CORS access")
			}
			if tc.want == 204 && rec.Header().Get("Access-Control-Allow-Origin") != tc.origin {
				t.Fatal("missing allowed origin")
			}
		})
	}
}

func TestDuplicateAndEmptyOriginRejected(t *testing.T) {
	for _, origins := range [][]string{{""}, {"http://localhost:5173", "https://attacker.example"}} {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/runtime/pick-log", nil)
		req.Header["Origin"] = origins
		rec := httptest.NewRecorder()
		NewServer(nil, "", nil).Handler().ServeHTTP(rec, req)
		if rec.Code != 403 {
			t.Fatalf("origins=%v: status=%d, want 403", origins, rec.Code)
		}
	}
}

func TestTrustedJSONMutationAndBodylessMutation(t *testing.T) {
	desktop := &recordingDesktop{}
	server := NewServer(nil, "", &appstate.Service{})
	server.SetDesktop(desktop)
	for _, endpoint := range []string{"open-url", "pick-log"} {
		body := ""
		if endpoint == "open-url" {
			body = `{"url":"https://example.org"}`
		}
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/runtime/"+endpoint, strings.NewReader(body))
		req.Header.Set("Origin", "http://localhost:5173")
		if body != "" {
			req.Header.Set("Content-Type", "application/json; charset=utf-8")
		}
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("%s status=%d, want 200: %s", endpoint, rec.Code, rec.Body.String())
		}
	}
	if desktop.opened != 1 || desktop.picked != 1 {
		t.Fatalf("native calls: opened=%d picked=%d", desktop.opened, desktop.picked)
	}
}

func TestUntrustedBodylessRequestsCannotStartOrStopLive(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	logPath := filepath.Join(dir, "Player.log")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.Init(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	service, err := appstate.NewService(appstate.Options{Store: db.NewStore(database), DBPath: dbPath, SupportDir: filepath.Join(dir, "support"), DefaultLogPath: logPath, DefaultPrevLogPath: filepath.Join(dir, "Player-prev.log"), DefaultPollInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	defer service.StopLive()
	handler := NewServer(db.NewStore(database), "", service).Handler()
	reject := func(action string, wantRunning bool) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "http://localhost:8080/api/runtime/live/"+action, nil)
		req.Header.Set("Origin", "https://attacker.example")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != 403 {
			t.Fatalf("%s status=%d, want 403", action, rec.Code)
		}
		if service.Status().LiveRunning != wantRunning {
			t.Fatalf("%s changed live state", action)
		}
	}
	reject("start", false)
	if _, err := service.StartLive(); err != nil {
		t.Fatal(err)
	}
	reject("stop", true)
}

func TestJSONMediaTypeRequiredAcrossEndpoints(t *testing.T) {
	// Each endpoint would parse JSON or cause a side effect with a valid body.
	// A nil store is deliberate: dispatch beyond the media-type gate must fail.
	server := NewServer(nil, "", &appstate.Service{})
	server.SetDesktop(&recordingDesktop{})
	for _, path := range []string{"/api/activity", "/api/runtime/config", "/api/runtime/import", "/api/runtime/autostart", "/api/runtime/reveal", "/api/runtime/open-url", "/api/matches/1/opponent-archetype"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "http://localhost:8080"+path, strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "text/plain")
			rec := httptest.NewRecorder()
			server.Handler().ServeHTTP(rec, req)
			if rec.Code != 415 {
				t.Fatalf("status=%d, want 415: %s", rec.Code, rec.Body.String())
			}
		})
	}
}
