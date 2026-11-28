package db

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

//go:embed schema_v1.sql
var baselineSchema string

// migrationStep moves a database from one schema version to the next.
type migrationStep func(ctx context.Context, tx *sql.Tx) error

// migrations[v] moves a database from schema version v to v+1, so the newest
// version this build understands is len(migrations). Each step runs in one
// transaction that also records its new version in PRAGMA user_version, so a
// failed step leaves the database at the version it had. Append new steps;
// never change one that has shipped.
var migrations = []migrationStep{
	applyBaselineSchema, // 0 → 1: the pre-1.0 development migrations, squashed.
}

// SchemaVersion is the newest database schema this build understands.
func SchemaVersion() int { return len(migrations) }

// keepBackups is how many pre-migration backups stay beside the database. One
// is enough: an upgrade across several versions backs up once before the
// first step, and an older backup would lose every match played since.
const keepBackups = 1

// NewerSchemaError means a newer Ponder already migrated this database. An
// older build can't know what that migration changed, so it won't touch it.
type NewerSchemaError struct {
	Version   int
	Supported int
}

func (e *NewerSchemaError) Error() string {
	return fmt.Sprintf("this database was last used by a newer version of Ponder "+
		"(schema version %d; this version understands up to %d). Ponder won't open it, "+
		"so it can't damage match history it doesn't understand. Update Ponder to the latest version",
		e.Version, e.Supported)
}

// LegacySchemaError means a database from before schema versioning hadn't
// finished the development migrations that the baseline replaced.
type LegacySchemaError struct {
	Problems []string
}

func (e *LegacySchemaError) Error() string {
	return "this database predates schema versioning and doesn't match the version 1 baseline, " +
		"so Ponder left it unchanged: " + strings.Join(e.Problems, "; ") +
		". Open it once with a Ponder build from before schema versioning to finish its old migrations"
}

func migrate(ctx context.Context, db *sql.DB) error {
	return migrateTo(ctx, db, migrations)
}

func migrateTo(ctx context.Context, db *sql.DB, steps []migrationStep) error {
	target := len(steps)
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Close()

	version, err := readSchemaVersion(ctx, conn)
	if err != nil {
		return err
	}
	if version > target {
		return &NewerSchemaError{Version: version, Supported: target}
	}
	if version == target {
		return nil
	}
	empty, err := databaseIsEmpty(ctx, conn)
	if err != nil {
		return err
	}
	if !empty {
		path, err := backupDatabase(ctx, conn, version, time.Now())
		if err != nil {
			return fmt.Errorf("back up database before migrating from schema version %d: %w", version, err)
		}
		if path != "" {
			log.Printf("db: backed up database to %s before migrating from schema version %d to %d", path, version, target)
		}
	}

	// Rebuilding a table (SQLite's only way to change most constraints) needs
	// foreign keys off, and the pragma is a no-op inside a transaction.
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return fmt.Errorf("disable foreign keys for migration: %w", err)
	}
	defer func() { _, _ = conn.ExecContext(context.WithoutCancel(ctx), `PRAGMA foreign_keys = ON`) }()

	for {
		done, err := applyNextMigration(ctx, conn, steps)
		if err != nil || done {
			return err
		}
	}
}

// applyNextMigration re-reads the version inside its write transaction, so a
// second process migrating the same file waits and then finds nothing to do.
func applyNextMigration(ctx context.Context, conn *sql.Conn, steps []migrationStep) (done bool, err error) {
	target := len(steps)
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	version, err := readSchemaVersion(ctx, tx)
	if err != nil {
		return false, err
	}
	if version > target {
		return false, &NewerSchemaError{Version: version, Supported: target}
	}
	if version == target {
		return true, tx.Commit()
	}

	empty := true
	if version == 0 {
		if empty, err = databaseIsEmpty(ctx, tx); err != nil {
			return false, err
		}
	}
	if version == 0 && !empty {
		err = adoptLegacyDatabase(ctx, tx)
	} else {
		err = steps[version](ctx, tx)
	}
	if err != nil {
		return false, fmt.Errorf("migrate database from schema version %d to %d: %w", version, version+1, err)
	}
	// Recorded in the same transaction as the step, so the two land together.
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, version+1)); err != nil {
		return false, fmt.Errorf("record schema version %d: %w", version+1, err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit migration to schema version %d: %w", version+1, err)
	}
	return version+1 == target, nil
}

func applyBaselineSchema(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, baselineSchema); err != nil {
		return fmt.Errorf("apply baseline schema: %w", err)
	}
	return nil
}

type queryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func readSchemaVersion(ctx context.Context, q queryRower) (int, error) {
	var version int
	if err := q.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	return version, nil
}

