package db

import (
	"context"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func newFastCompressed(t *testing.T, raw []byte) []byte {
	t.Helper()
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest))
	if err != nil {
		t.Fatalf("init fast zstd encoder: %v", err)
	}
	defer enc.Close()
	return enc.EncodeAll(raw, nil)
}

func TestInsertRawEventFiltersUnreadEvents(t *testing.T) {
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
	defer func() { _ = tx.Rollback() }()

	cases := []struct {
		name    string
		kind    string
		method  string
		payload string
		want    bool
	}{
		{"draft pick business event", "outgoing", "LogBusinessEvents", `{"EventType":24,"DraftId":"d1"}`, true},
		{"other business event", "outgoing", "LogBusinessEvents", `{"EventType":7}`, false},
		{"player draft pick", "outgoing", "EventPlayerDraftMakePick", `{"DraftId":"d1","Pack":1,"Pick":1}`, true},
		{"draft complete", "outgoing", "DraftCompleteDraft", `{"EventName":"E","IsBotDraft":false}`, true},
		{"set deck v2", "outgoing", "EventSetDeckV2", `{"EventName":"PremierDraft_X"}`, true},
		{"unread outgoing method", "outgoing", "DeckUpsertDeckV2", `{"Deck":{}}`, false},
		{"method complete marker", "method_complete", "LogBusinessEvents", ``, false},
		{"room state marker", "room_state", "matchGameRoomStateChangedEvent", ``, false},
		{"rank method result", "method_result", "RankGetCombinedRankInfo", `{"ConstructedClass":"Gold"}`, false},
	}

	for _, tc := range cases {
		stored, err := store.InsertRawEvent(ctx, tx, "Player.log", 1, 1, tc.kind, tc.method, "", []byte(tc.payload), "")
		if err != nil {
			t.Fatalf("%s: InsertRawEvent: %v", tc.name, err)
		}
		if stored != tc.want {
			t.Errorf("%s: stored = %v, want %v", tc.name, stored, tc.want)
		}
	}
}

func TestPruneRawEventsKeepsOnlyRepairInputs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	database := openTempSQLiteDB(t)
	if err := Init(ctx, database); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Insert legacy-style rows directly, bypassing the InsertRawEvent filter.
	mustExec(t, database, `
		INSERT INTO events_raw (log_path, line_no, byte_offset, kind, method_name, request_id, payload_json, raw_text, created_at) VALUES
		('p', 1, 1, 'outgoing', 'LogBusinessEvents', '', '{"EventType":24,"DraftId":"d1"}', '', '2026-01-01T00:00:00Z'),
		('p', 2, 2, 'outgoing', 'LogBusinessEvents', '', '{"EventType":7}', '', '2026-01-01T00:00:00Z'),
		('p', 3, 3, 'outgoing', 'EventPlayerDraftMakePick', '', '{"DraftId":"d1"}', '', '2026-01-01T00:00:00Z'),
		('p', 4, 4, 'outgoing', 'DeckUpsertDeckV2', '', '{"Deck":{}}', '', '2026-01-01T00:00:00Z'),
		('p', 5, 5, 'method_complete', 'QuestGetQuests', 'r5', '', '', '2026-01-01T00:00:00Z'),
		('p', 6, 6, 'room_state', 'matchGameRoomStateChangedEvent', '', '', '', '2026-01-01T00:00:00Z'),
		('p', 7, 7, 'method_result', 'RankGetCombinedRankInfo', 'r7', '{"ConstructedClass":"Gold"}', '', '2026-01-01T00:00:00Z')
	`)

	store := NewStore(database)
	pruned, err := store.PruneRawEvents(ctx)
	if err != nil {
		t.Fatalf("PruneRawEvents: %v", err)
	}
	if pruned != 5 {
		t.Fatalf("pruned = %d, want 5", pruned)
	}

	var remaining int
	if err := database.QueryRow(`SELECT COUNT(*) FROM events_raw`).Scan(&remaining); err != nil {
		t.Fatalf("count events_raw: %v", err)
	}
	if remaining != 2 {
		t.Fatalf("remaining rows = %d, want 2", remaining)
	}
}

func TestRunMaintenanceRecompressesArchivesOnce(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	database := openTempSQLiteDB(t)
	if err := Init(ctx, database); err != nil {
		t.Fatalf("Init: %v", err)
	}

	mustExec(t, database, `INSERT INTO matches (arena_match_id, created_at, updated_at) VALUES ('m1', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`)

	// Store a payload compressed at the fastest level so best level can win.
	raw := make([]byte, 0, 64*1024)
	for i := 0; i < 2048; i++ {
		raw = append(raw, []byte(`{"instanceId":123,"zoneType":"battlefield"}`)...)
	}
	fast := newFastCompressed(t, raw)
	if _, err := database.Exec(`
		INSERT INTO match_replay_archives (match_id, schema_version, frame_count, object_count, payload_zstd, created_at, updated_at)
		VALUES (1, 1, 1, 1, ?, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')
	`, fast); err != nil {
		t.Fatalf("insert archive: %v", err)
	}

	store := NewStore(database)
	result, err := store.RunMaintenance(ctx)
	if err != nil {
		t.Fatalf("RunMaintenance: %v", err)
	}
	if result.ArchivesRecompressed != 1 {
		t.Fatalf("ArchivesRecompressed = %d, want 1", result.ArchivesRecompressed)
	}

	var stored []byte
	if err := database.QueryRow(`SELECT payload_zstd FROM match_replay_archives WHERE match_id = 1`).Scan(&stored); err != nil {
		t.Fatalf("load recompressed archive: %v", err)
	}
	if len(stored) >= len(fast) {
		t.Fatalf("recompressed archive is %d bytes, want smaller than %d", len(stored), len(fast))
	}
	roundTripped, err := getZstdDecoder().DecodeAll(stored, nil)
	if err != nil {
		t.Fatalf("decompress recompressed archive: %v", err)
	}
	if string(roundTripped) != string(raw) {
		t.Fatal("recompressed archive does not round-trip to the original payload")
	}

	// A second pass must skip the recompress entirely.
	result, err = store.RunMaintenance(ctx)
	if err != nil {
		t.Fatalf("RunMaintenance second pass: %v", err)
	}
	if result.ArchivesRecompressed != 0 {
		t.Fatalf("second pass ArchivesRecompressed = %d, want 0", result.ArchivesRecompressed)
	}
}
