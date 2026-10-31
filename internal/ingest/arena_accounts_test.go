package ingest

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

func accountCompletedMatch(id, persona, name string) string {
	return strings.Replace(checkpointCompletedMatch(id, persona), `"playerName":"Self"`, `"playerName":"`+name+`"`, 1)
}

// Two accounts sharing one log: A, then B, then A again.
func arenaAccountsFixture() []string {
	return []string{
		`[UnityCrossThreadLogger]7/12/2026 11:40:38 AM`,
		`{"PersonaId":"ACCOUNT_A","screenName":"Alpha"}`,
		`<== StartHook(hook-a1)`,
		`{"InventoryInfo":{"SeqId":1,"Gems":1000,"Gold":500,"Changes":[]}}`,
		accountCompletedMatch("match-a1", "ACCOUNT_A", "Alpha"),
		`<== RankGetCombinedRankInfo(rank-a1)`,
		`{"constructedLevel":2}`,
		`[UnityCrossThreadLogger]7/12/2026 11:50:00 AM`,
		`{"PersonaId":"ACCOUNT_B","screenName":"Beta"}`,
		`<== StartHook(hook-b1)`,
		`{"InventoryInfo":{"SeqId":1,"Gems":40,"Gold":90000,"Changes":[]}}`,
		accountCompletedMatch("match-b1", "ACCOUNT_B", "Beta"),
		`<== RankGetCombinedRankInfo(rank-b1)`,
		`{"constructedLevel":4}`,
		`[UnityCrossThreadLogger]7/12/2026 12:00:00 PM`,
		`{"PersonaId":"ACCOUNT_A","screenName":"Alpha"}`,
		`<== StartHook(hook-a2)`,
		`{"InventoryInfo":{"SeqId":2,"Gems":1000,"Gold":500,"Changes":[]}}`,
		accountCompletedMatch("match-a2", "ACCOUNT_A", "Alpha"),
		`<== RankGetCombinedRankInfo(rank-a2)`,
		`{"constructedLevel":3}`,
	}
}

func TestIngestKeepsArenaAccountsSeparate(t *testing.T) {
	ctx := context.Background()
	database, store, logPath := checkpointDatabase(t)
	if err := writeLogLines(logPath, arenaAccountsFixture(), false); err != nil {
		t.Fatal(err)
	}
	if _, err := NewParser(store).ParseFile(ctx, logPath, false); err != nil {
		t.Fatal(err)
	}

	for match, want := range map[string]string{"match-a1": "ACCOUNT_A", "match-b1": "ACCOUNT_B", "match-a2": "ACCOUNT_A"} {
		var matchPersona, rankPersona string
		if err := database.QueryRow(`
			SELECT m.persona_id, r.persona_id
			FROM matches m JOIN match_rank_snapshots r ON r.match_id = m.id
			WHERE m.arena_match_id = ?
		`, match).Scan(&matchPersona, &rankPersona); err != nil {
			t.Fatalf("%s: %v", match, err)
		}
		if matchPersona != want || rankPersona != want {
			t.Fatalf("%s tagged match=%q rank=%q, want %q", match, matchPersona, rankPersona, want)
		}
	}

	// Each account's rank chain skips the other account's snapshots.
	for match, want := range map[string]sql.NullString{
		"match-a1": {},
		"match-b1": {},
		"match-a2": {String: "match-a1", Valid: true},
	} {
		var prev sql.NullString
		if err := database.QueryRow(`
			SELECT pm.arena_match_id
			FROM match_rank_snapshots r
			JOIN matches m ON m.id = r.match_id
			LEFT JOIN match_rank_snapshots pr ON pr.id = r.prev_snapshot_id
			LEFT JOIN matches pm ON pm.id = pr.match_id
			WHERE m.arena_match_id = ?
		`, match).Scan(&prev); err != nil {
			t.Fatalf("%s: %v", match, err)
		}
		if prev != want {
			t.Fatalf("%s previous rank snapshot = %v, want %v", match, prev, want)
		}
	}

	var untagged int
	if err := database.QueryRow(`
		SELECT (SELECT COUNT(*) FROM economy_snapshots WHERE persona_id IS NULL)
			+ (SELECT COUNT(*) FROM matches WHERE persona_id IS NULL)
			+ (SELECT COUNT(*) FROM match_rank_snapshots WHERE persona_id IS NULL)
	`).Scan(&untagged); err != nil {
		t.Fatal(err)
	}
	if untagged != 0 {
		t.Fatalf("%d rows left without an account", untagged)
	}

	// A's balance is unchanged across B's session, and B's first snapshot has
	// nothing of B's to reconcile against.
	inferred, err := store.ListInferredEconomyTransactions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(inferred) != 0 {
		t.Fatalf("inferred %d phantom transactions across the account switch: %+v", len(inferred), inferred)
	}

	for key, want := range map[string]string{
		"player_name":           "Alpha",
		"player_name:ACCOUNT_A": "Alpha",
		"player_name:ACCOUNT_B": "Beta",
	} {
		var got string
		if err := database.QueryRow(`SELECT value FROM app_metadata WHERE key = ?`, key).Scan(&got); err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		if got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
}

// Each log file is ingested in its own batches; the account carries over
// through the parser checkpoint when the next log starts.
func TestIngestTagsAccountAcrossBatchesAndResume(t *testing.T) {
	ctx := context.Background()
	database, store, logPath := checkpointDatabase(t)
	lines := arenaAccountsFixture()
	if err := writeLogLines(logPath, lines[:7], false); err != nil {
		t.Fatal(err)
	}
	if _, err := NewParser(store).ParseFile(ctx, logPath, true); err != nil {
		t.Fatal(err)
	}
	if err := writeLogLines(logPath, lines[7:], true); err != nil {
		t.Fatal(err)
	}
	if _, err := NewParser(store).ParseFile(ctx, logPath, true); err != nil {
		t.Fatal(err)
	}

	rows, err := database.Query(`SELECT arena_match_id, COALESCE(persona_id, '') FROM matches ORDER BY arena_match_id`)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for rows.Next() {
		var match, persona string
		if err := rows.Scan(&match, &persona); err != nil {
			t.Fatal(err)
		}
		got[match] = persona
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"match-a1": "ACCOUNT_A", "match-b1": "ACCOUNT_B", "match-a2": "ACCOUNT_A"}
	for match, persona := range want {
		if got[match] != persona {
			t.Fatalf("match personas = %v, want %v", got, want)
		}
	}
}
