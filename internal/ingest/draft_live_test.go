package ingest

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/solean/ponder/internal/db"
)

func TestLiveDraftAppendDoesNotRepairHistoricalRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.Init(ctx, database); err != nil {
		t.Fatal(err)
	}
	store := db.NewStore(database)
	parser := NewParser(store)
	logPath := filepath.Join(dir, "Player.log")
	if err := writeLogLines(logPath, []string{
		`[UnityCrossThreadLogger]9/24/2026 7:40:40 PM`,
		`[UnityCrossThreadLogger]Draft.Notify {"draftId":"active","SelfPick":1,"SelfPack":1,"PackCards":"1001,1002"}`,
	}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := parser.ParseFile(ctx, logPath, true); err != nil {
		t.Fatal(err)
	}

	// Legacy missing pack data and raw repair evidence must not make every live
	// append revisit history. The explicit import below proves the sentinel is
	// repairable and still gets repaired through the historical path.
	if _, err := database.ExecContext(ctx, `
 INSERT INTO draft_sessions (id,draft_id,is_bot_draft,event_name,started_at,completed_at,created_at,updated_at)
 VALUES (100,'legacy',0,'PremierDraft_OLD','2020-01-01T00:00:00Z','2020-01-01T01:00:00Z','2020-01-01T00:00:00Z','2020-01-01T00:00:00Z');
 INSERT INTO draft_picks (draft_session_id,pack_number,pick_number,picked_card_ids,pack_card_ids,pick_ts,created_at)
 VALUES (100,1,1,'[77]','[]','2020-01-01T00:00:00Z','2020-01-01T00:00:00Z');
 INSERT INTO events_raw (log_path,line_no,byte_offset,kind,method_name,payload_json,created_at)
 VALUES ('legacy.log',1,0,'outgoing','LogBusinessEvents','{"EventType":24,"DraftId":"legacy","PackNumber":1,"PickNumber":1,"CardsInPack":[77,78]}','2020-01-01T00:00:00Z');
 INSERT INTO event_runs (event_name,started_at,ended_at,updated_at)
 VALUES ('unrelated','2020-01-02T00:00:00Z','2020-01-01T00:00:00Z','2020-01-01T00:00:00Z');
 `); err != nil {
		t.Fatal(err)
	}
	if err := writeLogLines(logPath, []string{
		`[UnityCrossThreadLogger]==> EventPlayerDraftMakePick {"id":"pick-1","request":{"DraftId":"active","GrpIds":[1002],"Pack":1,"Pick":1}}`,
		`[UnityCrossThreadLogger]Draft.Notify {"draftId":"active","SelfPick":2,"SelfPack":1,"PackCards":"2001,2002"}`,
	}, true); err != nil {
		t.Fatal(err)
	}
	stats, err := parser.ParseFile(ctx, logPath, true)
	if err != nil {
		t.Fatal(err)
	}
	if stats.DraftPicksAdded != 1 || stats.RawEventsStored == 0 {
		t.Fatalf("append did not exercise repair triggers: %#v", stats)
	}
	var pack, picked string
	if err := database.QueryRowContext(ctx, `SELECT pack_card_ids,picked_card_ids FROM draft_picks WHERE draft_session_id=(SELECT id FROM draft_sessions WHERE draft_id='active') AND pick_number=2`).Scan(&pack, &picked); err != nil {
		t.Fatal(err)
	}
	if pack != "[2001,2002]" || picked != "[]" {
		t.Fatalf("next pack = %s, picked = %s", pack, picked)
	}
	if err := database.QueryRowContext(ctx, `SELECT pack_card_ids FROM draft_picks WHERE draft_session_id=100`).Scan(&pack); err != nil {
		t.Fatal(err)
	}
	if pack != "[]" {
		t.Fatalf("live append repaired historical pack: %s", pack)
	}
	var ended string
	if err := database.QueryRowContext(ctx, `SELECT ended_at FROM event_runs WHERE event_name='unrelated'`).Scan(&ended); err != nil {
		t.Fatal(err)
	}
	if ended != "2020-01-01T00:00:00Z" {
		t.Fatalf("live append ran global event repair: %s", ended)
	}

	if _, err := parser.ParseFile(ctx, logPath, false); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `SELECT pack_card_ids FROM draft_picks WHERE draft_session_id=100`).Scan(&pack); err != nil {
		t.Fatal(err)
	}
	if pack != "[77,78]" {
		t.Fatalf("historical import did not repair pack: %s", pack)
	}
	var endedIsNull bool
	if err := database.QueryRowContext(ctx, `SELECT ended_at IS NULL FROM event_runs WHERE event_name='unrelated'`).Scan(&endedIsNull); err != nil {
		t.Fatal(err)
	}
	if !endedIsNull {
		t.Fatal("historical import did not repair event run")
	}
}
