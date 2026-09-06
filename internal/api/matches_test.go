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

func TestMatchReplayStatusEndpoint(t *testing.T) {
	t.Parallel()

	store := seedMatchListStore(t, 1)
	matches := getMatchList(t, store, "/api/matches").Matches
	matchID := matches[0].ID
	handler := NewServer(store, "", nil).Handler()
	readStatus := func() model.MatchReplayStatus {
		t.Helper()
		target := fmt.Sprintf("/api/matches/%d/replay-status", matchID)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET replay-status: status=%d body=%s", rec.Code, rec.Body.String())
		}
		var status model.MatchReplayStatus
		if err := json.NewDecoder(rec.Body).Decode(&status); err != nil {
			t.Fatalf("decode replay status: %v", err)
		}
		return status
	}
	before := readStatus()
	if !before.Complete {
		t.Fatal("ended match reported incomplete")
	}
	if repeated := readStatus(); repeated != before {
		t.Fatalf("unchanged status was not stable: before=%+v repeated=%+v", before, repeated)
	}

	tx, err := store.BeginTx(context.Background())
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	defer tx.Rollback()
	if _, err := store.ReplaceMatchReplayFrame(
		context.Background(), tx, "match-0000", 1, 10, 0, 1,
		"full", "playing", "main1", "", "", "", "gre",
		nil, nil, nil, nil,
	); err != nil {
		t.Fatalf("ReplaceMatchReplayFrame: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit late frame: %v", err)
	}
	after := readStatus()
	if after.Revision == before.Revision || !after.Complete {
		t.Fatalf("late frame status=%+v, want changed revision and complete", after)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/matches/%d/replay", matchID), nil))
	var frames []model.MatchReplayFrameRow
	if rec.Code != http.StatusOK {
		t.Fatalf("GET replay: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if err := json.NewDecoder(rec.Body).Decode(&frames); err != nil {
		t.Fatalf("replay response is not a frame array: %v", err)
	}
	if len(frames) != 1 || frames[0].GameStateID == nil || *frames[0].GameStateID != 10 {
		t.Fatalf("replay frames=%+v, want late state 10", frames)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/matches/%d/replay-status", matchID+1), nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing match status=%d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}
