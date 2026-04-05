package db

import (
	"context"
	"testing"
)

func TestListDraftSessionsRepairsPlayerDraftMetadataFromRawEvents(t *testing.T) {
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
	if err := store.InsertRawEvent(ctx, tx, "Player.log", 10, 100, "outgoing", "LogBusinessEvents", "req-1", []byte(rawEvent), ""); err != nil {
		t.Fatalf("InsertRawEvent: %v", err)
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
	if row.StartedAt != "2026-04-04T00:33:13.720644Z" {
		t.Fatalf("StartedAt = %q, want 2026-04-04T00:33:13.720644Z", row.StartedAt)
	}
	if row.CompletedAt != "2026-04-04T00:33:13.720644Z" {
		t.Fatalf("CompletedAt = %q, want 2026-04-04T00:33:13.720644Z", row.CompletedAt)
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

func TestListDraftSessionsRepairsPlayerDraftFromPickAndCompleteEvents(t *testing.T) {
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
	if err := store.InsertRawEvent(ctx, tx, "Player.log", 20, 200, "outgoing", "EventPlayerDraftMakePick", "req-pick", []byte(rawPick), ""); err != nil {
		t.Fatalf("InsertRawEvent(pick): %v", err)
	}

	rawComplete := `{"EventName":"PremierDraft_TMT_20260303","IsBotDraft":false}`
	if err := store.InsertRawEvent(ctx, tx, "Player.log", 21, 220, "outgoing", "DraftCompleteDraft", "req-complete", []byte(rawComplete), ""); err != nil {
		t.Fatalf("InsertRawEvent(complete): %v", err)
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

func ptrString(value string) *string {
	return &value
}
