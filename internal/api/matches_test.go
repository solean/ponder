package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/solean/ponder/internal/db"
	"github.com/solean/ponder/internal/model"
)

func TestParseMatchListLimit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
		want int64
	}{
		{"missing falls back to default", "", matchListDefaultLimit},
		{"unparseable falls back to default", "abc", matchListDefaultLimit},
		{"zero falls back to default", "0", matchListDefaultLimit},
		{"negative falls back to default", "-1", matchListDefaultLimit},
		{"in range is honored", "37", 37},
		{"max is honored", "20000", matchListMaxLimit},
		{"above max clamps", "999999", matchListMaxLimit},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseMatchListLimit(tc.raw); got != tc.want {
				t.Fatalf("parseMatchListLimit(%q) = %d, want %d", tc.raw, got, tc.want)
			}
		})
	}
}

// seedMatchListStore commits `count` ended matches, alternating win and loss,
// under a single event so the list endpoint has something to truncate.
func seedMatchListStore(t *testing.T, count int) *db.Store {
	t.Helper()

	ctx := context.Background()
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.Init(ctx, database); err != nil {
		t.Fatalf("init db: %v", err)
	}

	store := db.NewStore(database)
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	for index := 0; index < count; index++ {
		arenaID := fmt.Sprintf("match-%04d", index)
		startedAt := fmt.Sprintf("2026-03-12T%02d:%02d:00Z", index/60%24, index%60)
		if _, err := store.UpsertMatchStart(ctx, tx, arenaID, "Ladder", 1, startedAt); err != nil {
			_ = tx.Rollback()
			t.Fatalf("UpsertMatchStart(%s): %v", arenaID, err)
		}
		winningTeamID := int64(1 + index%2)
		if _, _, _, err := store.UpdateMatchEnd(ctx, tx, arenaID, 1, winningTeamID, 9, 420, "Game", startedAt); err != nil {
			_ = tx.Rollback()
			t.Fatalf("UpdateMatchEnd(%s): %v", arenaID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit matches: %v", err)
	}
	return store
}

func getMatchList(t *testing.T, store *db.Store, target string) model.MatchListResponse {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	NewServer(store, "", nil).Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status = %d, want %d; body: %s", target, rec.Code, http.StatusOK, rec.Body.String())
	}

	var response model.MatchListResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("GET %s: decode response: %v", target, err)
	}
	return response
}

func TestMatchesEndpointReportsTotalBeyondTheRequestedPage(t *testing.T) {
	t.Parallel()

	const seeded = 205
	store := seedMatchListStore(t, seeded)

	cases := []struct {
		name     string
		target   string
		wantRows int
	}{
		{"explicit small limit truncates", "/api/matches?limit=1", 1},
		{"limit above max clamps without erroring", "/api/matches?limit=999999", seeded},
		{"unparseable limit uses the default", "/api/matches?limit=abc", matchListDefaultLimit},
		{"zero limit uses the default", "/api/matches?limit=0", matchListDefaultLimit},
		{"negative limit uses the default", "/api/matches?limit=-1", matchListDefaultLimit},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response := getMatchList(t, store, tc.target)
			if len(response.Matches) != tc.wantRows {
				t.Fatalf("len(matches) = %d, want %d", len(response.Matches), tc.wantRows)
			}
			if response.Total != seeded {
				t.Fatalf("total = %d, want %d (unaffected by limit)", response.Total, seeded)
			}
		})
	}
}

func TestMatchesEndpointTotalRespectsResultFilter(t *testing.T) {
	t.Parallel()

	store := seedMatchListStore(t, 10)

	response := getMatchList(t, store, "/api/matches?limit=2&result=win")
	if len(response.Matches) != 2 {
		t.Fatalf("len(matches) = %d, want 2", len(response.Matches))
	}
	if response.Total != 5 {
		t.Fatalf("total = %d, want 5 wins", response.Total)
	}
	for _, match := range response.Matches {
		if match.Result != "win" {
			t.Fatalf("match %d result = %q, want win", match.ID, match.Result)
		}
	}
}
