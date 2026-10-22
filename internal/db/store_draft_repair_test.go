package db

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
)

func TestRepairDraftDataBackfillsPlayerDraftMetadataFromRawEvents(t *testing.T) {
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

	sessionID, err := store.EnsureDraftSession(ctx, tx, "", ptrString("draft-123"), false, "")
	if err != nil {
		t.Fatalf("EnsureDraftSession: %v", err)
	}
	if err := store.InsertDraftPick(ctx, tx, sessionID, 1, 1, []int64{1001}, nil, ""); err != nil {
		t.Fatalf("InsertDraftPick: %v", err)
	}

	rawEvent := `{"DraftId":"draft-123","EventId":"PremierDraft_TMT_20260303","PackNumber":1,"PickNumber":1,"PickGrpId":1001,"CardsInPack":[1001,1002,1003],"EventType":24,"EventTime":"2026-04-04T00:33:13.720644Z"}`
	if stored, err := store.InsertRawEvent(ctx, tx, "Player.log", 10, 100, "outgoing", "LogBusinessEvents", "req-1", []byte(rawEvent), ""); err != nil {
		t.Fatalf("InsertRawEvent: %v", err)
	} else if !stored {
		t.Fatal("InsertRawEvent skipped a draft pick business event")
	}
	// A log backfill can encounter older unparsed request payloads. They must
	// not make SQLite's json_extract abort the entire repair pass.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO events_raw (
			log_path, line_no, byte_offset, kind, method_name,
			request_id, payload_json, raw_text, created_at
		) VALUES (
			'Player.log', 11, 200, 'outgoing', 'LogBusinessEvents',
			'req-bad', 'not-json', '', '2026-04-04T00:33:14Z'
		)
	`); err != nil {
		t.Fatalf("insert malformed legacy event: %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if err := store.RepairDraftDataFromRawEvents(ctx); err != nil {
		t.Fatalf("RepairDraftDataFromRawEvents: %v", err)
	}

	rows, err := store.ListDraftSessions(ctx)
	if err != nil {
		t.Fatalf("ListDraftSessions: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(ListDraftSessions) = %d, want 1", len(rows))
	}

	row := rows[0]
	if row.EventName != "PremierDraft_TMT_20260303" {
		t.Fatalf("EventName = %q, want PremierDraft_TMT_20260303", row.EventName)
	}
	if row.StartedAt != "2026-04-04T00:33:13.720644Z" {
		t.Fatalf("StartedAt = %q, want 2026-04-04T00:33:13.720644Z", row.StartedAt)
	}
	// One pick and no DraftCompleteDraft: the draft is still in progress.
	if row.CompletedAt != "" {
		t.Fatalf("CompletedAt = %q, want empty mid-draft", row.CompletedAt)
	}

	picks, err := store.ListDraftPicks(ctx, sessionID)
	if err != nil {
		t.Fatalf("ListDraftPicks: %v", err)
	}
	if len(picks) != 1 {
		t.Fatalf("len(ListDraftPicks) = %d, want 1", len(picks))
	}
	if picks[0].PickTs != "2026-04-04T00:33:13.720644Z" {
		t.Fatalf("PickTs = %q, want 2026-04-04T00:33:13.720644Z", picks[0].PickTs)
	}
	if picks[0].PackCardIDs != "[1001,1002,1003]" {
		t.Fatalf("PackCardIDs = %q, want [1001,1002,1003]", picks[0].PackCardIDs)
	}
}

func TestCompleteDraftSessionBackfillsLatestIncompletePlayerDraft(t *testing.T) {
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

	if _, err := store.EnsureDraftSession(ctx, tx, "", ptrString("draft-456"), false, "2026-04-04T00:33:13.720644Z"); err != nil {
		t.Fatalf("EnsureDraftSession: %v", err)
	}
	if err := store.CompleteDraftSession(ctx, tx, "PremierDraft_TMT_20260303", nil, false, "2026-04-04T00:46:25.744975Z"); err != nil {
		t.Fatalf("CompleteDraftSession: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	rows, err := store.ListDraftSessions(ctx)
	if err != nil {
		t.Fatalf("ListDraftSessions: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(ListDraftSessions) = %d, want 1", len(rows))
	}

	row := rows[0]
	if row.EventName != "PremierDraft_TMT_20260303" {
		t.Fatalf("EventName = %q, want PremierDraft_TMT_20260303", row.EventName)
	}
	if row.CompletedAt != "2026-04-04T00:46:25.744975Z" {
		t.Fatalf("CompletedAt = %q, want 2026-04-04T00:46:25.744975Z", row.CompletedAt)
	}
}

func TestRepairDraftDataBackfillsPlayerDraftFromPickAndCompleteEvents(t *testing.T) {
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

	sessionID, err := store.EnsureDraftSession(ctx, tx, "", ptrString("draft-789"), false, "")
	if err != nil {
		t.Fatalf("EnsureDraftSession: %v", err)
	}
	if err := store.InsertDraftPick(ctx, tx, sessionID, 3, 14, []int64{100508}, nil, ""); err != nil {
		t.Fatalf("InsertDraftPick: %v", err)
	}

	rawPick := `{"DraftId":"draft-789","GrpIds":[100508],"Pack":3,"Pick":14}`
	if stored, err := store.InsertRawEvent(ctx, tx, "Player.log", 20, 200, "outgoing", "EventPlayerDraftMakePick", "req-pick", []byte(rawPick), ""); err != nil {
		t.Fatalf("InsertRawEvent(pick): %v", err)
	} else if !stored {
		t.Fatal("InsertRawEvent skipped a player draft pick event")
	}

	rawComplete := `{"EventName":"PremierDraft_TMT_20260303","IsBotDraft":false}`
	if stored, err := store.InsertRawEvent(ctx, tx, "Player.log", 21, 220, "outgoing", "DraftCompleteDraft", "req-complete", []byte(rawComplete), ""); err != nil {
		t.Fatalf("InsertRawEvent(complete): %v", err)
	} else if !stored {
		t.Fatal("InsertRawEvent skipped a draft complete event")
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if err := store.RepairDraftDataFromRawEvents(ctx); err != nil {
		t.Fatalf("RepairDraftDataFromRawEvents: %v", err)
	}

	rows, err := store.ListDraftSessions(ctx)
	if err != nil {
		t.Fatalf("ListDraftSessions: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(ListDraftSessions) = %d, want 1", len(rows))
	}

	row := rows[0]
	if row.EventName != "PremierDraft_TMT_20260303" {
		t.Fatalf("EventName = %q, want PremierDraft_TMT_20260303", row.EventName)
	}
	if row.StartedAt == "" {
		t.Fatalf("StartedAt = %q, want non-empty fallback timestamp", row.StartedAt)
	}
	if row.CompletedAt == "" {
		t.Fatalf("CompletedAt = %q, want non-empty fallback timestamp", row.CompletedAt)
	}

	picks, err := store.ListDraftPicks(ctx, sessionID)
	if err != nil {
		t.Fatalf("ListDraftPicks: %v", err)
	}
	if len(picks) != 1 {
		t.Fatalf("len(ListDraftPicks) = %d, want 1", len(picks))
	}
	if picks[0].PickTs == "" {
		t.Fatalf("PickTs = %q, want non-empty fallback timestamp", picks[0].PickTs)
	}
}

func TestRepairDraftDataCompletesPlayerDraftOnlyAfterCompleteDraft(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	database := openTempSQLiteDB(t)
	if err := Init(ctx, database); err != nil {
		t.Fatalf("Init: %v", err)
	}
	store := NewStore(database)

	// Each batch mirrors one live ingest pass: store the pick, then repair.
	ingestPick := func(draftID string, pack, pick int64, ts string, line int64) int64 {
		t.Helper()
		tx, err := store.BeginTx(ctx)
		if err != nil {
			t.Fatalf("BeginTx: %v", err)
		}
		sessionID, err := store.EnsureDraftSession(ctx, tx, "", ptrString(draftID), false, ts)
		if err != nil {
			t.Fatalf("EnsureDraftSession: %v", err)
		}
		if err := store.InsertDraftPick(ctx, tx, sessionID, pack, pick, []int64{1000 + pick}, nil, ts); err != nil {
			t.Fatalf("InsertDraftPick: %v", err)
		}
		raw := fmt.Sprintf(`{"DraftId":%q,"GrpIds":[%d],"Pack":%d,"Pick":%d}`, draftID, 1000+pick, pack, pick)
		if _, err := store.InsertRawEvent(ctx, tx, "Player.log", line, line*10, "outgoing", "EventPlayerDraftMakePick", "", []byte(raw), ""); err != nil {
			t.Fatalf("InsertRawEvent(pick): %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("Commit: %v", err)
		}
		if err := store.RepairDraftDataFromRawEvents(ctx); err != nil {
			t.Fatalf("RepairDraftDataFromRawEvents: %v", err)
		}
		return sessionID
	}
	completedAt := func(sessionID int64) string {
		t.Helper()
		var v sql.NullString
		if err := database.QueryRowContext(ctx, `SELECT completed_at FROM draft_sessions WHERE id = ?`, sessionID).Scan(&v); err != nil {
			t.Fatalf("read completed_at: %v", err)
		}
		return v.String
	}

	// An abandoned draft, then a second draft that runs to completion.
	abandoned := ingestPick("draft-a", 1, 1, "2026-10-19T03:00:00Z", 1)
	sessionID := ingestPick("draft-b", 1, 1, "2026-10-19T03:56:14Z", 2)
	ingestPick("draft-b", 1, 2, "2026-10-19T03:57:00Z", 3)
	if got := completedAt(sessionID); got != "" {
		t.Fatalf("completed_at mid-draft = %q, want empty", got)
	}
	ingestPick("draft-b", 3, 14, "2026-10-19T04:12:06Z", 4)

	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	rawComplete := `{"EventName":"PremierDraft_FRA_20260929","IsBotDraft":false}`
	if _, err := store.InsertRawEvent(ctx, tx, "Player.log", 5, 50, "outgoing", "DraftCompleteDraft", "", []byte(rawComplete), ""); err != nil {
		t.Fatalf("InsertRawEvent(complete): %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := store.RepairDraftDataFromRawEvents(ctx); err != nil {
		t.Fatalf("RepairDraftDataFromRawEvents: %v", err)
	}

	if got := completedAt(sessionID); got != "2026-10-19T04:12:06Z" {
		t.Fatalf("completed_at = %q, want last pick 2026-10-19T04:12:06Z", got)
	}
	if got := completedAt(abandoned); got != "" {
		t.Fatalf("abandoned completed_at = %q, want empty", got)
	}
}

func ptrString(value string) *string {
	return &value
}
