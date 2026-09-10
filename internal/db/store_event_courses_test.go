package db

import (
	"context"
	"testing"
)

func TestEventRunReplayUsesDatedClaimedRun(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database, store := openEconomyTestDB(t)
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	const event = "PremierDraft_HOB_20260811"
	if err := store.UpsertEventRunJoin(ctx, tx, event, "Gem", 1500, "2026-09-09T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertMatchStart(ctx, tx, "replayed-match", event, 1, "2026-09-09T00:02:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkEventRunClaimed(ctx, tx, event, "2026-09-09T00:05:00Z"); err != nil {
		t.Fatal(err)
	}
	// A nearby active run must not win over the exact historical join.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO event_runs (event_name, started_at, updated_at)
		VALUES (?, '2026-09-09T00:10:00Z', '2026-09-09T00:10:00Z'),
		       (?, NULL, '2026-09-10T00:00:00Z')
	`, event, event); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := store.UpsertEventRunJoin(ctx, tx, event, "Gem", 1500, "2026-09-09T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
		if _, err := store.UpsertMatchStart(ctx, tx, "replayed-match", event, 1, "2026-09-09T00:02:00Z"); err != nil {
			t.Fatal(err)
		}
		if err := store.MarkEventRunClaimed(ctx, tx, event, "2026-09-09T00:05:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var runs, paid, claimed int64
	if err := database.QueryRowContext(ctx, `
		SELECT COUNT(*), SUM(CASE WHEN entry_currency_paid = 1500 THEN 1 ELSE 0 END),
			SUM(CASE WHEN status = 'claimed' THEN 1 ELSE 0 END) FROM event_runs WHERE event_name = ?
	`, event).Scan(&runs, &paid, &claimed); err != nil {
		t.Fatal(err)
	}
	if runs != 3 || paid != 1 || claimed != 1 {
		t.Fatalf("replay created or claimed another run: runs=%d paid=%d claimed=%d", runs, paid, claimed)
	}
	runEconomies, err := store.ListEventRunEconomies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(runEconomies) != 1 || runEconomies[0].StartedAt != "2026-09-09T00:00:00Z" || runEconomies[0].EntryGems != -1500 {
		t.Fatalf("historical payment moved on replay: %+v", runEconomies)
	}
}

func TestRepairEventCoursesMergesReplayEvidencePreservingHistory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database, store := openEconomyTestDB(t)
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	const event = "PremierDraft_HOB_20260811"
	if err := store.UpsertEventRunJoin(ctx, tx, event, "Gem", 1500, "2026-09-09T08:00:00Z"); err != nil {
		t.Fatal(err)
	}
	sessionID, err := store.EnsureDraftSession(ctx, tx, event, ptrString("real-draft"), false, "2026-09-09T08:00:05Z")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertMatchStart(ctx, tx, "phantom-origin", event, 1, "2026-09-09T08:30:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertEventCourse(ctx, tx, EventCourseRecord{CourseID: "actual-course", EventName: event, CurrentModule: "Complete", Wins: 4, Losses: 3}, "2026-09-09T10:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE event_runs SET ended_at = '2026-09-09T07:00:00Z' WHERE draft_session_id = ?
	`, sessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO event_runs (event_name, entry_currency_type, entry_currency_paid, pay_source_id, started_at, updated_at)
		VALUES (?, 'Gem', 1500, NULL, '2026-09-09T08:30:00Z', '2026-09-09T10:00:00Z'),
		       (?, 'Gem', 1500, 'missing-draft-course', '2026-09-09T09:30:00Z', '2026-09-09T10:00:00Z'),
		       (?, 'Gold', 5000, NULL, NULL, '2026-09-10T00:00:00Z'),
		       (?, 'Gem', 1500, NULL, '2026-09-08T07:00:00Z', '2026-09-10T00:00:00Z')
	`, event, event, event, event); err != nil {
		t.Fatal(err)
	}
	snapshotID, _, err := store.InsertEconomySnapshot(ctx, tx, "test.log", 1, EconomySnapshotRecord{ObservedAt: "2026-09-09T08:30:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO economy_transactions (snapshot_id, change_index, observed_at, source, source_id, event_name, event_run_id, event_link, gems_delta, created_at)
		SELECT ?, 0, '2026-09-09T08:30:00Z', 'EventGrantCardPool', ?, ?, id, 'event_name', 20, '2026-09-09T10:00:00Z'
		FROM event_runs WHERE started_at = '2026-09-09T08:30:00Z'
	`, snapshotID, event, event); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := store.RepairEventRunInstances(ctx); err != nil {
			t.Fatal(err)
		}
	}
	runs, err := store.ListEventRunEconomies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 4 {
		t.Fatalf("repair lost history or retained phantom: %+v", runs)
	}
	if runs[len(runs)-1].StartedAt != "" {
		t.Fatalf("undated history sorted ahead of dated runs: %+v", runs)
	}
	var wins, losses, reward int64
	var endedAt string
	if err := database.QueryRowContext(ctx, `
		SELECT er.wins, er.losses, er.ended_at, SUM(et.gems_delta)
		FROM event_runs er JOIN economy_transactions et ON et.event_run_id = er.id
		WHERE er.draft_session_id = ? GROUP BY er.id
	`, sessionID).Scan(&wins, &losses, &endedAt, &reward); err != nil {
		t.Fatal(err)
	}
	if wins != 4 || losses != 3 || endedAt != "2026-09-09T10:00:00Z" || reward != 20 {
		t.Fatalf("course/ledger evidence lost: %d-%d end=%q reward=%d", wins, losses, endedAt, reward)
	}
}

