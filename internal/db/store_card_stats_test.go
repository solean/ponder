package db

import (
	"context"
	"testing"
)

func TestMigrateCardStatSnapshotsKeepsAllPlayersRatings(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	database := openTempSQLiteDB(t)
	mustExec(t, database, `CREATE TABLE card_stat_snapshots (
		expansion TEXT NOT NULL,
		event_type TEXT NOT NULL,
		payload_json TEXT NOT NULL,
		fetched_at TEXT NOT NULL,
		PRIMARY KEY(expansion, event_type)
	)`)
	mustExec(t, database, `INSERT INTO card_stat_snapshots VALUES ('FRA', 'PremierDraft', '[{"arenaId":101}]', '2026-10-18T18:53:41Z')`)

	if err := migrateCardStatSnapshots(ctx, database); err != nil {
		t.Fatalf("migrateCardStatSnapshots: %v", err)
	}
	if err := migrateCardStatSnapshots(ctx, database); err != nil {
		t.Fatalf("migrateCardStatSnapshots idempotent run: %v", err)
	}

	store := NewStore(database)
	snapshot, err := store.LookupCardStatSnapshot(ctx, "FRA", "PremierDraft", "")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot == nil || len(snapshot.Cards) != 1 || snapshot.Cards[0].ArenaID != 101 {
		t.Fatalf("all-players snapshot after migration = %+v", snapshot)
	}
	if top, err := store.LookupCardStatSnapshot(ctx, "FRA", "PremierDraft", "top"); err != nil || top != nil {
		t.Fatalf("top-player snapshot = %+v, %v; want none", top, err)
	}
}
