package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/solean/ponder/internal/db"
	"github.com/solean/ponder/internal/model"
)

func TestLivePlayerDrawCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		turn     int64
		playDraw string
		want     int64
	}{
		{name: "pre-game", turn: 0, playDraw: "play", want: 0},
		{name: "negative sentinel", turn: -1, playDraw: "draw", want: 0},
		{name: "on play first turn skips draw", turn: 1, playDraw: "play", want: 0},
		{name: "on play opposing first turn", turn: 2, playDraw: "play", want: 0},
		{name: "on play second turn", turn: 3, playDraw: "play", want: 1},
		{name: "on play opposing second turn", turn: 4, playDraw: "play", want: 1},
		{name: "on draw before first draw", turn: 1, playDraw: "draw", want: 0},
		{name: "on draw first turn", turn: 2, playDraw: "draw", want: 1},
		{name: "on draw before second turn", turn: 3, playDraw: "draw", want: 1},
		{name: "on draw second turn", turn: 4, playDraw: "draw", want: 2},
		{name: "unknown uses completed full turns", turn: 5, playDraw: "", want: 2},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := livePlayerDrawCount(test.turn, test.playDraw); got != test.want {
				t.Fatalf("livePlayerDrawCount(%d, %q) = %d, want %d", test.turn, test.playDraw, got, test.want)
			}
		})
	}
}

func TestLiveLibraryEstimateUsesPlayerDrawsAndKeepsLowerBound(t *testing.T) {
	t.Parallel()

	if got := liveLibraryEstimate(60, 4, "play"); got != 52 {
		t.Fatalf("on-play estimate = %d, want 52", got)
	}
	if got := liveLibraryEstimate(60, 4, "draw"); got != 51 {
		t.Fatalf("on-draw estimate = %d, want 51", got)
	}
	if got := liveLibraryEstimate(7, 40, "draw"); got != 1 {
		t.Fatalf("bounded estimate = %d, want 1", got)
	}
}

func TestLiveEndpointProjectsTurnButStoreRetainsArenaTurn(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	if err := db.Init(ctx, database); err != nil {
		t.Fatalf("init db: %v", err)
	}

	store := db.NewStore(database)
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if _, err := store.UpsertMatchStart(ctx, tx, "live-turn-projection", "Ladder", 1,
		time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("upsert match: %v", err)
	}
	if err := store.UpsertMatchCardPlay(ctx, tx, "live-turn-projection", 1, 101, 5001, 1, 4,
		"main1", "battlefield", time.Now().UTC().Format(time.RFC3339Nano), "test"); err != nil {
		t.Fatalf("upsert card play: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	rows, err := store.ListMatches(ctx, 1, "", "")
	if err != nil || len(rows) != 1 {
		t.Fatalf("list matches = %#v, %v", rows, err)
	}
	_, rawTurn, err := store.GetLiveProgress(ctx, rows[0].ID)
	if err != nil {
		t.Fatalf("get raw live progress: %v", err)
	}
	if rawTurn != 4 {
		t.Fatalf("stored live turn = %d, want raw Arena turn 4", rawTurn)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/live", nil)
	rec := httptest.NewRecorder()
	NewServer(store, "", nil).Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var response struct {
		Live *model.LiveMatch `json:"live"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Live == nil {
		t.Fatal("live response is nil")
	}
	if response.Live.TurnNumber != 2 {
		t.Fatalf("displayed live turn = %d, want full turn 2", response.Live.TurnNumber)
	}
}
