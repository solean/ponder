package db

import (
	"context"
	"testing"

	"github.com/solean/ponder/internal/model"
)

func TestLiveManaStateScopesLatestGameAndController(t *testing.T) {
	ctx := context.Background()
	database := openTempSQLiteDB(t)
	if err := Init(ctx, database); err != nil {
		t.Fatal(err)
	}
	store := NewStore(database)
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	id, err := store.UpsertMatchStart(ctx, tx, "mana", "Ladder", 1, "2026-11-02T12:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	own := liveCountsTestObject(1, 10, 1, "battlefield")
	stolen := liveCountsTestObject(2, 11, 2, "battlefield")
	stolen.ControllerSeatID = int64Ptr(1)
	lost := liveCountsTestObject(3, 12, 1, "battlefield")
	lost.ControllerSeatID = int64Ptr(2)
	hand := liveCountsTestObject(4, 13, 1, "hand")
	for _, frame := range []struct {
		state   int64
		objects []model.MatchReplayFrameObjectRow
	}{
		{1, []model.MatchReplayFrameObjectRow{own}}, {2, []model.MatchReplayFrameObjectRow{own, stolen, lost, hand}},
	} {
		if _, err := store.ReplaceMatchReplayFrame(ctx, tx, "mana", 1, frame.state, 0, 1, "full", "play", "main1", "", "", "2026-11-02T12:00:00Z", "test", nil, nil, nil, frame.objects); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.UpdateLiveManaPool(ctx, tx, "mana", 1, 1, 2, []model.ManaPoolEntry{{Color: "ManaColor_Red", Count: 2}}); err != nil {
		t.Fatal(err)
	}
	// An older replay import must not overwrite the current pool.
	if err := store.UpdateLiveManaPool(ctx, tx, "mana", 1, 1, 1, nil); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	objects, pool, available, known, err := store.GetLiveManaState(ctx, id, 1)
	if err != nil || !available || !known || len(objects) != 2 || len(pool) != 1 || pool[0].Count != 2 {
		t.Fatalf("state: %v %v %v %v %v", objects, pool, available, known, err)
	}
	_, _, available, known, err = store.GetLiveManaState(ctx, id, 2)
	if err != nil || available || known {
		t.Fatalf("new game: %v %v %v", available, known, err)
	}
	tx, err = store.BeginTx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReplaceMatchReplayFrame(ctx, tx, "mana", 2, 1, 0, 1, "full", "play", "main1", "", "", "2026-11-02T12:01:00Z", "test", nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateLiveManaPool(ctx, tx, "mana", 2, 1, 1, nil); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	objects, pool, available, known, err = store.GetLiveManaState(ctx, id, 2)
	if err != nil || !available || !known || len(objects) != 0 || len(pool) != 0 {
		t.Fatalf("empty game: %v %v %v %v %v", objects, pool, available, known, err)
	}
}
