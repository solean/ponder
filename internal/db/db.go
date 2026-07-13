package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaFS embed.FS

// Pragmas are connection-scoped in SQLite, so they must ride on the DSN to
// apply to every pooled connection — foreign_keys in particular guards the
// ON DELETE CASCADE cleanup that keeps the database free of orphan rows.
// _txlock=immediate makes concurrent write transactions queue on the busy
// handler instead of failing with SQLITE_BUSY on lock upgrade.
const dsnOptions = "_txlock=immediate" +
	"&_pragma=busy_timeout(5000)" +
	"&_pragma=foreign_keys(1)" +
	"&_pragma=journal_mode(WAL)" +
	"&_pragma=synchronous(NORMAL)"

func dsn(path string) string {
	// url.URL renders a relative path as file://<first-segment>/..., which
	// SQLite reads as an authority, so the path must be absolute.
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	u := url.URL{Scheme: "file", Path: path, RawQuery: dsnOptions}
	return u.String()
}

func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// WAL allows readers to run alongside the single writer; a small pool
	// keeps API reads from queueing behind ingest write batches.
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	return db, nil
}

// dbConn abstracts *sql.DB and *sql.Conn so migrations can run on a dedicated
// connection whose pragmas differ from the pool's.
type dbConn interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func Init(ctx context.Context, db *sql.DB) error {
	schema, err := schemaFS.ReadFile("schema.sql")
	if err != nil {
		return fmt.Errorf("read schema: %w", err)
	}

	if _, err := db.ExecContext(ctx, string(schema)); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}

	// Migrations rebuild tables from legacy databases written before foreign
	// keys were enforced, so they may carry dangling references; run them on a
	// dedicated connection with enforcement off, then restore it before the
	// connection returns to the pool.
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return fmt.Errorf("disable foreign keys for migration: %w", err)
	}
	defer func() {
		_, _ = conn.ExecContext(ctx, `PRAGMA foreign_keys = ON`)
	}()

	if err := migrateMatchObservationTables(ctx, conn); err != nil {
		return err
	}

	if err := dropRedundantIndexes(ctx, conn); err != nil {
		return err
	}

	return nil
}

// dropRedundantIndexes removes indexes that older schema versions created but
// that duplicate the leftmost prefix of a UNIQUE constraint's autoindex or of
// another retained index. They cost space and write amplification during live
// ingest without serving any query.
func dropRedundantIndexes(ctx context.Context, conn dbConn) error {
	stmts := []string{
		`DROP INDEX IF EXISTS idx_match_replay_frames_match_game_state`,
		`DROP INDEX IF EXISTS idx_match_card_plays_match_id`,
		`DROP INDEX IF EXISTS idx_match_opponent_cards_match_id`,
		`DROP INDEX IF EXISTS idx_match_replay_frame_objects_frame_id`,
		`DROP INDEX IF EXISTS idx_events_raw_kind`,
	}
	for _, stmt := range stmts {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("drop redundant index: %w", err)
		}
	}
	return nil
}

