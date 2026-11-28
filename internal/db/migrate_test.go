package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func openMigrationTestDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ponder.db")
	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database, path
}

func schemaVersionOf(t *testing.T, database *sql.DB) int {
	t.Helper()
	version, err := readSchemaVersion(context.Background(), database)
	if err != nil {
		t.Fatal(err)
	}
	return version
}

func backupsOf(t *testing.T, dbPath string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(filepath.Dir(dbPath), "backups"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

// seedLegacyDatabase writes a database the way pre-versioning builds left it
// after finishing every development migration: baseline tables, the backfill
// markers, and user_version still 0.
func seedLegacyDatabase(t *testing.T, database *sql.DB) {
	t.Helper()
	mustExec(t, database, baselineSchema)
	for _, key := range legacyMigrationMarkers {
		if _, err := database.Exec(`INSERT INTO app_metadata (key, value, updated_at) VALUES (?, 'complete', 'now')`, key); err != nil {
			t.Fatal(err)
		}
	}
	mustExec(t, database, `INSERT INTO matches (arena_match_id, created_at, updated_at) VALUES ('kept-match', 'now', 'now')`)
}

func TestInitCreatesCurrentSchemaWithoutBackup(t *testing.T) {
	t.Parallel()
	database, path := openMigrationTestDB(t)
	if err := Init(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	if got := schemaVersionOf(t, database); got != SchemaVersion() {
		t.Fatalf("schema version = %d, want %d", got, SchemaVersion())
	}
	if backups := backupsOf(t, path); len(backups) != 0 {
		t.Fatalf("new database was backed up: %v", backups)
	}
	if err := Init(context.Background(), database); err != nil {
		t.Fatalf("second Init: %v", err)
	}
	if backups := backupsOf(t, path); len(backups) != 0 {
		t.Fatalf("current database was backed up: %v", backups)
	}
}

func TestInitRefusesNewerSchema(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database, path := openMigrationTestDB(t)
	if err := Init(ctx, database); err != nil {
		t.Fatal(err)
	}
	mustExec(t, database, fmt.Sprintf(`PRAGMA user_version = %d`, SchemaVersion()+1))

	err := Init(ctx, database)
	var newer *NewerSchemaError
	if !errors.As(err, &newer) {
		t.Fatalf("Init = %v, want NewerSchemaError", err)
	}
	if newer.Version != SchemaVersion()+1 || newer.Supported != SchemaVersion() {
		t.Fatalf("NewerSchemaError = %+v", newer)
	}
	if !strings.Contains(err.Error(), "newer version of Ponder") {
		t.Fatalf("error doesn't explain itself: %v", err)
	}
	if backups := backupsOf(t, path); len(backups) != 0 {
		t.Fatalf("refused database was backed up: %v", backups)
	}
}

func TestMigrationBacksUpBeforeChangingDatabase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database, path := openMigrationTestDB(t)
	if err := Init(ctx, database); err != nil {
		t.Fatal(err)
	}
	mustExec(t, database, `INSERT INTO matches (arena_match_id, created_at, updated_at) VALUES ('kept-match', 'now', 'now')`)

	steps := append(append([]migrationStep{}, migrations...), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM matches`)
		return err
	})
	if err := migrateTo(ctx, database, steps); err != nil {
		t.Fatal(err)
	}
	if got := schemaVersionOf(t, database); got != len(steps) {
		t.Fatalf("schema version = %d, want %d", got, len(steps))
	}

	backups := backupsOf(t, path)
	wantSuffix := fmt.Sprintf("-v%d.db", SchemaVersion())
	if len(backups) != 1 || !strings.HasPrefix(backups[0], "ponder-") || !strings.HasSuffix(backups[0], wantSuffix) {
		t.Fatalf("backups = %v, want one ponder-*%s", backups, wantSuffix)
	}
	backup, err := Open(filepath.Join(filepath.Dir(path), "backups", backups[0]))
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Close()
	var matches int
	if err := backup.QueryRow(`SELECT COUNT(*) FROM matches`).Scan(&matches); err != nil {
		t.Fatal(err)
	}
	if matches != 1 || schemaVersionOf(t, backup) != SchemaVersion() {
		t.Fatalf("backup has %d matches at version %d, want the pre-migration database", matches, schemaVersionOf(t, backup))
	}
}

func TestFailedMigrationKeepsPreviousVersionAndData(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database, _ := openMigrationTestDB(t)
	if err := Init(ctx, database); err != nil {
		t.Fatal(err)
	}
	mustExec(t, database, `INSERT INTO matches (arena_match_id, created_at, updated_at) VALUES ('kept-match', 'now', 'now')`)

	steps := append(append([]migrationStep{}, migrations...), func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM matches`); err != nil {
			return err
		}
		return errors.New("step failed halfway")
	})
	if err := migrateTo(ctx, database, steps); err == nil {
		t.Fatal("migrateTo succeeded, want the step's error")
	}
	if got := schemaVersionOf(t, database); got != SchemaVersion() {
		t.Fatalf("schema version = %d, want %d", got, SchemaVersion())
	}
	var matches int
	if err := database.QueryRow(`SELECT COUNT(*) FROM matches`).Scan(&matches); err != nil {
		t.Fatal(err)
	}
	if matches != 1 {
		t.Fatalf("matches = %d, want the failed step rolled back", matches)
	}
}

