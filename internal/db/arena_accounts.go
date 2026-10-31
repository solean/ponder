package db

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// personaTables are the root rows for each kind of ingested data. Child rows
// (games, picks, deck cards, transactions) inherit their account from these.
var personaTables = []string{
	"matches",
	"decks",
	"draft_sessions",
	"event_runs",
	"event_courses",
	"economy_snapshots",
	"match_rank_snapshots",
}

func playerNameKey(personaID string) string {
	return appMetadataPlayerNameKey + ":" + personaID
}

func migrateArenaAccountColumns(ctx context.Context, conn dbConn) error {
	for _, table := range personaTables {
		exists, err := tableHasColumn(ctx, conn, table, "persona_id")
		if err != nil {
			return fmt.Errorf("inspect %s persona column: %w", table, err)
		}
		if exists {
			continue
		}
		if _, err := conn.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE %s ADD COLUMN persona_id TEXT`, table)); err != nil {
			return fmt.Errorf("add %s persona column: %w", table, err)
		}
	}
	return nil
}

// assignSingleArenaAccount gives untagged rows to the only Arena account this
// database has seen. That covers rows written before accounts were recorded
// and rows that maintenance repairs recreate outside ingest. Once a second
// account appears, untagged rows are ambiguous and stay NULL.
func assignSingleArenaAccount(ctx context.Context, conn dbConn) error {
	personas, err := knownArenaAccounts(ctx, conn)
	if err != nil {
		return err
	}
	if len(personas) != 1 {
		return nil
	}
	var personaID string
	for only := range personas {
		personaID = only
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin arena account assignment: %w", err)
	}
	defer tx.Rollback()
	for _, table := range personaTables {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`UPDATE %s SET persona_id = ? WHERE persona_id IS NULL`, table), personaID); err != nil {
			return fmt.Errorf("assign %s arena account: %w", table, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO app_metadata (key, value, updated_at)
		SELECT ?, value, ? FROM app_metadata WHERE key = ?
		ON CONFLICT(key) DO NOTHING
	`, playerNameKey(personaID), nowUTC(), appMetadataPlayerNameKey); err != nil {
		return fmt.Errorf("assign arena account player name: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit arena account assignment: %w", err)
	}
	return nil
}

// knownArenaAccounts collects every account recorded on a row or in a parser
// checkpoint. Checkpoints count because each log file's checkpoint holds the
// account that wrote it, including logs ingested before rows were tagged.
func knownArenaAccounts(ctx context.Context, conn dbConn) (map[string]struct{}, error) {
	personas := make(map[string]struct{})
	add := func(personaID string) {
		if personaID = strings.TrimSpace(personaID); personaID != "" {
			personas[personaID] = struct{}{}
		}
	}

	selects := make([]string, 0, len(personaTables))
	for _, table := range personaTables {
		selects = append(selects, fmt.Sprintf(`SELECT persona_id FROM %s WHERE persona_id IS NOT NULL`, table))
	}
	tagged, err := queryStrings(ctx, conn, strings.Join(selects, " UNION "))
	if err != nil {
		return nil, fmt.Errorf("list tagged arena accounts: %w", err)
	}
	for _, personaID := range tagged {
		add(personaID)
	}

	checkpoints, err := queryStrings(ctx, conn, `
		SELECT value FROM app_metadata WHERE key = 'parser_checkpoint'
		UNION ALL
		SELECT context_json FROM ingest_state WHERE context_json != ''
	`)
	if err != nil {
		return nil, fmt.Errorf("list parser checkpoints: %w", err)
	}
	for _, checkpoint := range checkpoints {
		add(checkpointPersonaID(checkpoint))
	}
	return personas, nil
}

func queryStrings(ctx context.Context, conn dbConn, query string, args ...any) ([]string, error) {
	rows, err := conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, rows.Err()
}

// checkpointPersonaID reads the PersonaID field of a parser checkpoint, which
// the ingest package stores as JSON, optionally gzipped behind a "gzip:"
// prefix. Unreadable checkpoints contribute no account.
func checkpointPersonaID(data string) string {
	payload := []byte(data)
	if packed, ok := strings.CutPrefix(data, "gzip:"); ok {
		compressed, err := base64.StdEncoding.DecodeString(packed)
		if err != nil {
			return ""
		}
		reader, err := gzip.NewReader(bytes.NewReader(compressed))
		if err != nil {
			return ""
		}
		defer reader.Close()
		if payload, err = io.ReadAll(reader); err != nil {
			return ""
		}
	}
	var wire struct{ PersonaID string }
	if json.Unmarshal(payload, &wire) != nil {
		return ""
	}
	return wire.PersonaID
}

// PersonaWatermarks records each root table's highest rowid when an ingest
// batch begins, so ClaimPersonaRows tags only the rows that batch created.
type PersonaWatermarks map[string]int64

func (s *Store) PersonaWatermarks(ctx context.Context, tx *sql.Tx) (PersonaWatermarks, error) {
	out := make(PersonaWatermarks, len(personaTables))
	for _, table := range personaTables {
		var rowID int64
		if err := tx.QueryRowContext(ctx, fmt.Sprintf(`SELECT COALESCE(MAX(rowid), 0) FROM %s`, table)).Scan(&rowID); err != nil {
			return nil, fmt.Errorf("read %s watermark: %w", table, err)
		}
		out[table] = rowID
	}
	return out, nil
}

// ClaimPersonaRows tags untagged rows created since the watermarks with the
// account that was active while they were ingested. Rows updated in place
// keep the account they already had.
func (s *Store) ClaimPersonaRows(ctx context.Context, tx *sql.Tx, personaID string, since PersonaWatermarks) error {
	personaID = strings.TrimSpace(personaID)
	if personaID == "" {
		return errors.New("claim persona rows: empty persona id")
	}
	for _, table := range personaTables {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
			UPDATE %s SET persona_id = ? WHERE rowid > ? AND persona_id IS NULL
		`, table), personaID, since[table]); err != nil {
			return fmt.Errorf("claim %s rows for arena account: %w", table, err)
		}
	}
	return nil
}