func migrateMatchObservationTables(ctx context.Context, db dbConn) error {
	hasGameNo, err := tableHasColumn(ctx, db, "match_card_plays", "game_number")
	if err != nil {
		return fmt.Errorf("inspect match_card_plays schema: %w", err)
	}
	if !hasGameNo {
		if err := rebuildMatchCardPlaysTable(ctx, db); err != nil {
			return err
		}
	}

	hasOpponentGameNo, err := tableHasColumn(ctx, db, "match_opponent_card_instances", "game_number")
	if err != nil {
		return fmt.Errorf("inspect match_opponent_card_instances schema: %w", err)
	}
	if !hasOpponentGameNo {
		if err := rebuildMatchOpponentCardInstancesTable(ctx, db); err != nil {
			return err
		}
	}

	hasReplayLifeTotals, err := tableHasColumn(ctx, db, "match_replay_frames", "player_life_totals_json")
	if err != nil {
		return fmt.Errorf("inspect match_replay_frames schema: %w", err)
	}
	hasReplayGameStage, err := tableHasColumn(ctx, db, "match_replay_frames", "game_stage")
	if err != nil {
		return fmt.Errorf("inspect match_replay_frames game stage schema: %w", err)
	}
	hasReplayWinningSide, err := tableHasColumn(ctx, db, "match_replay_frames", "winning_player_side")
	if err != nil {
		return fmt.Errorf("inspect match_replay_frames winning player side schema: %w", err)
	}
	hasReplayWinReason, err := tableHasColumn(ctx, db, "match_replay_frames", "win_reason")
	if err != nil {
		return fmt.Errorf("inspect match_replay_frames win reason schema: %w", err)
	}
	if !hasReplayLifeTotals {
		if err := rebuildMatchReplayFramesTable(ctx, db); err != nil {
			return err
		}
	} else if !hasReplayGameStage || !hasReplayWinningSide || !hasReplayWinReason {
		if err := addMatchReplayFrameResultColumns(ctx, db, !hasReplayGameStage, !hasReplayWinningSide, !hasReplayWinReason); err != nil {
			return err
		}
	}

	hasReplayControllerSeat, err := tableHasColumn(ctx, db, "match_replay_frame_objects", "controller_seat_id")
	if err != nil {
		return fmt.Errorf("inspect match_replay_frame_objects schema: %w", err)
	}
	replayObjectsReferenceFrames, err := tableHasForeignKeyTarget(ctx, db, "match_replay_frame_objects", "match_replay_frames")
	if err != nil {
		return fmt.Errorf("inspect match_replay_frame_objects foreign keys: %w", err)
	}
	if !hasReplayControllerSeat || !replayObjectsReferenceFrames {
		if err := rebuildMatchReplayFrameObjectsTable(ctx, db); err != nil {
			return err
		}
	}

	return nil
}

func tableHasColumn(ctx context.Context, db dbConn, tableName, columnName string) (bool, error) {
	query := fmt.Sprintf(`PRAGMA table_info(%s)`, tableName)
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &pk); err != nil {
			return false, err
		}
		if strings.EqualFold(strings.TrimSpace(name), strings.TrimSpace(columnName)) {
			return true, nil
		}
	}

	if err := rows.Err(); err != nil {
		return false, err
	}
	return false, nil
}

func tableHasColumnInTx(ctx context.Context, tx *sql.Tx, tableName, columnName string) (bool, error) {
	query := fmt.Sprintf(`PRAGMA table_info(%s)`, tableName)
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &pk); err != nil {
			return false, err
		}
		if strings.EqualFold(strings.TrimSpace(name), strings.TrimSpace(columnName)) {
			return true, nil
		}
	}

	if err := rows.Err(); err != nil {
		return false, err
	}
	return false, nil
}

func tableHasForeignKeyTarget(ctx context.Context, db dbConn, tableName, targetTable string) (bool, error) {
	query := fmt.Sprintf(`PRAGMA foreign_key_list(%s)`, tableName)
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id       int
			seq      int
			refTable string
			fromCol  string
			toCol    string
			onUpdate string
			onDelete string
			match    string
		)
		if err := rows.Scan(&id, &seq, &refTable, &fromCol, &toCol, &onUpdate, &onDelete, &match); err != nil {
			return false, err
		}
		if strings.EqualFold(strings.TrimSpace(refTable), strings.TrimSpace(targetTable)) {
			return true, nil
		}
	}

	if err := rows.Err(); err != nil {
		return false, err
	}
	return false, nil
}