func databaseIsEmpty(ctx context.Context, q queryRower) (bool, error) {
	var tables int
	if err := q.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'
	`).Scan(&tables); err != nil {
		return false, fmt.Errorf("inspect database tables: %w", err)
	}
	return tables == 0, nil
}

// backupDatabase copies the database into a backups folder beside it and
// prunes all but the newest keepBackups copies. It returns "" for an
// in-memory database, which has nothing to keep.
func backupDatabase(ctx context.Context, conn *sql.Conn, version int, now time.Time) (string, error) {
	var path string
	if err := conn.QueryRowContext(ctx, `SELECT file FROM pragma_database_list WHERE name = 'main'`).Scan(&path); err != nil {
		return "", fmt.Errorf("locate database file: %w", err)
	}
	if path == "" {
		return "", nil
	}
	dir := filepath.Join(filepath.Dir(path), "backups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	final := filepath.Join(dir, fmt.Sprintf("%s-%s-v%d.db", stem, now.UTC().Format("20060102-150405.000"), version))
	partial := final + ".partial"
	_ = os.Remove(partial)

	// VACUUM INTO writes a consistent copy of a WAL database without blocking
	// readers. Writing under another name first keeps a half-written copy from
	// counting as a backup, or from pushing a good one out.
	if _, err := conn.ExecContext(ctx, `VACUUM INTO ?`, partial); err != nil {
		_ = os.Remove(partial)
		return "", fmt.Errorf("copy database: %w", err)
	}
	if info, err := os.Stat(path); err == nil {
		_ = os.Chmod(partial, info.Mode().Perm())
	}
	if err := os.Rename(partial, final); err != nil {
		_ = os.Remove(partial)
		return "", err
	}
	if err := pruneBackups(dir, stem, keepBackups); err != nil {
		log.Printf("db: prune old database backups: %v", err)
	}
	return final, nil
}

// pruneBackups deletes all but the newest keep backups of stem. Names embed a
// fixed-width UTC timestamp, so name order is age order.
func pruneBackups(dir, stem string, keep int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() && strings.HasPrefix(name, stem+"-") && strings.HasSuffix(name, ".db") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for len(names) > keep {
		if err := os.Remove(filepath.Join(dir, names[0])); err != nil {
			return err
		}
		names = names[1:]
	}
	return nil
}

// legacyMigrationMarkers are the app_metadata keys the development-era data
// backfills wrote once they finished.
var legacyMigrationMarkers = []string{
	"card_stats_backfill_v2",
	"draft_completed_at_repair_v1",
	"economy_backfill_v1",
	"economy_backfill_v2",
	"event_courses_backfill_v1",
	"game_shape_backfill_v1",
	"mulligan_boundary_backfill_v2",
	"opponent_copies_backfill_v3",
	"opponent_hand_replay_backfill_v1",
	"sideboard_snapshots_backfill_v1",
	"spell_copies_backfill_v1",
	"utc_log_header_backfill_v1",
}

// adoptLegacyDatabase stamps a database from before schema versioning as
// version 1 when it already matches the baseline. Those databases ran every
// development migration on each launch, so one that finished them all has the
// baseline's shape. One that didn't can't be finished now that the old
// migrations are gone, so the transaction rolls back and the file is untouched.
func adoptLegacyDatabase(ctx context.Context, tx *sql.Tx) error {
	// Launches used to apply the schema with IF NOT EXISTS, which created
	// tables and indexes added since the database was last opened.
	if _, err := tx.ExecContext(ctx, baselineSchema); err != nil {
		return &LegacySchemaError{Problems: []string{err.Error()}}
	}
	problems, err := legacyBaselineProblems(ctx, tx)
	if err != nil {
		return err
	}
	if len(problems) > 0 {
		return &LegacySchemaError{Problems: problems}
	}
	return nil
}

func legacyBaselineProblems(ctx context.Context, tx *sql.Tx) ([]string, error) {
	want, err := baselineShape(ctx)
	if err != nil {
		return nil, err
	}
	got, err := readSchemaShape(ctx, tx)
	if err != nil {
		return nil, err
	}
	problems := compareToBaseline(want, got)

	for _, key := range legacyMigrationMarkers {
		var found int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM app_metadata WHERE key = ?`, key).Scan(&found); err != nil {
			return nil, fmt.Errorf("check legacy migration marker %s: %w", key, err)
		}
		if found == 0 {
			problems = append(problems, "migration "+key+" never finished")
		}
	}
	return problems, nil
}

