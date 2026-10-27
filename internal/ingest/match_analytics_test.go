package ingest

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

func TestCompletedMatchAnalyticsRefreshDuringIngest(t *testing.T) {
	completions := map[string]string{
		"room state":     checkpointCompletedMatch("match-context", "self-user"),
		"business event": `[UnityCrossThreadLogger]==> LogBusinessEvents {"id":"end","request":{"EventType":4,"MatchId":"match-context","TeamId":2,"WinningTeamId":1,"EventTime":"2026-03-12T19:26:52Z"}}`,
	}
	for name, completion := range completions {
		for _, resume := range []bool{false, true} {
			mode := "import"
			if resume {
				mode = "live"
			}
			t.Run(name+"/"+mode, func(t *testing.T) {
				// A single connection catches analytics accidentally run inside
				// the ingest transaction, or a nested writer acquisition.
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				database, store, logPath := checkpointDatabase(t)
				database.SetMaxOpenConns(1)
				parser := NewParser(store)
				lines := checkpointFixture()
				for i := range lines {
					lines[i] = strings.ReplaceAll(lines[i], `"gameNumber":2`, `"gameNumber":1`)
				}
				if err := writeLogLines(logPath, lines, false); err != nil {
					t.Fatal(err)
				}
				if _, err := parser.ParseFile(ctx, logPath, resume); err != nil {
					t.Fatal(err)
				}
				assertTableCount(t, database, "match_analytics_coverage", 0)
				if err := writeLogLines(logPath, []string{completion}, true); err != nil {
					t.Fatal(err)
				}
				if _, err := parser.ParseFile(ctx, logPath, resume); err != nil {
					t.Fatal(err)
				}

				// Read persisted facts directly, with no maintenance or lazy
				// EnsureMatchAnalytics call to hide missing derivation.
				assertTableCount(t, database, "match_replay_archives", 1)
				assertTableCount(t, database, "match_replay_frames", 0)
				assertTableCount(t, database, "games", 1)
				assertTableCount(t, database, "match_opponent_card_counts", 3)
				coverage, err := store.GetMatchAnalyticsCoverage(ctx, 1)
				if err != nil {
					t.Fatal(err)
				}
				if !coverage.ReplayAvailable || coverage.ReplayFrameCount != 3 || coverage.GameCount != 1 || coverage.GamesWithResult != 1 || coverage.GamesWithTurnStats != 1 {
					t.Fatalf("incomplete persisted coverage: %+v", coverage)
				}
				var result string
				var turns int
				if err := database.QueryRow(`SELECT result, turn_count FROM games`).Scan(&result, &turns); err != nil {
					t.Fatal(err)
				}
				if result != "loss" || turns != 4 {
					t.Fatalf("derived game = %s, %d turns; want loss, 4 turns", result, turns)
				}
				assertTableCount(t, database, "game_turn_stats", 2)

				// A repeated completion still refreshes an existing archive,
				// without duplicating derived game or opponent rows.
				if err := writeLogLines(logPath, []string{completion}, true); err != nil {
					t.Fatal(err)
				}
				if _, err := parser.ParseFile(ctx, logPath, resume); err != nil {
					t.Fatal(err)
				}
				assertTableCount(t, database, "games", 1)
				assertTableCount(t, database, "match_opponent_card_counts", 3)
			})
		}
	}
}

func TestCompletedMatchAnalyticsAtBatchCommit(t *testing.T) {
	ctx := context.Background()
	database, store, logPath := checkpointDatabase(t)
	lines := append(checkpointFixture(), checkpointCompletedMatch("match-context", "self-user"))
	for len(lines) < 500 {
		lines = append(lines, "")
	}
	// Fail a later batch to verify analytics were refreshed at the first
	// batch commit, rather than waiting until the entire file is parsed.
	lines = append(lines, `{"clientId":"self-user","screenName":"FailedName"}`)
	if _, err := database.Exec(`CREATE TRIGGER fail_later_batch BEFORE UPDATE ON app_metadata
		WHEN NEW.key='parser_checkpoint' AND NEW.value LIKE '%FailedName%'
		BEGIN SELECT RAISE(ABORT,'checkpoint failed'); END`); err != nil {
		t.Fatal(err)
	}
	if err := writeLogLines(logPath, lines, false); err != nil {
		t.Fatal(err)
	}
	if _, err := NewParser(store).ParseFile(ctx, logPath, true); err == nil || !strings.Contains(err.Error(), "checkpoint failed") {
		t.Fatalf("expected later checkpoint failure, got %v", err)
	}
	assertTableCount(t, database, "match_replay_archives", 1)
	assertTableCount(t, database, "match_analytics_coverage", 1)
	assertTableCount(t, database, "games", 1)
}

func TestCompletedMatchAnalyticsIgnoreRolledBackBatch(t *testing.T) {
	ctx := context.Background()
	database, store, logPath := checkpointDatabase(t)
	lines := append(checkpointFixture(), checkpointCompletedMatch("match-context", "self-user"))
	if _, err := database.Exec(`CREATE TRIGGER fail_checkpoint BEFORE INSERT ON app_metadata
		WHEN NEW.key='parser_checkpoint'
		BEGIN SELECT RAISE(ABORT,'checkpoint failed'); END`); err != nil {
		t.Fatal(err)
	}
	if err := writeLogLines(logPath, lines, false); err != nil {
		t.Fatal(err)
	}
	parser := NewParser(store)
	if _, err := parser.ParseFile(ctx, logPath, true); err == nil {
		t.Fatal("expected checkpoint failure")
	}
	assertTableCount(t, database, "matches", 0)
	assertTableCount(t, database, "match_analytics_coverage", 0)
	if _, err := database.Exec(`DROP TRIGGER fail_checkpoint`); err != nil {
		t.Fatal(err)
	}
	if _, err := parser.ParseFile(ctx, logPath, true); err != nil {
		t.Fatal(err)
	}
	assertTableCount(t, database, "match_analytics_coverage", 1)
	assertTableCount(t, database, "games", 1)
}

func assertTableCount(t *testing.T, database *sql.DB, table string, want int) {
	t.Helper()
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("%s row count = %d, want %d", table, count, want)
	}
}
