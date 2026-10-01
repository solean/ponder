package ingest

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/solean/ponder/internal/db"
)

func checkpointFixture() []string {
	return []string{
		`{"clientId":"self-user","screenName":"Self"}`,
		`{"timestamp":"1772330782273","matchGameRoomStateChangedEvent":{"gameRoomInfo":{"gameRoomConfig":{"reservedPlayers":[{"userId":"opp-user","playerName":"Opp","systemSeatId":1,"teamId":1,"eventId":"Traditional_Ladder"},{"userId":"self-user","playerName":"Self","systemSeatId":2,"teamId":2,"eventId":"Traditional_Ladder"}],"matchId":"match-context"},"stateType":"MatchGameRoomStateType_Playing"}}}`,
		`{"timestamp":"1772330782309","greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","systemSeatIds":[2],"gameStateMessage":{"type":"GameStateType_Full","gameStateId":1,"gameInfo":{"matchID":"match-context","gameNumber":2},"turnInfo":{"phase":"Phase_Main1","turnNumber":3,"activePlayer":2},"players":[{"systemSeatNumber":1,"lifeTotal":18},{"systemSeatNumber":2,"lifeTotal":17}],"zones":[{"zoneId":28,"type":"ZoneType_Battlefield","visibility":"Visibility_Public","objectInstanceIds":[101]},{"zoneId":42,"type":"ZoneType_Hand","visibility":"Visibility_Private","ownerSeatId":1,"objectInstanceIds":[201]}],"gameObjects":[{"instanceId":101,"grpId":5001,"type":"GameObjectType_Card","zoneId":28,"visibility":"Visibility_Public","ownerSeatId":1,"controllerSeatId":1,"isTapped":true},{"instanceId":201,"grpId":6001,"type":"GameObjectType_Card","zoneId":42,"visibility":"Visibility_Private","ownerSeatId":1,"controllerSeatId":1}]}}]}}`,
		`{"timestamp":"1772330782310","greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","gameStateMessage":{"type":"GameStateType_Diff","gameStateId":2,"prevGameStateId":1,"zones":[{"zoneId":28,"objectInstanceIds":[101,201,102]},{"zoneId":42,"objectInstanceIds":[]}],"gameObjects":[{"instanceId":201,"type":"GameObjectType_Card","zoneId":28,"visibility":"Visibility_Public","hasSummoningSickness":true},{"instanceId":102,"grpId":5002,"type":"GameObjectType_Card","zoneId":28,"visibility":"Visibility_Public","ownerSeatId":1}]}}]}}`,
		`{"timestamp":"1772330782311","greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","gameStateMessage":{"type":"GameStateType_Diff","gameStateId":3,"prevGameStateId":2,"turnInfo":{"phase":"Phase_Main1","turnNumber":4,"activePlayer":2},"gameObjects":[{"instanceId":101,"type":"GameObjectType_Card","isTapped":false}]}}]}}`,
	}
}

