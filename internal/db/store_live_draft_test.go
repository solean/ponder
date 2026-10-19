package db

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func TestGetLiveDraftFollowsPlayerDraftPicks(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	database := openTempSQLiteDB(t)
	if err := Init(ctx, database); err != nil {
		t.Fatalf("Init: %v", err)
	}
	store := NewStore(database)

	write := func(apply func(tx *sql.Tx)) {
		t.Helper()
		tx, err := store.BeginTx(ctx)
		if err != nil {
			t.Fatalf("BeginTx: %v", err)
		}
		apply(tx)
		if err := tx.Commit(); err != nil {
			t.Fatalf("Commit: %v", err)
		}
	}
	notify := func(tx *sql.Tx, pack, pick int64, cards []int64) {
		t.Helper()
		sessionID, err := store.EnsureDraftSession(ctx, tx, "", ptrString("draft-live"), false, "")
		if err != nil {
			t.Fatalf("EnsureDraftSession: %v", err)
		}
		if err := store.InsertDraftPick(ctx, tx, sessionID, pack, pick, nil, cards, ""); err != nil {
			t.Fatalf("InsertDraftPick(notify): %v", err)
		}
	}
	makePick := func(tx *sql.Tx, pack, pick, card int64) {
		t.Helper()
		sessionID, err := store.EnsureDraftSession(ctx, tx, "", ptrString("draft-live"), false, "")
		if err != nil {
			t.Fatalf("EnsureDraftSession: %v", err)
		}
		if err := store.InsertDraftPick(ctx, tx, sessionID, pack, pick, []int64{card}, nil, ""); err != nil {
			t.Fatalf("InsertDraftPick(pick): %v", err)
		}
	}

	if got, err := store.GetLiveDraft(ctx, time.Now()); err != nil || got != nil {
		t.Fatalf("GetLiveDraft(empty) = %+v, %v; want nil", got, err)
	}

	// Arena logs the event join before the first pack; the session itself
	// has no event name until the draft ends.
	write(func(tx *sql.Tx) {
		joinedAt := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
		if err := store.UpsertEventRunJoin(ctx, tx, "PremierDraft_FRA_20260929", "Gem", 1500, joinedAt); err != nil {
			t.Fatalf("UpsertEventRunJoin: %v", err)
		}
		notify(tx, 1, 1, []int64{101, 102, 103})
	})
	got, err := store.GetLiveDraft(ctx, time.Now())
	if err != nil || got == nil {
		t.Fatalf("GetLiveDraft(pack shown) = %+v, %v; want a draft", got, err)
	}
	if got.EventName != "PremierDraft_FRA_20260929" || got.PackNumber != 1 || got.PickNumber != 1 || got.Waiting || len(got.Pack) != 3 || got.Pack[0].CardID != 101 {
		t.Fatalf("GetLiveDraft(pack shown) = %+v", got)
	}

	write(func(tx *sql.Tx) { makePick(tx, 1, 1, 102) })
	got, err = store.GetLiveDraft(ctx, time.Now())
	if err != nil || got == nil || !got.Waiting || len(got.Pack) != 0 || got.PickNumber != 1 {
		t.Fatalf("GetLiveDraft(after pick) = %+v, %v; want waiting", got, err)
	}
	if got, err := store.GetLiveDraft(ctx, time.Now().Add(liveDraftWaitWindow+time.Minute)); err != nil || got != nil {
		t.Fatalf("GetLiveDraft(next pack never came) = %+v, %v; want nil", got, err)
	}

	write(func(tx *sql.Tx) { notify(tx, 1, 2, []int64{201, 202}) })
	got, err = store.GetLiveDraft(ctx, time.Now().Add(liveDraftPickWindow-time.Minute))
	if err != nil || got == nil || got.Waiting || got.PickNumber != 2 || len(got.Pack) != 2 {
		t.Fatalf("GetLiveDraft(second pack) = %+v, %v", got, err)
	}
	if got, err := store.GetLiveDraft(ctx, time.Now().Add(liveDraftPickWindow+time.Minute)); err != nil || got != nil {
		t.Fatalf("GetLiveDraft(abandoned pack) = %+v, %v; want nil", got, err)
	}

	// The last card of the final pack ends the draft at once.
	write(func(tx *sql.Tx) {
		notify(tx, 3, 14, []int64{314})
		makePick(tx, 3, 14, 314)
	})
	if got, err := store.GetLiveDraft(ctx, time.Now()); err != nil || got != nil {
		t.Fatalf("GetLiveDraft(final pick made) = %+v, %v; want nil", got, err)
	}
}

func TestGetLiveDraftNumbersBotDraftsFromOne(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	database := openTempSQLiteDB(t)
	if err := Init(ctx, database); err != nil {
		t.Fatalf("Init: %v", err)
	}
	store := NewStore(database)

	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	sessionID, err := store.EnsureDraftSession(ctx, tx, "QuickDraft_TST_20260901", nil, true, "")
	if err != nil {
		t.Fatalf("EnsureDraftSession: %v", err)
	}
	if err := store.InsertDraftPick(ctx, tx, sessionID, 0, 0, nil, []int64{1001, 1002}, ""); err != nil {
		t.Fatalf("InsertDraftPick: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	got, err := store.GetLiveDraft(ctx, time.Now())
	if err != nil || got == nil {
		t.Fatalf("GetLiveDraft = %+v, %v; want a draft", got, err)
	}
	if got.EventName != "QuickDraft_TST_20260901" || got.PackNumber != 1 || got.PickNumber != 1 || len(got.Pack) != 2 {
		t.Fatalf("GetLiveDraft = %+v", got)
	}
}

func TestGetLiveDraftYieldsToALaterMatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	database := openTempSQLiteDB(t)
	if err := Init(ctx, database); err != nil {
		t.Fatalf("Init: %v", err)
	}
	store := NewStore(database)

	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	sessionID, err := store.EnsureDraftSession(ctx, tx, "QuickDraft_TST_20260901", nil, true, "")
	if err != nil {
		t.Fatalf("EnsureDraftSession: %v", err)
	}
	if err := store.InsertDraftPick(ctx, tx, sessionID, 1, 4, nil, []int64{1001}, ""); err != nil {
		t.Fatalf("InsertDraftPick: %v", err)
	}
	startedAt := time.Now().Add(time.Second).UTC().Format(time.RFC3339Nano)
	if _, err := store.UpsertMatchStart(ctx, tx, "match-after-draft", "Traditional_Ladder", 1, startedAt); err != nil {
		t.Fatalf("UpsertMatchStart: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if got, err := store.GetLiveDraft(ctx, time.Now()); err != nil || got != nil {
		t.Fatalf("GetLiveDraft = %+v, %v; want nil once a match started", got, err)
	}
}
