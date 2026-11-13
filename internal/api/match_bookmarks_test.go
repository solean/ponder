package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMatchBookmarkEndpoint(t *testing.T) {
	t.Parallel()

	store := seedMatchListStore(t, 2)
	handler := NewServer(store, "", nil).Handler()
	matches := getMatchList(t, store, "/api/matches").Matches
	target, other := matches[0].ID, matches[1].ID

	post := func(matchID int64, body string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		path := fmt.Sprintf("/api/matches/%d/bookmark", matchID)
		handler.ServeHTTP(rec, localRequest(http.MethodPost, path, strings.NewReader(body)))
		return rec
	}
	savedByID := func() map[int64]bool {
		t.Helper()
		out := map[int64]bool{}
		for _, match := range getMatchList(t, store, "/api/matches").Matches {
			out[match.ID] = match.Saved
		}
		return out
	}

	if saved := savedByID(); saved[target] || saved[other] {
		t.Fatalf("fresh matches reported saved: %v", saved)
	}

	for range 2 { // saving twice is a no-op, not an error
		if rec := post(target, `{"saved":true}`); rec.Code != http.StatusOK {
			t.Fatalf("save: status=%d body=%s", rec.Code, rec.Body.String())
		}
	}
	if saved := savedByID(); !saved[target] || saved[other] {
		t.Fatalf("after save: %v, want only %d saved", saved, target)
	}

	detail := httptest.NewRecorder()
	handler.ServeHTTP(detail, localRequest(http.MethodGet, fmt.Sprintf("/api/matches/%d", target), nil))
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), `"saved":true`) {
		t.Fatalf("detail did not report saved: status=%d", detail.Code)
	}

	if rec := post(target, `{"saved":false}`); rec.Code != http.StatusOK {
		t.Fatalf("unsave: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if saved := savedByID(); saved[target] {
		t.Fatalf("after unsave: %v", saved)
	}

	if rec := post(other+100, `{"saved":true}`); rec.Code != http.StatusNotFound {
		t.Fatalf("missing match: status=%d, want 404", rec.Code)
	}
	if rec := post(target, `{}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing field: status=%d, want 400", rec.Code)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, localRequest(http.MethodGet, fmt.Sprintf("/api/matches/%d/bookmark", target), nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET bookmark: status=%d, want 405", rec.Code)
	}
}
