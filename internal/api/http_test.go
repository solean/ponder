package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
)

// Existing endpoint fixtures use the same host/media type as the frontend.
// Boundary tests construct raw requests themselves to exercise rejected input.
func localRequest(method, target string, body io.Reader) *http.Request {
	if strings.HasPrefix(target, "/") {
		target = "http://127.0.0.1:8080" + target
	}
	req := httptest.NewRequest(method, target, body)
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}