func addMatchReplayFrameResultColumns(ctx context.Context, db dbConn, addGameStage, addWinningPlayerSide, addWinReason bool) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migrate match_replay_frames result columns: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	steps := make([]string, 0, 3)
	if addGameStage {
		steps = append(steps, `ALTER TABLE match_replay_frames ADD COLUMN game_stage TEXT`)
	}
	if addWinningPlayerSide {
		steps = append(steps, `ALTER TABLE match_replay_frames ADD COLUMN winning_player_side TEXT`)
	}
	if addWinReason {
		steps = append(steps, `ALTER TABLE match_replay_frames ADD COLUMN win_reason TEXT`)
	}

	for _, step := range steps {
		if _, err := tx.ExecContext(ctx, step); err != nil {
			return fmt.Errorf("migrate match_replay_frames result columns: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migrate match_replay_frames result columns: %w", err)
	}
	return nil
}

func rebuildMatchCardPlaysTable(ctx context.Context, db dbConn) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migrate match_card_plays: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	steps := []string{
		`ALTER TABLE match_card_plays RENAME TO match_card_plays_old`,
		`DROP INDEX IF EXISTS idx_match_card_plays_match_id`,
		`DROP INDEX IF EXISTS idx_match_card_plays_card_id`,
		`DROP INDEX IF EXISTS idx_match_card_plays_turn_order`,
		`CREATE TABLE match_card_plays (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			match_id INTEGER NOT NULL,
			game_number INTEGER NOT NULL DEFAULT 1,
			instance_id INTEGER NOT NULL,
			card_id INTEGER NOT NULL,
			owner_seat_id INTEGER,
			first_public_zone TEXT,
			turn_number INTEGER,
			phase TEXT,
			source TEXT,
			played_at TEXT,
			created_at TEXT NOT NULL,
			UNIQUE(match_id, game_number, instance_id),
			FOREIGN KEY(match_id) REFERENCES matches(id) ON DELETE CASCADE
		)`,
		`INSERT INTO match_card_plays (
			id, match_id, game_number, instance_id, card_id, owner_seat_id, first_public_zone, turn_number, phase, source, played_at, created_at
		)
		SELECT
			id, match_id, 1, instance_id, card_id, owner_seat_id, first_public_zone, turn_number, phase, source, played_at, created_at
		FROM match_card_plays_old`,
		`CREATE INDEX idx_match_card_plays_card_id ON match_card_plays(card_id)`,
		`CREATE INDEX idx_match_card_plays_turn_order ON match_card_plays(match_id, turn_number, played_at, id)`,
		`DROP TABLE match_card_plays_old`,
	}

	for _, step := range steps {
		if _, err := tx.ExecContext(ctx, step); err != nil {
			return fmt.Errorf("migrate match_card_plays: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migrate match_card_plays: %w", err)
	}
	return nil
}

func rebuildMatchOpponentCardInstancesTable(ctx context.Context, db dbConn) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migrate match_opponent_card_instances: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	steps := []string{
		`ALTER TABLE match_opponent_card_instances RENAME TO match_opponent_card_instances_old`,
		`DROP INDEX IF EXISTS idx_match_opponent_cards_match_id`,
		`DROP INDEX IF EXISTS idx_match_opponent_cards_card_id`,
		`CREATE TABLE match_opponent_card_instances (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			match_id INTEGER NOT NULL,
			game_number INTEGER NOT NULL DEFAULT 1,
			instance_id INTEGER NOT NULL,
			card_id INTEGER NOT NULL,
			source TEXT,
			first_seen_at TEXT,
			created_at TEXT NOT NULL,
			UNIQUE(match_id, game_number, instance_id),
			FOREIGN KEY(match_id) REFERENCES matches(id) ON DELETE CASCADE
		)`,
		`INSERT INTO match_opponent_card_instances (
			id, match_id, game_number, instance_id, card_id, source, first_seen_at, created_at
		)
		SELECT
			id, match_id, 1, instance_id, card_id, source, first_seen_at, created_at
		FROM match_opponent_card_instances_old`,
		`CREATE INDEX idx_match_opponent_cards_card_id ON match_opponent_card_instances(card_id)`,
		`DROP TABLE match_opponent_card_instances_old`,
	}

	for _, step := range steps {
		if _, err := tx.ExecContext(ctx, step); err != nil {
			return fmt.Errorf("migrate match_opponent_card_instances: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migrate match_opponent_card_instances: %w", err)
	}
	return nil
}

