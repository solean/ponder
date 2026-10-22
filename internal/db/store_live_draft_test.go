package db

import (
	"context"
	"database/sql"
	"fmt"
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
	// Join and pack notifications share Arena's log clock, including its skew.
	draftStarted := time.Now().Add(7 * time.Hour).UTC()

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
		sessionID, err := store.EnsureDraftSession(ctx, tx, "", ptrString("draft-live"), false, draftStarted.Format(time.RFC3339))
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
	// has no event name until the draft ends. The join's log timestamp can
	// run hours ahead of Ponder's clock (Arena logging UTC read as local).
	write(func(tx *sql.Tx) {
		joinedAt := draftStarted.Add(-time.Minute).Format(time.RFC3339)
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

func TestGetLiveDraftBoundsEventLookupOnArenaClock(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		joinDeltas []time.Duration
		wantEvent  string
	}{
		{name: "missing join does not reuse last week's event", joinDeltas: []time.Duration{-7 * 24 * time.Hour}},
		{name: "future join belongs to another draft", joinDeltas: []time.Duration{time.Minute}},
		{name: "latest prior join wins over insertion order", joinDeltas: []time.Duration{-time.Minute, -10 * time.Minute}, wantEvent: "PremierDraft_SET0"},
		{name: "missing timestamps do not guess an event"},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
			defer tx.Rollback()
			// Both sides are shifted from Ponder's clock by the same amount.
			arenaStart := time.Now().Add(7 * time.Hour).UTC()
			for i, delta := range tc.joinDeltas {
				event := fmt.Sprintf("PremierDraft_SET%d", i)
				if err := store.UpsertEventRunJoin(ctx, tx, event, "Gem", 1500, arenaStart.Add(delta).Format(time.RFC3339)); err != nil {
					t.Fatal(err)
				}
			}
			startedAt := arenaStart.Format(time.RFC3339)
			if len(tc.joinDeltas) == 0 {
				startedAt = ""
				if err := store.UpsertEventRunJoin(ctx, tx, "PremierDraft_UNKNOWN", "Gem", 1500, ""); err != nil {
					t.Fatal(err)
				}
			}
			session, err := store.EnsureDraftSession(ctx, tx, "", ptrString("draft-current"), false, startedAt)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.InsertDraftPick(ctx, tx, session, 1, 1, nil, []int64{101, 102}, startedAt); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			draft, err := store.GetLiveDraft(ctx, time.Now())
			if err != nil || draft == nil {
				t.Fatalf("GetLiveDraft = %+v, %v", draft, err)
			}
			if draft.EventName != tc.wantEvent {
				t.Fatalf("event = %q, want %q", draft.EventName, tc.wantEvent)
			}
		})
	}
}

func TestGetLiveDraftRequiresLaterMatchEventAndObservation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		matchEventAgo time.Duration
		observedAgo   time.Duration
		importOlder   bool
		wantLive      bool
	}{
		{name: "historical match imported after draft", matchEventAgo: 24 * time.Hour, observedAgo: time.Second, wantLive: true},
		{name: "older match with future log clock", matchEventAgo: -7 * time.Hour, observedAgo: 3 * time.Minute, wantLive: true},
		{name: "later match hides draft", matchEventAgo: time.Minute, observedAgo: time.Minute},
		{name: "later match still counts after historical import", matchEventAgo: time.Minute, observedAgo: time.Minute, importOlder: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
			defer tx.Rollback()
			now := time.Now().UTC()
			session, err := store.EnsureDraftSession(ctx, tx, "QuickDraft_TST", nil, true, "")
			if err != nil {
				t.Fatal(err)
			}
			if err := store.InsertDraftPick(ctx, tx, session, 0, 0, nil, []int64{101, 102}, ""); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.ExecContext(ctx, `UPDATE draft_picks SET created_at = ? WHERE draft_session_id = ?`, utcTimestamp(now.Add(-2*time.Minute)), session); err != nil {
				t.Fatal(err)
			}
			match, err := store.UpsertMatchStart(ctx, tx, "match", "Play", 1, utcTimestamp(now.Add(-tc.matchEventAgo)))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tx.ExecContext(ctx, `UPDATE matches SET created_at = ? WHERE id = ?`, utcTimestamp(now.Add(-tc.observedAgo)), match); err != nil {
				t.Fatal(err)
			}
			if tc.importOlder {
				if _, err := store.UpsertMatchStart(ctx, tx, "historical-match", "Play", 1, utcTimestamp(now.Add(-24*time.Hour))); err != nil {
					t.Fatal(err)
				}
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			draft, err := store.GetLiveDraft(ctx, now)
			if err != nil || (draft != nil) != tc.wantLive {
				t.Fatalf("GetLiveDraft = %+v, %v; want live = %v", draft, err, tc.wantLive)
			}
		})
	}
}