func checkpointDatabase(t *testing.T) (*sql.DB, *db.Store, string) {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.Init(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	return database, db.NewStore(database), filepath.Join(dir, "Player.log")
}

// Compare semantic row fields, including complete frame/object contents, while
// excluding surrogate keys and ingestion wall-clock bookkeeping timestamps.
func checkpointRows(t *testing.T, database *sql.DB) map[string][][]any {
	t.Helper()
	out := make(map[string][][]any)
	for _, table := range []string{"match_card_plays", "match_opponent_card_instances", "match_replay_frames", "match_replay_frame_objects"} {
		order := "game_number, instance_id"
		if table == "match_replay_frames" {
			order = "game_number, game_state_id"
		} else if table == "match_replay_frame_objects" {
			order = "frame_id, instance_id"
		}
		rows, err := database.Query("SELECT * FROM " + table + " ORDER BY " + order)
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			values := make([]any, len(columns))
			refs := make([]any, len(columns))
			for i := range values {
				refs[i] = &values[i]
			}
			if err := rows.Scan(refs...); err != nil {
				t.Fatal(err)
			}
			var record []any
			for i, col := range columns {
				if col == "id" || col == "created_at" {
					continue
				}
				record = append(record, values[i])
			}
			out[table] = append(out[table], record)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
	}
	return out
}

func TestTailRestartMatchesUninterruptedReplay(t *testing.T) {
	ctx := context.Background()
	lines := checkpointFixture()
	uninterrupted, store, logPath := checkpointDatabase(t)
	if err := writeLogLines(logPath, lines, false); err != nil {
		t.Fatal(err)
	}
	if _, err := NewParser(store).ParseFile(ctx, logPath, true); err != nil {
		t.Fatal(err)
	}
	want := checkpointRows(t, uninterrupted)
	if len(want["match_replay_frames"]) != 3 || len(want["match_card_plays"]) != 3 {
		t.Fatalf("incomplete control observations: %#v", want)
	}

	restarted, store, logPath := checkpointDatabase(t)
	if err := writeLogLines(logPath, lines[:3], false); err != nil {
		t.Fatal(err)
	}
	if _, err := NewParser(store).ParseFile(ctx, logPath, true); err != nil {
		t.Fatal(err)
	}
	for _, line := range lines[3:] {
		if err := writeLogLines(logPath, []string{line}, true); err != nil {
			t.Fatal(err)
		}
		stats, err := NewParser(store).ParseFile(ctx, logPath, true)
		if err != nil {
			t.Fatal(err)
		}
		if stats.LinesRead != 1 {
			t.Fatalf("restart read %d lines, want only appended line", stats.LinesRead)
		}
		for range 2 {
			stats, err = NewParser(store).ParseFile(ctx, logPath, true)
			if err != nil {
				t.Fatal(err)
			}
			if stats.LinesRead != 0 {
				t.Fatalf("unchanged restart read %d lines", stats.LinesRead)
			}
		}
	}
	if got := checkpointRows(t, restarted); !reflect.DeepEqual(got, want) {
		t.Fatalf("restart observations differ\ngot: %#v\nwant: %#v", got, want)
	}
}

func TestTailFailedCheckpointRetriesDurableState(t *testing.T) {
	for _, batchCommitted := range []bool{false, true} {
		t.Run(fmt.Sprintf("prior_batch_%v", batchCommitted), func(t *testing.T) {
			ctx := context.Background()
			database, store, logPath := checkpointDatabase(t)
			lines := checkpointFixture()
			initial := append([]string(nil), lines[:3]...)
			if batchCommitted {
				for len(initial) < 500 {
					initial = append(initial, "")
				}
			}
			initial = append(initial, `{"clientId":"self-user","screenName":"FailedName"}`, lines[3])
			if err := writeLogLines(logPath, initial, false); err != nil {
				t.Fatal(err)
			}
			for _, action := range []string{"INSERT", "UPDATE"} {
				_, err := database.Exec(`CREATE TRIGGER fail_checkpoint_` + strings.ToLower(action) + ` BEFORE ` + action + ` ON app_metadata WHEN NEW.key='parser_checkpoint' AND NEW.value LIKE '%FailedName%' BEGIN SELECT RAISE(ABORT,'checkpoint failed'); END`)
				if err != nil {
					t.Fatal(err)
				}
			}
			parser := NewParser(store)
			if _, err := parser.ParseFile(ctx, logPath, true); err == nil {
				t.Fatal("expected failed checkpoint")
			}
			before, err := store.GetIngestState(ctx, logPath)
			if err != nil {
				t.Fatal(err)
			}
			if batchCommitted && before.LineNo != 500 {
				t.Fatalf("durable batch line = %d", before.LineNo)
			}
			if !batchCommitted && before.Found {
				t.Fatalf("failed first batch persisted state: %+v", before)
			}
			for _, action := range []string{"insert", "update"} {
				if _, err := database.Exec(`DROP TRIGGER fail_checkpoint_` + action); err != nil {
					t.Fatal(err)
				}
			}
			stats, err := parser.ParseFile(ctx, logPath, true)
			if err != nil {
				t.Fatal(err)
			}
			wantLines := int64(len(initial))
			if batchCommitted {
				wantLines = 2
			}
			if stats.LinesRead != wantLines {
				t.Fatalf("retry read %d lines, want %d", stats.LinesRead, wantLines)
			}
			name, err := store.PlayerName(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if name != "FailedName" {
				t.Fatalf("retried player name = %q", name)
			}
			got := checkpointRows(t, database)
			if len(got["match_card_plays"]) != 3 || len(got["match_replay_frames"]) != 2 {
				t.Fatalf("retry lost/duplicated observations: %#v", got)
			}
			contextState, err := store.GetIngestState(ctx, logPath)
			if err != nil {
				t.Fatal(err)
			}
			restored, ok := decodeParseCheckpoint(contextState.ContextJSON)
			if !ok || restored.activeMatchID != "match-context" || restored.gameNumber("match-context") != 2 {
				t.Fatalf("inconsistent retried context: %+v", restored)
			}
		})
	}
}

func TestLegacyCheckpointReconstructsWithoutDuplicatingPrefix(t *testing.T) {
	ctx := context.Background()
	database, store, logPath := checkpointDatabase(t)
	lines := checkpointFixture()
	// Start with diff-only history to catch hydration from a newer stored frame.
	lines[2] = strings.Replace(lines[2], "GameStateType_Full", "GameStateType_Diff", 1)
	raw := `[UnityCrossThreadLogger]==> EventSetDeckV3 {"id":"deck-request","request":{"EventName":"Traditional_Ladder","Summary":{"DeckId":"deck-1","Name":"Deck"},"Deck":{"MainDeck":[{"cardId":6001,"quantity":4}]}}}`
	initial := append([]string{raw}, lines[:3]...)
	if err := writeLogLines(logPath, initial, false); err != nil {
		t.Fatal(err)
	}
	if _, err := NewParser(store).ParseFile(ctx, logPath, true); err != nil {
		t.Fatal(err)
	}
	var rawBefore int
	if err := database.QueryRow(`SELECT count(*) FROM events_raw`).Scan(&rawBefore); err != nil {
		t.Fatal(err)
	}
	// Simulate the schema that saved only byte/line cursor, with no context.
	if _, err := database.Exec(`UPDATE ingest_state SET context_json=''; DELETE FROM app_metadata WHERE key='parser_checkpoint'`); err != nil {
		t.Fatal(err)
	}
	if err := writeLogLines(logPath, lines[3:], true); err != nil {
		t.Fatal(err)
	}
	stats, err := NewParser(store).ParseFile(ctx, logPath, true)
	if err != nil {
		t.Fatal(err)
	}
	if stats.LinesRead != 2 {
		t.Fatalf("stats included reconstructed prefix: %+v", stats)
	}
	var rawAfter int
	if err := database.QueryRow(`SELECT count(*) FROM events_raw`).Scan(&rawAfter); err != nil {
		t.Fatal(err)
	}
	if rawAfter != rawBefore || rawBefore != 1 {
		t.Fatalf("raw records before/after = %d/%d, want 1/1", rawBefore, rawAfter)
	}
	got := checkpointRows(t, database)
	if len(got["match_card_plays"]) != 3 || len(got["match_replay_frames"]) != 3 {
		t.Fatalf("legacy restart lost observations: %#v", got)
	}
	saved, err := store.GetIngestState(ctx, logPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := decodeParseCheckpoint(saved.ContextJSON); !ok {
		t.Fatal("legacy checkpoint not upgraded")
	}
}

func TestHistoricalReparseDoesNotApplyOldRankToPendingMatch(t *testing.T) {
	for _, backfill := range []bool{false, true} {
		t.Run(fmt.Sprintf("zero_cursor_backfill_%v", backfill), func(t *testing.T) {
			ctx := context.Background()
			database, store, logPath := checkpointDatabase(t)
			completed := func(id string) string {
				return fmt.Sprintf(`{"timestamp":"1773367612385","matchGameRoomStateChangedEvent":{"gameRoomInfo":{"gameRoomConfig":{"matchId":%q,"reservedPlayers":[{"userId":"opp","systemSeatId":1,"teamId":1},{"userId":"self","systemSeatId":2,"teamId":2}]},"stateType":"MatchGameRoomStateType_MatchCompleted","finalMatchResult":{"matchId":%q,"resultList":[{"scope":"MatchScope_Match","winningTeamId":1}]}}}}`, id, id)
			}
			lines := []string{`{"PersonaId":"self"}`, completed("rank-a"), `<== RankGetCombinedRankInfo(old-rank)`, `{"constructedLevel":3}`, completed("rank-b")}
			if err := writeLogLines(logPath, lines, false); err != nil {
				t.Fatal(err)
			}
			if _, err := NewParser(store).ParseFile(ctx, logPath, true); err != nil {
				t.Fatal(err)
			}
			if backfill {
				if _, err := database.Exec(`UPDATE ingest_state SET byte_offset=0,line_no=0`); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := NewParser(store).ParseFile(ctx, logPath, backfill); err != nil {
				t.Fatal(err)
			}
			var snapshots int
			if err := database.QueryRow(`SELECT count(*) FROM match_rank_snapshots r JOIN matches m ON m.id=r.match_id WHERE m.arena_match_id='rank-b'`).Scan(&snapshots); err != nil {
				t.Fatal(err)
			}
			if snapshots != 0 {
				t.Fatal("old rank response was attached to newer pending match B during historical replay")
			}
			if err := writeLogLines(logPath, []string{`<== RankGetCombinedRankInfo(new-rank)`, `{"constructedLevel":9}`}, true); err != nil {
				t.Fatal(err)
			}
			if _, err := NewParser(store).ParseFile(ctx, logPath, true); err != nil {
				t.Fatal(err)
			}
			var level int
			if err := database.QueryRow(`SELECT constructed_level FROM match_rank_snapshots r JOIN matches m ON m.id=r.match_id WHERE m.arena_match_id='rank-b'`).Scan(&level); err != nil {
				t.Fatal(err)
			}
			if level != 9 {
				t.Fatalf("pending match got rank %d, want own later rank 9", level)
			}
		})
	}
}

func TestEmptyCurrentLogRetainsCrossFileRankQueueAfterRestart(t *testing.T) {
	ctx := context.Background()
	database, store, current := checkpointDatabase(t)
	previous := filepath.Join(filepath.Dir(current), "Player-prev.log")
	lines := []string{
		`{"PersonaId":"self"}`,
		`{"timestamp":"1773367612385","matchGameRoomStateChangedEvent":{"gameRoomInfo":{"gameRoomConfig":{"matchId":"empty-current-rank","reservedPlayers":[{"userId":"opp","systemSeatId":1,"teamId":1},{"userId":"self","systemSeatId":2,"teamId":2}]},"stateType":"MatchGameRoomStateType_MatchCompleted","finalMatchResult":{"matchId":"empty-current-rank","resultList":[{"scope":"MatchScope_Match","winningTeamId":1}]}}}}`,
	}
	if err := writeLogLines(previous, lines, false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(current, nil, 0600); err != nil {
		t.Fatal(err)
	}
	parser := NewParser(store)
	if _, err := parser.ParseFile(ctx, previous, true); err != nil {
		t.Fatal(err)
	}
	if _, err := parser.ParseFile(ctx, current, true); err != nil {
		t.Fatal(err)
	}
	empty, err := store.GetIngestState(ctx, current)
	if err != nil {
		t.Fatal(err)
	}
	if !empty.Found || empty.Offset != 0 || empty.LineNo != 0 || empty.FileSignature != "" {
		t.Fatalf("empty-log checkpoint = %+v", empty)
	}
	if err := writeLogLines(current, []string{`<== RankGetCombinedRankInfo(rank)`, `{"constructedLevel":9}`}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := NewParser(store).ParseFile(ctx, current, true); err != nil {
		t.Fatal(err)
	}
	var level int
	if err := database.QueryRow(`SELECT constructed_level FROM match_rank_snapshots`).Scan(&level); err != nil {
		t.Fatal(err)
	}
	if level != 9 {
		t.Fatalf("rank after empty-log restart = %d", level)
	}
}

func TestLegacyPrefixDoesNotConsumeDurableCrossFileRankQueue(t *testing.T) {
	ctx := context.Background()
	_, store, logPath := checkpointDatabase(t)
	initial := append(checkpointFixture()[:3], `<== RankGetCombinedRankInfo(old-response)`, `{"constructedLevel":3}`)
	if err := writeLogLines(logPath, initial, false); err != nil {
		t.Fatal(err)
	}
	if _, err := NewParser(store).ParseFile(ctx, logPath, true); err != nil {
		t.Fatal(err)
	}
	progress, err := store.GetIngestState(ctx, logPath)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	global := `{"Version":1,"PersonaID":"self-user","PlayerName":"Self","PendingCompletedMatches":["another-file-pending"]}`
	if err := store.SaveIngestCheckpoint(ctx, tx, logPath, progress.Offset, progress.LineNo, progress.FileSignature, "", global); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := writeLogLines(logPath, checkpointFixture()[3:], true); err != nil {
		t.Fatal(err)
	}
	parser := NewParser(store)
	if _, err := parser.ParseFile(ctx, logPath, true); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(parser.pendingCompletedMatches, []string{"another-file-pending"}) {
		t.Fatalf("legacy prefix changed durable queue: %v", parser.pendingCompletedMatches)
	}
}
