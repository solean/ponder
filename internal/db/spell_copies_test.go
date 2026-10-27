package db

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/solean/ponder/internal/model"
)

func TestSpellCopyMigrationRepairsLegacyArchivesAndObservations(t *testing.T) {
	ctx := context.Background()
	store := newReplayArchiveTestStore(t)
	tx := mustBeginTx(t, store)
	const arenaID = "legacy-copies"
	matchID, err := store.UpsertMatchStart(ctx, tx, arenaID, "Ladder", 1, "2026-03-12T19:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range []struct{ id, card, seat int64 }{{101, 5001, 2}, {102, 5001, 2}, {103, 5002, 2}, {201, 6001, 1}, {202, 6002, 1}} {
		if err := store.UpsertMatchCardPlay(ctx, tx, arenaID, 1, spec.id, spec.card, spec.seat, 3, "main1", "stack", "", "gre"); err != nil {
			t.Fatal(err)
		}
		if spec.seat == 2 {
			if err := store.UpsertMatchOpponentCardInstance(ctx, tx, arenaID, 1, spec.id, spec.card, "", "gre"); err != nil {
				t.Fatal(err)
			}
		}
	}
	objects := []model.MatchReplayFrameObjectRow{}
	for _, spec := range []struct {
		id, card, seat int64
		copy           bool
	}{{101, 5001, 2, false}, {102, 5001, 2, true}, {103, 5002, 2, true}, {201, 6001, 1, true}, {202, 6002, 1, true}} {
		object := liveCountsTestObject(spec.id, spec.card, spec.seat, "stack")
		if spec.copy {
			object.DetailsJSON = `{"isCopy":true}` // legacy payload has no typed flag
		}
		objects = append(objects, object)
	}
	insertReplayTestFrame(t, store, tx, arenaID, 1, 1, 3, `{"1":20,"2":20}`, objects)
	annotations := `{"annotations":[{"type":["AnnotationType_ZoneTransfer"],"affectedIds":[202],"details":[{"key":"category","valueString":["CastSpell"]}]}]}`
	if _, err := tx.Exec(`UPDATE match_replay_frames SET annotations_json = ? WHERE match_id = ?`, annotations, matchID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ArchiveMatchReplay(ctx, tx, arenaID); err != nil {
		t.Fatal(err)
	}
	mustCommit(t, tx)
	// Simulate an installation from before the new columns and backfill marker.
	for _, table := range []string{"match_replay_frame_objects", "match_card_plays"} {
		for _, column := range []string{"is_copy", "cast_confirmed"} {
			if _, err := store.db.Exec(`ALTER TABLE ` + table + ` DROP COLUMN ` + column); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := store.db.Exec(`DELETE FROM app_metadata WHERE key = 'spell_copies_backfill_v1'`); err != nil {
		t.Fatal(err)
	}
	if err := Init(ctx, store.db); err != nil {
		t.Fatal(err)
	}
	if count, err := store.RefreshPendingMatchAnalytics(ctx); err != nil || count != 1 {
		t.Fatalf("backfill = %d, %v; want one refreshed match", count, err)
	}
	quantities, err := store.ListMatchOpponentCardQuantities(ctx, []int64{matchID})
	if err != nil {
		t.Fatal(err)
	}
	if len(quantities[matchID]) != 1 || quantities[matchID][5001] != 1 {
		t.Fatalf("legacy copy evidence survived: %v", quantities)
	}
	var copies, spells, cardStats int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM match_card_plays WHERE is_copy = 1`).Scan(&copies); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT SUM(spells_cast) FROM game_turn_stats`).Scan(&spells); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM game_card_stats`).Scan(&cardStats); err != nil {
		t.Fatal(err)
	}
	if copies != 4 || spells != 1 || cardStats != 0 {
		t.Fatalf("backfilled copies=%d spells=%d deck-card stats=%d; want 4,1,0", copies, spells, cardStats)
	}
	archived, found, err := loadReplayArchivePayload(ctx, store.db, matchID)
	if err != nil || !found {
		t.Fatalf("load backfilled archive: %v, %v", found, err)
	}
	if !archived[0].Objects[1].IsCopy || !archived[0].Objects[4].CastConfirmed {
		wire, _ := json.Marshal(archived[0].Objects)
		t.Fatalf("archive flags not persisted: %s", wire)
	}
	// Reopening and rerunning maintenance must not invalidate or duplicate data.
	if err := Init(ctx, store.db); err != nil {
		t.Fatal(err)
	}
	if count, err := store.RefreshPendingMatchAnalytics(ctx); err != nil || count != 0 {
		t.Fatalf("second backfill = %d, %v; want no work", count, err)
	}
}

func TestOpponentQuantitiesPreferDerivedAndKeepLegacyFallback(t *testing.T) {
	ctx := context.Background()
	store := newReplayArchiveTestStore(t)
	tx := mustBeginTx(t, store)
	const arenaID = "opponent-quantities"
	matchID, err := store.UpsertMatchStart(ctx, tx, arenaID, "Ladder", 1, "")
	if err != nil {
		t.Fatal(err)
	}
	for id := int64(1); id <= 6; id++ {
		if err := store.UpsertMatchOpponentCardInstance(ctx, tx, arenaID, 1, id, 5001, "", "gre"); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.UpsertMatchOpponentCardInstance(ctx, tx, arenaID, 1, 7, 5002, "", "legacy"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO match_opponent_card_counts (match_id, card_id, max_copies, derived_at) VALUES (?, 5001, 1, '2026-03-12T19:00:00Z')`, matchID); err != nil {
		t.Fatal(err)
	}
	mustCommit(t, tx)
	// Include missing IDs and a second batch to exercise placeholder binding.
	ids := []int64{matchID, matchID}
	for id := int64(100); id < 1010; id++ {
		ids = append(ids, id)
	}
	quantities, err := store.ListMatchOpponentCardQuantities(ctx, ids)
	if err != nil {
		t.Fatal(err)
	}
	if len(quantities) != 1 || len(quantities[matchID]) != 2 || quantities[matchID][5001] != 1 || quantities[matchID][5002] != 1 {
		t.Fatalf("unexpected quantities: %v", quantities)
	}
	if empty, err := store.ListMatchOpponentCardQuantities(ctx, nil); err != nil || len(empty) != 0 {
		t.Fatalf("empty request = %v, %v", empty, err)
	}
}
