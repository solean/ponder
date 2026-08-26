package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestSPAFallback(t *testing.T) {
	assets := fstest.MapFS{
		"index.html":         {Data: []byte("<html>app</html>")},
		"assets/index-ab.js": {Data: []byte("console.log('js')")},
	}

	server := NewServer(nil, "", nil)
	server.SetStaticAssets(assets)
	handler := server.Handler()

	cases := []struct {
		name       string
		path       string
		wantStatus int
		wantBody   string
	}{
		{"root serves index", "/", 200, "<html>app</html>"},
		{"deep link falls back to index", "/matches/675", 200, "<html>app</html>"},
		{"nested deep link falls back", "/drafts/12/picks", 200, "<html>app</html>"},
		{"real asset served as-is", "/assets/index-ab.js", 200, "console.log('js')"},
		{"unknown api path stays 404", "/api/nope", 404, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tc.path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("GET %s: status = %d, want %d", tc.path, rec.Code, tc.wantStatus)
			}
			if tc.wantBody != "" {
				body, _ := io.ReadAll(rec.Body)
				if string(body) != tc.wantBody {
					t.Fatalf("GET %s: body = %q, want %q", tc.path, body, tc.wantBody)
				}
			}
		})
	}
}

func TestRunUpdateCheckUsesPonderRepository(t *testing.T) {
	var requestedURL string
	server := NewServer(nil, "", nil)
	server.httpClient = &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			requestedURL = req.URL.String()
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("")),
				Request:    req,
			}, nil
		}),
	}

	result := server.runUpdateCheck(context.Background())
	const wantURL = "https://api.github.com/repos/solean/ponder/releases/latest"
	if requestedURL != wantURL {
		t.Fatalf("update URL = %q, want %q", requestedURL, wantURL)
	}
	if result.Note != "no releases published yet" {
		t.Fatalf("update note = %q, want %q", result.Note, "no releases published yet")
	}
}

func TestScryfallRequestsStopDuringRateLimitCooldown(t *testing.T) {
	requestCount := 0
	server := NewServer(nil, "", nil)
	server.httpClient = &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			requestCount++
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     http.Header{"Retry-After": []string{"60"}},
				Body:       io.NopCloser(strings.NewReader("rate limited")),
				Request:    req,
			}, nil
		}),
	}

	firstRequest, err := http.NewRequest(http.MethodGet, "https://api.scryfall.com/cards/arena/1", nil)
	if err != nil {
		t.Fatalf("build first Scryfall request: %v", err)
	}
	firstResponse, err := server.doScryfallRequest(firstRequest)
	if err != nil {
		t.Fatalf("first Scryfall request: %v", err)
	}
	firstResponse.Body.Close()

	secondRequest, err := http.NewRequest(http.MethodGet, "https://api.scryfall.com/cards/arena/2", nil)
	if err != nil {
		t.Fatalf("build second Scryfall request: %v", err)
	}
	if _, err := server.doScryfallRequest(secondRequest); !errors.Is(err, errScryfallCooldown) {
		t.Fatalf("second Scryfall request error = %v, want cooldown", err)
	}
	if requestCount != 1 {
		t.Fatalf("Scryfall network requests = %d, want 1", requestCount)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
