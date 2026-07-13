package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Store struct {
	db *sql.DB
}

type IngestState struct {
	Offset int64
	LineNo int64
	Found  bool
}

const sqliteInClauseBatchSize = 900
const appMetadataPlayerNameKey = "player_name"

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func nowUTC() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func uniquePositiveInt64(values []int64) []int64 {
	if len(values) == 0 {
		return nil
	}

	out := make([]int64, 0, len(values))
	seen := make(map[int64]struct{}, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func int64Batches(values []int64, batchSize int) [][]int64 {
	values = uniquePositiveInt64(values)
	if len(values) == 0 {
		return nil
	}
	if batchSize <= 0 {
		batchSize = sqliteInClauseBatchSize
	}

	out := make([][]int64, 0, (len(values)+batchSize-1)/batchSize)
	for start := 0; start < len(values); start += batchSize {
		end := min(start+batchSize, len(values))
		out = append(out, values[start:end])
	}
	return out
}

func normalizeTS(ts string) string {
	ts = strings.TrimSpace(ts)
	if ts == "" {
		return ""
	}
	parsed, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return ts
	}
	return parsed.UTC().Format(time.RFC3339Nano)
}

func (s *Store) BeginTx(ctx context.Context) (*sql.Tx, error) {
	return s.db.BeginTx(ctx, nil)
}

func (s *Store) GetIngestState(ctx context.Context, logPath string) (IngestState, error) {
	state := IngestState{}
	err := s.db.QueryRowContext(ctx, `
		SELECT byte_offset, line_no
		FROM ingest_state
		WHERE log_path = ?
	`, logPath).Scan(&state.Offset, &state.LineNo)
	if errors.Is(err, sql.ErrNoRows) {
		return state, nil
	}
	if err != nil {
		return state, fmt.Errorf("get ingest_state: %w", err)
	}
	state.Found = true
	return state, nil
}

func (s *Store) SaveIngestState(ctx context.Context, tx *sql.Tx, logPath string, offset, lineNo int64) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO ingest_state (log_path, byte_offset, line_no, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(log_path) DO UPDATE SET
			byte_offset = excluded.byte_offset,
			line_no = excluded.line_no,
			updated_at = excluded.updated_at
	`, logPath, offset, lineNo, nowUTC())
	if err != nil {
		return fmt.Errorf("save ingest_state: %w", err)
	}
	return nil
}

func (s *Store) SavePlayerName(ctx context.Context, tx *sql.Tx, playerName string) error {
	playerName = strings.TrimSpace(playerName)
	if playerName == "" {
		return nil
	}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO app_metadata (key, value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			value = excluded.value,
			updated_at = excluded.updated_at
	`, appMetadataPlayerNameKey, playerName, nowUTC())
	if err != nil {
		return fmt.Errorf("save player name: %w", err)
	}
	return nil
}

func (s *Store) PlayerName(ctx context.Context) (string, error) {
	var playerName string
	err := s.db.QueryRowContext(ctx, `
		SELECT value
		FROM app_metadata
		WHERE key = ?
	`, appMetadataPlayerNameKey).Scan(&playerName)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get player name: %w", err)
	}
	return strings.TrimSpace(playerName), nil
}

// rawEventPersistMethods lists the outgoing methods whose payloads
// RepairDraftDataFromRawEvents reads back. Everything else parses inline
// during ingest and would only occupy space, so it is never stored.
var rawEventPersistMethods = map[string]bool{
	"LogBusinessEvents":        true,
	"EventPlayerDraftMakePick": true,
	"DraftCompleteDraft":       true,
	"EventSetDeckV2":           true,
	"EventSetDeckV3":           true,
}

// logBusinessEventTypeDraftPick is the LogBusinessEvents EventType carrying
// draft pick data; it is the only business event draft repair consumes.
const logBusinessEventTypeDraftPick = 24

// shouldPersistRawEvent reports whether a raw event is worth storing: only
// outgoing draft/deck methods are ever read back, and of LogBusinessEvents
// only the draft pick events.
func shouldPersistRawEvent(kind, method string, payload []byte) bool {
	if kind != "outgoing" || !rawEventPersistMethods[method] {
		return false
	}
	if method != "LogBusinessEvents" {
		return true
	}
	var probe struct {
		EventType int64 `json:"EventType"`
	}
	if err := json.Unmarshal(payload, &probe); err != nil {
		return false
	}
	return probe.EventType == logBusinessEventTypeDraftPick
}

// InsertRawEvent stores a raw log event when a later repair pass can use it
// (see shouldPersistRawEvent). Returns whether a row was written.
func (s *Store) InsertRawEvent(ctx context.Context, tx *sql.Tx, logPath string, lineNo, byteOffset int64, kind, method, requestID string, payload []byte, rawText string) (bool, error) {
	if !shouldPersistRawEvent(kind, method, payload) {
		return false, nil
	}
	payloadText := ""
	if len(payload) > 0 {
		payloadText = string(payload)
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO events_raw (
			log_path, line_no, byte_offset, kind, method_name, request_id, payload_json, raw_text, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, logPath, lineNo, byteOffset, kind, method, requestID, payloadText, rawText, nowUTC())
	if err != nil {
		return false, fmt.Errorf("insert events_raw: %w", err)
	}
	return true, nil
}

// PruneRawEvents deletes stored raw events that no reader consumes — rows
// written before InsertRawEvent started filtering. Returns rows deleted.
func (s *Store) PruneRawEvents(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM events_raw
		WHERE NOT (
			kind = 'outgoing'
			AND method_name IN ('LogBusinessEvents', 'EventPlayerDraftMakePick', 'DraftCompleteDraft', 'EventSetDeckV2', 'EventSetDeckV3')
			AND (
				method_name != 'LogBusinessEvents'
				OR (json_valid(payload_json) AND json_extract(payload_json, '$.EventType') = ?)
			)
		)
	`, logBusinessEventTypeDraftPick)
	if err != nil {
		return 0, fmt.Errorf("prune events_raw: %w", err)
	}
	deleted, err := res.RowsAffected()
	if err != nil {
		return 0, nil
	}
	return deleted, nil
}

func nullableInt(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

func nullableIntPtr(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullInt64Ptr(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	out := v.Int64
	return &out
}

func nullIfEmpty(v string) any {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return v
}

func nullableInt64Ptr(value int64) *int64 {
	out := value
	return &out
}

func parseStoredTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func boolToInt(v bool) int64 {
	if v {
		return 1
	}
	return 0
}