func TestCourseRecordsOverrideIncompleteReusedDeckResults(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, store := openEconomyTestDB(t)
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	const event = "PremierDraft_HOB_20260811"
	for _, run := range []struct {
		course, draft, start, end string
		wins                      int64
	}{
		{"course-one", "draft-one", "2026-09-09T10:00:00Z", "2026-09-09T11:00:00Z", 4},
		{"course-two", "draft-two", "2026-09-09T12:00:00Z", "2026-09-09T13:00:00Z", 1},
	} {
		if err := store.UpsertEventRunJoin(ctx, tx, event, "Gem", 1500, run.start); err != nil {
			t.Fatal(err)
		}
		if _, err := store.EnsureDraftSession(ctx, tx, event, ptrString(run.draft), false, run.start); err != nil {
			t.Fatal(err)
		}
		if err := store.UpsertEventCourse(ctx, tx, EventCourseRecord{CourseID: run.course, EventName: event, DeckID: "reused-deck", CurrentModule: "Complete", Wins: run.wins, Losses: 3}, run.end); err != nil {
			t.Fatal(err)
		}
		// The old enrollment response arrives after the terminal response on replay.
		if err := store.UpsertEventCourse(ctx, tx, EventCourseRecord{CourseID: run.course, EventName: event, CurrentModule: "Draft"}, run.start); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.UpsertDeck(ctx, tx, "reused-deck", event, "Draft Deck", "Draft", "test", "2026-09-09T12:10:00Z", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertMatchStart(ctx, tx, "incomplete-match", event, 1, "2026-09-09T12:20:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := store.UpdateMatchEnd(ctx, tx, "incomplete-match", 1, 1, 0, 0, "", "2026-09-09T12:30:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := store.LinkMatchToLatestDeckByEvent(ctx, tx, "incomplete-match", event, "test"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := store.RepairEventRunInstances(ctx); err != nil {
		t.Fatal(err)
	}
	sessions, err := store.ListDraftSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("sessions = %+v", sessions)
	}
	for index, wins := range []int64{1, 4} {
		row := sessions[index]
		if row.Wins == nil || row.Losses == nil || *row.Wins != wins || *row.Losses != 3 {
			t.Fatalf("draft %d ignored exact course record: %+v", index, row)
		}
		if row.Economy == nil || row.Economy.Wins != wins || row.Economy.Losses != 3 || row.Economy.RewardGems != 0 {
			t.Fatalf("economy %d lost course record or invented prizes: %+v", index, row.Economy)
		}
	}
}