func rebuildMatchReplayFrameObjectsTable(ctx context.Context, db dbConn) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migrate match_replay_frame_objects: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `ALTER TABLE match_replay_frame_objects RENAME TO match_replay_frame_objects_old`); err != nil {
		return fmt.Errorf("migrate match_replay_frame_objects: %w", err)
	}
	if err := createMatchReplayFrameObjectsTable(ctx, tx); err != nil {
		return fmt.Errorf("migrate match_replay_frame_objects: %w", err)
	}
	if err := insertMatchReplayFrameObjectsFromTable(ctx, tx, "match_replay_frame_objects_old"); err != nil {
		return fmt.Errorf("migrate match_replay_frame_objects: %w", err)
	}
	if err := createMatchReplayFrameObjectsIndexes(ctx, tx); err != nil {
		return fmt.Errorf("migrate match_replay_frame_objects: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE match_replay_frame_objects_old`); err != nil {
		return fmt.Errorf("migrate match_replay_frame_objects: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migrate match_replay_frame_objects: %w", err)
	}
	return nil
}

func rebuildMatchReplayFramesTable(ctx context.Context, db dbConn) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migrate match_replay_frames: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE match_replay_frame_objects_backup AS SELECT * FROM match_replay_frame_objects`); err != nil {
		return fmt.Errorf("migrate match_replay_frames: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE match_replay_frame_objects`); err != nil {
		return fmt.Errorf("migrate match_replay_frames: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE match_replay_frames RENAME TO match_replay_frames_old`); err != nil {
		return fmt.Errorf("migrate match_replay_frames: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DROP INDEX IF EXISTS idx_match_replay_frames_match_game_state`); err != nil {
		return fmt.Errorf("migrate match_replay_frames: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DROP INDEX IF EXISTS idx_match_replay_frames_turn_order`); err != nil {
		return fmt.Errorf("migrate match_replay_frames: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE match_replay_frames (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		match_id INTEGER NOT NULL,
		game_number INTEGER NOT NULL DEFAULT 1,
		game_state_id INTEGER,
		prev_game_state_id INTEGER,
		game_state_type TEXT,
		game_stage TEXT,
		turn_number INTEGER,
		phase TEXT,
		player_life_totals_json TEXT,
		winning_player_side TEXT,
		win_reason TEXT,
		source TEXT,
		recorded_at TEXT,
		actions_json TEXT,
		annotations_json TEXT,
		created_at TEXT NOT NULL,
		UNIQUE(match_id, game_number, game_state_id),
		FOREIGN KEY(match_id) REFERENCES matches(id) ON DELETE CASCADE
	)`); err != nil {
		return fmt.Errorf("migrate match_replay_frames: %w", err)
	}

	oldHasReplayLifeTotals, err := tableHasColumnInTx(ctx, tx, "match_replay_frames_old", "player_life_totals_json")
	if err != nil {
		return fmt.Errorf("inspect match_replay_frames_old schema: %w", err)
	}
	playerLifeTotalsExpr := "NULL"
	if oldHasReplayLifeTotals {
		playerLifeTotalsExpr = "player_life_totals_json"
	}

	insertQuery := fmt.Sprintf(`
		INSERT INTO match_replay_frames (
			id,
			match_id,
			game_number,
			game_state_id,
			prev_game_state_id,
			game_state_type,
			game_stage,
			turn_number,
			phase,
			player_life_totals_json,
			winning_player_side,
			win_reason,
			source,
			recorded_at,
			actions_json,
			annotations_json,
			created_at
		)
		SELECT
			id,
			match_id,
			game_number,
			game_state_id,
			prev_game_state_id,
			game_state_type,
			'',
			turn_number,
			phase,
			%s,
			'',
			'',
			source,
			recorded_at,
			actions_json,
			annotations_json,
			created_at
		FROM match_replay_frames_old
	`, playerLifeTotalsExpr)
	if _, err := tx.ExecContext(ctx, insertQuery); err != nil {
		return fmt.Errorf("migrate match_replay_frames: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE INDEX idx_match_replay_frames_turn_order ON match_replay_frames(match_id, game_number, turn_number, game_state_id, id)`); err != nil {
		return fmt.Errorf("migrate match_replay_frames: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE match_replay_frames_old`); err != nil {
		return fmt.Errorf("migrate match_replay_frames: %w", err)
	}
	if err := createMatchReplayFrameObjectsTable(ctx, tx); err != nil {
		return fmt.Errorf("migrate match_replay_frames: %w", err)
	}
	if err := insertMatchReplayFrameObjectsFromTable(ctx, tx, "match_replay_frame_objects_backup"); err != nil {
		return fmt.Errorf("migrate match_replay_frames: %w", err)
	}
	if err := createMatchReplayFrameObjectsIndexes(ctx, tx); err != nil {
		return fmt.Errorf("migrate match_replay_frames: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE match_replay_frame_objects_backup`); err != nil {
		return fmt.Errorf("migrate match_replay_frames: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migrate match_replay_frames: %w", err)
	}
	return nil
}

func createMatchReplayFrameObjectsTable(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `DROP INDEX IF EXISTS idx_match_replay_frame_objects_frame_id`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DROP INDEX IF EXISTS idx_match_replay_frame_objects_card_id`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DROP INDEX IF EXISTS idx_match_replay_frame_objects_zone`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE match_replay_frame_objects (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		frame_id INTEGER NOT NULL,
		instance_id INTEGER NOT NULL,
		card_id INTEGER NOT NULL,
		owner_seat_id INTEGER,
		controller_seat_id INTEGER,
		zone_id INTEGER,
		zone_type TEXT NOT NULL,
		zone_position INTEGER,
		visibility TEXT,
		power INTEGER,
		toughness INTEGER,
		is_tapped INTEGER NOT NULL DEFAULT 0,
		has_summoning_sickness INTEGER NOT NULL DEFAULT 0,
		attack_state TEXT,
		attack_target_id INTEGER,
		block_state TEXT,
		block_attacker_ids_json TEXT,
		counter_summary_json TEXT,
		details_json TEXT,
		is_token INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL,
		UNIQUE(frame_id, instance_id),
		FOREIGN KEY(frame_id) REFERENCES match_replay_frames(id) ON DELETE CASCADE
	)`); err != nil {
		return err
	}
	return nil
}

func createMatchReplayFrameObjectsIndexes(ctx context.Context, tx *sql.Tx) error {
	steps := []string{
		`CREATE INDEX idx_match_replay_frame_objects_card_id ON match_replay_frame_objects(card_id)`,
		`CREATE INDEX idx_match_replay_frame_objects_zone ON match_replay_frame_objects(frame_id, zone_type, zone_position, instance_id)`,
	}

	for _, step := range steps {
		if _, err := tx.ExecContext(ctx, step); err != nil {
			return err
		}
	}
	return nil
}

func insertMatchReplayFrameObjectsFromTable(ctx context.Context, tx *sql.Tx, sourceTable string) error {
	ownerSeatIDExpr, err := columnExprInTx(ctx, tx, sourceTable, "owner_seat_id", "NULL")
	if err != nil {
		return err
	}
	controllerSeatIDExpr, err := columnExprInTx(ctx, tx, sourceTable, "controller_seat_id", "NULL")
	if err != nil {
		return err
	}
	zoneIDExpr, err := columnExprInTx(ctx, tx, sourceTable, "zone_id", "NULL")
	if err != nil {
		return err
	}
	zonePositionExpr, err := columnExprInTx(ctx, tx, sourceTable, "zone_position", "NULL")
	if err != nil {
		return err
	}
	visibilityExpr, err := columnExprInTx(ctx, tx, sourceTable, "visibility", "NULL")
	if err != nil {
		return err
	}
	powerExpr, err := columnExprInTx(ctx, tx, sourceTable, "power", "NULL")
	if err != nil {
		return err
	}
	toughnessExpr, err := columnExprInTx(ctx, tx, sourceTable, "toughness", "NULL")
	if err != nil {
		return err
	}
	isTappedExpr, err := columnExprInTx(ctx, tx, sourceTable, "is_tapped", "0")
	if err != nil {
		return err
	}
	hasSummoningSicknessExpr, err := columnExprInTx(ctx, tx, sourceTable, "has_summoning_sickness", "0")
	if err != nil {
		return err
	}
	attackStateExpr, err := columnExprInTx(ctx, tx, sourceTable, "attack_state", "NULL")
	if err != nil {
		return err
	}
	attackTargetIDExpr, err := columnExprInTx(ctx, tx, sourceTable, "attack_target_id", "NULL")
	if err != nil {
		return err
	}
	blockStateExpr, err := columnExprInTx(ctx, tx, sourceTable, "block_state", "NULL")
	if err != nil {
		return err
	}
	blockAttackerIDsExpr, err := columnExprInTx(ctx, tx, sourceTable, "block_attacker_ids_json", "NULL")
	if err != nil {
		return err
	}
	counterSummaryExpr, err := columnExprInTx(ctx, tx, sourceTable, "counter_summary_json", "NULL")
	if err != nil {
		return err
	}
	detailsExpr, err := columnExprInTx(ctx, tx, sourceTable, "details_json", "NULL")
	if err != nil {
		return err
	}
	isTokenExpr, err := columnExprInTx(ctx, tx, sourceTable, "is_token", "0")
	if err != nil {
		return err
	}

	insertQuery := fmt.Sprintf(`
		INSERT INTO match_replay_frame_objects (
			id,
			frame_id,
			instance_id,
			card_id,
			owner_seat_id,
			controller_seat_id,
			zone_id,
			zone_type,
			zone_position,
			visibility,
			power,
			toughness,
			is_tapped,
			has_summoning_sickness,
			attack_state,
			attack_target_id,
			block_state,
			block_attacker_ids_json,
			counter_summary_json,
			details_json,
			is_token,
			created_at
		)
		SELECT
			id,
			frame_id,
			instance_id,
			card_id,
			%s,
			%s,
			%s,
			zone_type,
			%s,
			%s,
			%s,
			%s,
			%s,
			%s,
			%s,
			%s,
			%s,
			%s,
			%s,
			%s,
			%s,
			created_at
		FROM %s
	`,
		ownerSeatIDExpr,
		controllerSeatIDExpr,
		zoneIDExpr,
		zonePositionExpr,
		visibilityExpr,
		powerExpr,
		toughnessExpr,
		isTappedExpr,
		hasSummoningSicknessExpr,
		attackStateExpr,
		attackTargetIDExpr,
		blockStateExpr,
		blockAttackerIDsExpr,
		counterSummaryExpr,
		detailsExpr,
		isTokenExpr,
		sourceTable,
	)

	if _, err := tx.ExecContext(ctx, insertQuery); err != nil {
		return err
	}
	return nil
}

func columnExprInTx(ctx context.Context, tx *sql.Tx, tableName, columnName, fallback string) (string, error) {
	hasColumn, err := tableHasColumnInTx(ctx, tx, tableName, columnName)
	if err != nil {
		return "", err
	}
	if hasColumn {
		return columnName, nil
	}
	return fallback, nil
}
