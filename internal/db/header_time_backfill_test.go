package db

import (
	"context"
	"testing"
	"time"
)

func TestBackfillUTCLogHeaderTimesShiftsOnlyMisreadRows(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	database := openTempSQLiteDB(t)
	if err := Init(ctx, database); err != nil {
		t.Fatalf("Init: %v", err)
	}
	mst := time.FixedZone("MST", -7*60*60)

	if _, err := database.ExecContext(ctx, `
		DELETE FROM app_metadata WHERE key = 'utc_log_header_backfill_v1';
		INSERT INTO draft_sessions (id, draft_id, is_bot_draft, started_at, completed_at, created_at, updated_at) VALUES
			-- Before the switch: header times were local and are correct.
			(1, 'before', 0, '2026-10-20T03:06:00Z', '2026-10-20T03:22:39Z', '2026-10-20T03:06:25.38Z', '2026-10-20T03:22:49Z'),
			-- After it: 7h ahead of when Ponder ingested them.
			(2, 'after', 0, '2026-10-21T06:30:03Z', '2026-10-21T06:45:12Z', '2026-10-20T23:30:41.80Z', '2026-10-20T23:45:26Z');
		INSERT INTO draft_picks (draft_session_id, pack_number, pick_number, picked_card_ids, pick_ts, created_at) VALUES
			(1, 1, 1, '[]', '2026-10-20T03:06:00Z', '2026-10-20T03:06:25.38Z'),
			(2, 1, 1, '[]', '2026-10-21T06:30:03Z', '2026-10-20T23:30:41.80Z'),
			-- Ingested long after the fact: not provably misread, left alone.
			(2, 1, 2, '[]', '2026-10-21T06:31:00Z', '2026-10-22T01:00:00.00Z');
		INSERT INTO event_runs (id, event_name, started_at, ended_at, updated_at) VALUES
			(1, 'PremierDraft', '2026-10-20T03:05:58Z', '2026-10-21T06:28:54Z', '2026-10-22T04:14:10.31Z'),
			-- A GRE-clock time has fractional seconds and is never shifted.
			(2, 'Ladder', '2026-10-21T06:29:53.123Z', '2026-10-22T11:13:39Z', '2026-10-22T04:14:10.31Z');
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := backfillUTCLogHeaderTimes(ctx, database, mst); err != nil {
		t.Fatalf("backfillUTCLogHeaderTimes: %v", err)
	}

	checks := []struct{ query, want string }{
		{`SELECT started_at FROM draft_sessions WHERE id = 1`, "2026-10-20T03:06:00Z"},
		{`SELECT completed_at FROM draft_sessions WHERE id = 1`, "2026-10-20T03:22:39Z"},
		{`SELECT started_at FROM draft_sessions WHERE id = 2`, "2026-10-20T23:30:03Z"},
		{`SELECT completed_at FROM draft_sessions WHERE id = 2`, "2026-10-20T23:45:12Z"},
		{`SELECT pick_ts FROM draft_picks WHERE draft_session_id = 1`, "2026-10-20T03:06:00Z"},
		{`SELECT pick_ts FROM draft_picks WHERE draft_session_id = 2 AND pick_number = 1`, "2026-10-20T23:30:03Z"},
		{`SELECT pick_ts FROM draft_picks WHERE draft_session_id = 2 AND pick_number = 2`, "2026-10-21T06:31:00Z"},
		{`SELECT started_at FROM event_runs WHERE id = 1`, "2026-10-20T03:05:58Z"},
		{`SELECT ended_at FROM event_runs WHERE id = 1`, "2026-10-20T23:28:54Z"},
		{`SELECT started_at FROM event_runs WHERE id = 2`, "2026-10-21T06:29:53.123Z"},
		{`SELECT ended_at FROM event_runs WHERE id = 2`, "2026-10-22T04:13:39Z"},
	}
	for _, check := range checks {
		var got string
		if err := database.QueryRowContext(ctx, check.query).Scan(&got); err != nil {
			t.Fatalf("%s: %v", check.query, err)
		}
		if got != check.want {
			t.Errorf("%s = %q, want %q", check.query, got, check.want)
		}
	}

	// The marker makes a second run a no-op.
	if err := backfillUTCLogHeaderTimes(ctx, database, mst); err != nil {
		t.Fatalf("second backfillUTCLogHeaderTimes: %v", err)
	}
	var started string
	if err := database.QueryRowContext(ctx, `SELECT started_at FROM draft_sessions WHERE id = 2`).Scan(&started); err != nil || started != "2026-10-20T23:30:03Z" {
		t.Fatalf("after second run started_at = %q, %v", started, err)
	}
}
