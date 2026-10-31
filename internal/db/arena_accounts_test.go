package db

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/base64"
	"testing"
)

func gzipCheckpoint(t *testing.T, json string) string {
	t.Helper()
	var packed bytes.Buffer
	writer := gzip.NewWriter(&packed)
	if _, err := writer.Write([]byte(json)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return "gzip:" + base64.StdEncoding.EncodeToString(packed.Bytes())
}

// seedLegacyAccountData writes untagged rows the way a database from before
// accounts were recorded holds them.
func seedLegacyAccountData(t *testing.T, db *sql.DB, parserPersona, logPersona string) {
	t.Helper()
	ctx := context.Background()
	if err := Init(ctx, db); err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`INSERT INTO matches (arena_match_id, created_at, updated_at) VALUES ('legacy-match', 'now', 'now')`,
		`INSERT INTO economy_snapshots (log_path, line_no, observed_at, created_at) VALUES ('Player.log', 1, '2026-07-12T10:00:00Z', 'now')`,
		`INSERT INTO app_metadata (key, value, updated_at) VALUES ('player_name', 'bhris', 'now')`,
		`INSERT INTO app_metadata (key, value, updated_at) VALUES ('parser_checkpoint', '{"Version":1,"PersonaID":"` + parserPersona + `"}', 'now')`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO ingest_state (log_path, context_json, updated_at) VALUES ('Player-prev.log', ?, 'now')
	`, gzipCheckpoint(t, `{"Version":3,"PersonaID":"`+logPersona+`"}`)); err != nil {
		t.Fatal(err)
	}
}

func personaOf(t *testing.T, db *sql.DB, query string) sql.NullString {
	t.Helper()
	var persona sql.NullString
	if err := db.QueryRow(query).Scan(&persona); err != nil {
		t.Fatal(err)
	}
	return persona
}

func TestInitAssignsUntaggedRowsToTheOnlyArenaAccount(t *testing.T) {
	db := openTempSQLiteDB(t)
	seedLegacyAccountData(t, db, "ONLY_ACCOUNT", "ONLY_ACCOUNT")
	if err := Init(context.Background(), db); err != nil {
		t.Fatal(err)
	}

	want := sql.NullString{String: "ONLY_ACCOUNT", Valid: true}
	for _, query := range []string{
		`SELECT persona_id FROM matches WHERE arena_match_id = 'legacy-match'`,
		`SELECT persona_id FROM economy_snapshots`,
	} {
		if got := personaOf(t, db, query); got != want {
			t.Fatalf("%s = %v, want %v", query, got, want)
		}
	}
	var name string
	if err := db.QueryRow(`SELECT value FROM app_metadata WHERE key = 'player_name:ONLY_ACCOUNT'`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "bhris" {
		t.Fatalf("account player name = %q, want bhris", name)
	}
}

func TestInitLeavesUntaggedRowsWhenAccountsDisagree(t *testing.T) {
	db := openTempSQLiteDB(t)
	// The rotated log was written by another account, so the legacy rows could
	// belong to either one.
	seedLegacyAccountData(t, db, "ACCOUNT_A", "ACCOUNT_B")
	if err := Init(context.Background(), db); err != nil {
		t.Fatal(err)
	}

	if got := personaOf(t, db, `SELECT persona_id FROM matches WHERE arena_match_id = 'legacy-match'`); got.Valid {
		t.Fatalf("ambiguous legacy match assigned to %q", got.String)
	}
}

func TestInitKeepsTaggedRowsOnceASecondAccountAppears(t *testing.T) {
	ctx := context.Background()
	db := openTempSQLiteDB(t)
	seedLegacyAccountData(t, db, "ACCOUNT_A", "ACCOUNT_A")
	if err := Init(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO matches (arena_match_id, persona_id, created_at, updated_at) VALUES ('b-match', 'ACCOUNT_B', 'now', 'now');
		INSERT INTO matches (arena_match_id, created_at, updated_at) VALUES ('repaired-match', 'now', 'now');
	`); err != nil {
		t.Fatal(err)
	}
	if err := Init(ctx, db); err != nil {
		t.Fatal(err)
	}

	for match, want := range map[string]sql.NullString{
		"legacy-match":   {String: "ACCOUNT_A", Valid: true},
		"b-match":        {String: "ACCOUNT_B", Valid: true},
		"repaired-match": {},
	} {
		if got := personaOf(t, db, `SELECT persona_id FROM matches WHERE arena_match_id = '`+match+`'`); got != want {
			t.Fatalf("%s persona = %v, want %v", match, got, want)
		}
	}
}
