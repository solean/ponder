package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/solean/ponder/internal/db"
	"github.com/solean/ponder/internal/model"
)

func TestEconomyEndpointReturnsLatestAndHistory(t *testing.T) {
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
	for index, snapshot := range []db.EconomySnapshotRecord{
		{ObservedAt: "2026-07-12T18:40:38Z", Gold: 1000, Gems: 200},
		{ObservedAt: "2026-07-12T19:14:09Z", Gold: 1250, Gems: 200},
	} {
		if _, _, err := store.InsertEconomySnapshot(ctx, tx, "Player.log", int64(index+1), snapshot); err != nil {
			_ = tx.Rollback()
			t.Fatalf("insert snapshot %d: %v", index, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit snapshots: %v", err)
	}

	req := localRequest(http.MethodGet, "/api/economy", nil)
	rec := httptest.NewRecorder()
	NewServer(store, "", nil).Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var response model.EconomyHistory
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.History) != 2 {
		t.Fatalf("history length = %d, want 2", len(response.History))
	}
	if response.Latest == nil || response.Latest.Gold != 1250 || response.Latest.ObservedAt != "2026-07-12T19:14:09Z" {
		t.Fatalf("latest = %#v", response.Latest)
	}
}