func TestBackupsKeepOnlyTheNewest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database, path := openMigrationTestDB(t)
	if err := Init(ctx, database); err != nil {
		t.Fatal(err)
	}
	conn, err := database.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	start := time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC)
	var made []string
	for i := range keepBackups + 2 {
		backup, err := backupDatabase(ctx, conn, 1, start.Add(time.Duration(i)*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		made = append(made, filepath.Base(backup))
	}
	got := backupsOf(t, path)
	want := made[len(made)-keepBackups:]
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("backups = %v, want the newest %v", got, want)
	}
}

func TestInitAdoptsFinishedLegacyDatabase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database, path := openMigrationTestDB(t)
	seedLegacyDatabase(t, database)
	// A build from before card_name_misses existed; launches created missing
	// tables, so adoption does too.
	mustExec(t, database, `DROP TABLE card_name_misses`)
	// Unmerged feature branches leave their own tables and plain indexes.
	mustExec(t, database, `CREATE TABLE deck_projects (id INTEGER PRIMARY KEY, name TEXT NOT NULL)`)
	mustExec(t, database, `CREATE INDEX idx_games_match_number ON games(match_id, game_number)`)

	if err := Init(ctx, database); err != nil {
		t.Fatal(err)
	}
	if got := schemaVersionOf(t, database); got != SchemaVersion() {
		t.Fatalf("schema version = %d, want %d", got, SchemaVersion())
	}
	var tables int
	if err := database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name IN ('card_name_misses', 'deck_projects')`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 2 {
		t.Fatalf("found %d of card_name_misses and deck_projects, want both", tables)
	}
	if backups := backupsOf(t, path); len(backups) != 1 || !strings.HasSuffix(backups[0], "-v0.db") {
		t.Fatalf("backups = %v, want one -v0.db", backups)
	}
}

func TestInitLeavesUnfinishedLegacyDatabaseUntouched(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		damage string
		want   string
	}{
		"missing column":    {`ALTER TABLE ingest_state DROP COLUMN context_json`, "column ingest_state.context_json is missing"},
		"missing marker":    {`DELETE FROM app_metadata WHERE key = 'utc_log_header_backfill_v1'`, "utc_log_header_backfill_v1 never finished"},
		"stale foreign key": {"", "foreign keys on match_decks differ"},
		"extra unique":      {`CREATE UNIQUE INDEX idx_snapshot_line ON economy_snapshots(log_path, line_no)`, "unique index idx_snapshot_line"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			database, _ := openMigrationTestDB(t)
			seedLegacyDatabase(t, database)
			if tc.damage != "" {
				mustExec(t, database, tc.damage)
			} else {
				// An older build's table whose rows outlive their match.
				var create string
				if err := database.QueryRow(`SELECT sql FROM sqlite_master WHERE name = 'match_decks'`).Scan(&create); err != nil {
					t.Fatal(err)
				}
				weakened := strings.Replace(create, "REFERENCES matches(id) ON DELETE CASCADE", "REFERENCES matches(id)", 1)
				if weakened == create {
					t.Fatal("match_decks no longer cascades from matches; update this test")
				}
				mustExec(t, database, `DROP TABLE match_decks`)
				mustExec(t, database, weakened)
			}
			mustExec(t, database, `DROP TABLE card_name_misses`)

			err := Init(ctx, database)
			var legacy *LegacySchemaError
			if !errors.As(err, &legacy) {
				t.Fatalf("Init = %v, want LegacySchemaError", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Init = %v, want it to mention %q", err, tc.want)
			}
			if got := schemaVersionOf(t, database); got != 0 {
				t.Fatalf("schema version = %d, want 0", got)
			}
			var recreated int
			if err := database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'card_name_misses'`).Scan(&recreated); err != nil {
				t.Fatal(err)
			}
			if recreated != 0 {
				t.Fatal("refused database was changed: card_name_misses was created")
			}
		})
	}
}

func TestVersion1DatabaseGainsCardPreviews(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database, _ := openMigrationTestDB(t)
	if err := migrateTo(ctx, database, migrations[:1]); err != nil {
		t.Fatal(err)
	}
	mustExec(t, database, `INSERT INTO matches (arena_match_id, created_at, updated_at) VALUES ('kept-match', 'now', 'now')`)

	if err := Init(ctx, database); err != nil {
		t.Fatal(err)
	}
	var matches, previews int
	if err := database.QueryRow(`SELECT COUNT(*) FROM matches`).Scan(&matches); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM card_previews`).Scan(&previews); err != nil {
		t.Fatalf("card_previews missing after upgrade: %v", err)
	}
	if matches != 1 || previews != 0 {
		t.Fatalf("matches = %d, previews = %d after upgrade", matches, previews)
	}
}