// tableShape is what a table's structure is compared on: its columns, its
// foreign keys, and its indexes.
type tableShape struct {
	columns     map[string]string // name → type|notnull|default|pk
	foreignKeys map[string]bool   // table|from|to|on_update|on_delete
	// Constraint indexes (UNIQUE, PRIMARY KEY) have generated names, so they
	// are compared by definition; created indexes by name.
	constraintIndexes map[string]bool
	createdIndexes    map[string]string // name → definition
}

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// One query, so a single-connection pool never holds two result sets open.
const schemaShapeQuery = `
SELECT 'column', m.name, p.name,
	p.type || '|' || p."notnull" || '|' || COALESCE(p.dflt_value, 'NULL') || '|' || p.pk
FROM sqlite_master m JOIN pragma_table_info(m.name) p
WHERE m.type = 'table' AND m.name NOT LIKE 'sqlite_%'
UNION ALL
SELECT 'fk', m.name, '',
	f."table" || '|' || f."from" || '|' || COALESCE(f."to", '') || '|' || f.on_update || '|' || f.on_delete
FROM sqlite_master m JOIN pragma_foreign_key_list(m.name) f
WHERE m.type = 'table' AND m.name NOT LIKE 'sqlite_%'
UNION ALL
SELECT 'index', m.name, il.name,
	il.origin || '|' || il."unique" || '|' || il.partial || '|' ||
	COALESCE((SELECT group_concat(c, ',') FROM (
		SELECT COALESCE(ii.name, '<expr>') AS c FROM pragma_index_info(il.name) ii ORDER BY ii.seqno
	)), '') || '|' ||
	COALESCE((SELECT sql FROM sqlite_master s WHERE s.type = 'index' AND s.name = il.name), '')
FROM sqlite_master m JOIN pragma_index_list(m.name) il
WHERE m.type = 'table' AND m.name NOT LIKE 'sqlite_%'
`

func readSchemaShape(ctx context.Context, q queryer) (map[string]*tableShape, error) {
	rows, err := q.QueryContext(ctx, schemaShapeQuery)
	if err != nil {
		return nil, fmt.Errorf("read schema shape: %w", err)
	}
	defer rows.Close()

	shapes := map[string]*tableShape{}
	for rows.Next() {
		var kind, table, name, definition string
		if err := rows.Scan(&kind, &table, &name, &definition); err != nil {
			return nil, fmt.Errorf("scan schema shape: %w", err)
		}
		shape := shapes[table]
		if shape == nil {
			shape = &tableShape{
				columns:           map[string]string{},
				foreignKeys:       map[string]bool{},
				constraintIndexes: map[string]bool{},
				createdIndexes:    map[string]string{},
			}
			shapes[table] = shape
		}
		switch kind {
		case "column":
			shape.columns[name] = definition
		case "fk":
			shape.foreignKeys[definition] = true
		case "index":
			// The stored CREATE INDEX text keeps the author's whitespace.
			definition = strings.Join(strings.Fields(strings.ToLower(definition)), "")
			if strings.HasPrefix(definition, "c|") {
				shape.createdIndexes[name] = definition
			} else {
				shape.constraintIndexes[definition] = true
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read schema shape: %w", err)
	}
	return shapes, nil
}

func baselineShape(ctx context.Context) (map[string]*tableShape, error) {
	mem, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, fmt.Errorf("open baseline schema: %w", err)
	}
	defer mem.Close()
	// Every connection to :memory: is its own database.
	mem.SetMaxOpenConns(1)
	if _, err := mem.ExecContext(ctx, baselineSchema); err != nil {
		return nil, fmt.Errorf("apply baseline schema: %w", err)
	}
	return readSchemaShape(ctx, mem)
}

// compareToBaseline lists how got differs from the baseline. Tables and plain
// indexes the baseline doesn't have are allowed: development builds of
// unmerged features leave them behind, and they don't change how Ponder reads
// or writes baseline tables. A UNIQUE one would, so it isn't.
func compareToBaseline(want, got map[string]*tableShape) []string {
	var problems []string
	tables := make([]string, 0, len(want))
	for table := range want {
		tables = append(tables, table)
	}
	sort.Strings(tables)

	for _, table := range tables {
		w, g := want[table], got[table]
		if g == nil {
			problems = append(problems, "table "+table+" is missing")
			continue
		}
		for column, definition := range w.columns {
			if have, ok := g.columns[column]; !ok {
				problems = append(problems, fmt.Sprintf("column %s.%s is missing", table, column))
			} else if have != definition {
				problems = append(problems, fmt.Sprintf("column %s.%s is %s, want %s", table, column, have, definition))
			}
		}
		for column := range g.columns {
			if _, ok := w.columns[column]; !ok {
				problems = append(problems, fmt.Sprintf("column %s.%s isn't in the baseline", table, column))
			}
		}
		if !sameSet(w.foreignKeys, g.foreignKeys) {
			problems = append(problems, "foreign keys on "+table+" differ")
		}
		if !sameSet(w.constraintIndexes, g.constraintIndexes) {
			problems = append(problems, "unique constraints on "+table+" differ")
		}
		for name, definition := range w.createdIndexes {
			if have, ok := g.createdIndexes[name]; !ok {
				problems = append(problems, "index "+name+" is missing")
			} else if have != definition {
				problems = append(problems, "index "+name+" differs")
			}
		}
		for name, definition := range g.createdIndexes {
			if _, ok := w.createdIndexes[name]; !ok && strings.HasPrefix(definition, "c|1|") {
				problems = append(problems, "unique index "+name+" isn't in the baseline")
			}
		}
	}
	sort.Strings(problems)
	return problems
}

func sameSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for key := range a {
		if !b[key] {
			return false
		}
	}
	return true
}
