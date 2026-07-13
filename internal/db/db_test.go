package db

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
)

func TestMigrateMatchObservationTablesRepairsReplayObjectForeignKey(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTempSQLiteDB(t)

	mustExec(t, db, `CREATE TABLE matches (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		arena_match_id TEXT,
		player_seat_id INTEGER
	)`)
	mustExec(t, db, `CREATE TABLE match_card_plays (
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
		created_at TEXT NOT NULL
	)`)
	mustExec(t, db, `CREATE TABLE match_opponent_card_instances (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		match_id INTEGER NOT NULL,
		game_number INTEGER NOT NULL DEFAULT 1,
		instance_id INTEGER NOT NULL,
		card_id INTEGER NOT NULL,
		source TEXT,
		first_seen_at TEXT,
		created_at TEXT NOT NULL
	)`)
	mustExec(t, db, `CREATE TABLE match_replay_frames (
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
	)`)
	mustExec(t, db, `CREATE TABLE match_replay_frame_objects (
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
		FOREIGN KEY(frame_id) REFERENCES match_replay_frames_old(id) ON DELETE CASCADE
	)`)

	mustExec(t, db, `INSERT INTO matches (id, arena_match_id, player_seat_id) VALUES (1, 'match-1', 1)`)
	mustExec(t, db, `INSERT INTO match_replay_frames (
		id, match_id, game_number, game_state_id, created_at
	) VALUES (1, 1, 1, 10, '2026-03-12T01:40:00Z')`)
	mustExec(t, db, `INSERT INTO match_replay_frame_objects (
		id, frame_id, instance_id, card_id, owner_seat_id, controller_seat_id, zone_id, zone_type, zone_position, visibility,
		power, toughness, is_tapped, has_summoning_sickness, attack_state, attack_target_id, block_state,
		block_attacker_ids_json, counter_summary_json, details_json, is_token, created_at
	) VALUES (
		1, 1, 1001, 2001, 1, 2, 15, 'battlefield', 3, 'public',
		4, 5, 1, 1, 'attacking', 77, 'blocking',
		'[1002]', '{"shield":1}', '{"name":"Card A"}', 1, '2026-03-12T01:40:00Z'
	)`)

	if err := migrateMatchObservationTables(ctx, db); err != nil {
		t.Fatalf("migrateMatchObservationTables: %v", err)
	}

	assertReplayObjectFKTarget(t, db, "match_replay_frames")
	assertReplayObjectColumns(t, db, replayObjectExpectations{
		frameID:              1,
		instanceID:           1001,
		cardID:               2001,
		ownerSeatID:          1,
		controllerSeatID:     2,
		zoneID:               15,
		zoneType:             "battlefield",
		zonePosition:         3,
		visibility:           "public",
		power:                4,
		toughness:            5,
		isTapped:             1,
		hasSummoningSickness: 1,
		attackState:          "attacking",
		attackTargetID:       77,
		blockState:           "blocking",
		blockAttackerIDsJSON: "[1002]",
		counterSummaryJSON:   "{\"shield\":1}",
		detailsJSON:          "{\"name\":\"Card A\"}",
		isToken:              1,
	})

	if _, err := db.ExecContext(ctx, `DELETE FROM match_replay_frame_objects WHERE frame_id = 1`); err != nil {
		t.Fatalf("delete repaired replay frame objects: %v", err)
	}
}

func TestMigrateMatchObservationTablesRebuildsReplayFramesBeforeRepairingReplayObjects(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTempSQLiteDB(t)

	mustExec(t, db, `CREATE TABLE matches (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		arena_match_id TEXT,
		player_seat_id INTEGER
	)`)
	mustExec(t, db, `CREATE TABLE match_card_plays (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		match_id INTEGER NOT NULL,
		instance_id INTEGER NOT NULL,
		card_id INTEGER NOT NULL,
		owner_seat_id INTEGER,
		first_public_zone TEXT,
		turn_number INTEGER,
		phase TEXT,
		source TEXT,
		played_at TEXT,
		created_at TEXT NOT NULL
	)`)
	mustExec(t, db, `CREATE TABLE match_opponent_card_instances (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		match_id INTEGER NOT NULL,
		instance_id INTEGER NOT NULL,
		card_id INTEGER NOT NULL,
		source TEXT,
		first_seen_at TEXT,
		created_at TEXT NOT NULL
	)`)
	mustExec(t, db, `CREATE TABLE match_replay_frames (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		match_id INTEGER NOT NULL,
		game_number INTEGER NOT NULL DEFAULT 1,
		game_state_id INTEGER,
		prev_game_state_id INTEGER,
		game_state_type TEXT,
		turn_number INTEGER,
		phase TEXT,
		source TEXT,
		recorded_at TEXT,
		actions_json TEXT,
		annotations_json TEXT,
		created_at TEXT NOT NULL,
		UNIQUE(match_id, game_number, game_state_id),
		FOREIGN KEY(match_id) REFERENCES matches(id) ON DELETE CASCADE
	)`)
	mustExec(t, db, `CREATE TABLE match_replay_frame_objects (
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
	)`)

	mustExec(t, db, `INSERT INTO matches (id, arena_match_id, player_seat_id) VALUES (1, 'match-2', 1)`)
	mustExec(t, db, `INSERT INTO match_replay_frames (
		id, match_id, game_number, game_state_id, created_at
	) VALUES (1, 1, 1, 12, '2026-03-12T01:41:00Z')`)
	mustExec(t, db, `INSERT INTO match_replay_frame_objects (
		id, frame_id, instance_id, card_id, owner_seat_id, controller_seat_id, zone_id, zone_type, zone_position, visibility,
		power, toughness, is_tapped, has_summoning_sickness, attack_state, attack_target_id, block_state,
		block_attacker_ids_json, counter_summary_json, details_json, is_token, created_at
	) VALUES (
		1, 1, 1002, 2002, 2, 1, 21, 'stack', 1, 'public',
		6, 7, 1, 0, 'attacking', 88, 'blocking',
		'[1003]', '{"poison":2}', '{"name":"Card B"}', 0, '2026-03-12T01:41:00Z'
	)`)

	if err := migrateMatchObservationTables(ctx, db); err != nil {
		t.Fatalf("migrateMatchObservationTables: %v", err)
	}

	assertTableHasColumn(t, db, "match_replay_frames", "player_life_totals_json")
	assertTableHasColumn(t, db, "match_replay_frames", "game_stage")
	assertTableHasColumn(t, db, "match_replay_frames", "winning_player_side")
	assertTableHasColumn(t, db, "match_replay_frames", "win_reason")
	assertReplayObjectFKTarget(t, db, "match_replay_frames")
	assertReplayObjectColumns(t, db, replayObjectExpectations{
		frameID:              1,
		instanceID:           1002,
		cardID:               2002,
		ownerSeatID:          2,
		controllerSeatID:     1,
		zoneID:               21,
		zoneType:             "stack",
		zonePosition:         1,
		visibility:           "public",
		power:                6,
		toughness:            7,
		isTapped:             1,
		hasSummoningSickness: 0,
		attackState:          "attacking",
		attackTargetID:       88,
		blockState:           "blocking",
		blockAttackerIDsJSON: "[1003]",
		counterSummaryJSON:   "{\"poison\":2}",
		detailsJSON:          "{\"name\":\"Card B\"}",
		isToken:              0,
	})

	if _, err := db.ExecContext(ctx, `DELETE FROM match_replay_frame_objects WHERE frame_id = 1`); err != nil {
		t.Fatalf("delete replay frame objects after frame rebuild: %v", err)
	}
}

func TestMigrateMatchObservationTablesAddsReplayResultColumnsWithoutTouchingObjects(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTempSQLiteDB(t)

	mustExec(t, db, `CREATE TABLE matches (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		arena_match_id TEXT,
		player_seat_id INTEGER
	)`)
	mustExec(t, db, `CREATE TABLE match_card_plays (
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
		created_at TEXT NOT NULL
	)`)
	mustExec(t, db, `CREATE TABLE match_opponent_card_instances (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		match_id INTEGER NOT NULL,
		game_number INTEGER NOT NULL DEFAULT 1,
		instance_id INTEGER NOT NULL,
		card_id INTEGER NOT NULL,
		source TEXT,
		first_seen_at TEXT,
		created_at TEXT NOT NULL
	)`)
	mustExec(t, db, `CREATE TABLE match_replay_frames (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		match_id INTEGER NOT NULL,
		game_number INTEGER NOT NULL DEFAULT 1,
		game_state_id INTEGER,
		prev_game_state_id INTEGER,
		game_state_type TEXT,
		turn_number INTEGER,
		phase TEXT,
		player_life_totals_json TEXT,
		source TEXT,
		recorded_at TEXT,
		actions_json TEXT,
		annotations_json TEXT,
		created_at TEXT NOT NULL,
		UNIQUE(match_id, game_number, game_state_id),
		FOREIGN KEY(match_id) REFERENCES matches(id) ON DELETE CASCADE
	)`)
	mustExec(t, db, `CREATE TABLE match_replay_frame_objects (
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
	)`)

	mustExec(t, db, `INSERT INTO matches (id, arena_match_id, player_seat_id) VALUES (1, 'match-3', 1)`)
	mustExec(t, db, `INSERT INTO match_replay_frames (
		id, match_id, game_number, game_state_id, player_life_totals_json, created_at
	) VALUES (1, 1, 2, 15, '{"1":20,"2":17}', '2026-03-12T01:42:00Z')`)
	mustExec(t, db, `INSERT INTO match_replay_frame_objects (
		id, frame_id, instance_id, card_id, owner_seat_id, controller_seat_id, zone_id, zone_type, zone_position, visibility,
		power, toughness, is_tapped, has_summoning_sickness, attack_state, attack_target_id, block_state,
		block_attacker_ids_json, counter_summary_json, details_json, is_token, created_at
	) VALUES (
		1, 1, 1003, 2003, 1, 1, 31, 'battlefield', 2, 'public',
		8, 9, 0, 0, '', NULL, '', NULL, '{"energy":3}', '{"name":"Card C"}', 0, '2026-03-12T01:42:00Z'
	)`)

	if err := migrateMatchObservationTables(ctx, db); err != nil {
		t.Fatalf("migrateMatchObservationTables: %v", err)
	}

	assertTableHasColumn(t, db, "match_replay_frames", "game_stage")
	assertTableHasColumn(t, db, "match_replay_frames", "winning_player_side")
	assertTableHasColumn(t, db, "match_replay_frames", "win_reason")
	assertReplayObjectFKTarget(t, db, "match_replay_frames")
	assertReplayObjectColumns(t, db, replayObjectExpectations{
		frameID:              1,
		instanceID:           1003,
		cardID:               2003,
		ownerSeatID:          1,
		controllerSeatID:     1,
		zoneID:               31,
		zoneType:             "battlefield",
		zonePosition:         2,
		visibility:           "public",
		power:                8,
		toughness:            9,
		isTapped:             0,
		hasSummoningSickness: 0,
		attackState:          "",
		attackTargetID:       0,
		blockState:           "",
		blockAttackerIDsJSON: "",
		counterSummaryJSON:   "{\"energy\":3}",
		detailsJSON:          "{\"name\":\"Card C\"}",
		isToken:              0,
	})
}

func openTempSQLiteDB(t *testing.T) *sql.DB {
	t.Helper()

	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open(%s): %v", path, err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	// These tests recreate legacy databases written before foreign keys were
	// enforced (including dangling references), so run them the way Init runs
	// migrations: a single connection with enforcement off.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatalf("disable foreign_keys: %v", err)
	}
	return db
}

func assertReplayObjectFKTarget(t *testing.T, db *sql.DB, want string) {
	t.Helper()

	ctx := context.Background()
	rows, err := db.QueryContext(ctx, `PRAGMA foreign_key_list(match_replay_frame_objects)`)
	if err != nil {
		t.Fatalf("foreign_key_list(match_replay_frame_objects): %v", err)
	}
	defer rows.Close()

	found := false
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
			t.Fatalf("scan foreign key row: %v", err)
		}
		if refTable == want {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate foreign key rows: %v", err)
	}
	if !found {
		t.Fatalf("expected match_replay_frame_objects to reference %q", want)
	}
}

type replayObjectExpectations struct {
	frameID              int64
	instanceID           int64
	cardID               int64
	ownerSeatID          int64
	controllerSeatID     int64
	zoneID               int64
	zoneType             string
	zonePosition         int64
	visibility           string
	power                int64
	toughness            int64
	isTapped             int64
	hasSummoningSickness int64
	attackState          string
	attackTargetID       int64
	blockState           string
	blockAttackerIDsJSON string
	counterSummaryJSON   string
	detailsJSON          string
	isToken              int64
}

func assertReplayObjectColumns(t *testing.T, db *sql.DB, want replayObjectExpectations) {
	t.Helper()

	var (
		got                  replayObjectExpectations
		attackTargetID       sql.NullInt64
		blockAttackerIDsJSON sql.NullString
	)

	err := db.QueryRow(`
		SELECT
			frame_id,
			instance_id,
			card_id,
			COALESCE(owner_seat_id, 0),
			COALESCE(controller_seat_id, 0),
			COALESCE(zone_id, 0),
			zone_type,
			COALESCE(zone_position, 0),
			COALESCE(visibility, ''),
			COALESCE(power, 0),
			COALESCE(toughness, 0),
			is_tapped,
			has_summoning_sickness,
			COALESCE(attack_state, ''),
			attack_target_id,
			COALESCE(block_state, ''),
			block_attacker_ids_json,
			COALESCE(counter_summary_json, ''),
			COALESCE(details_json, ''),
			is_token
		FROM match_replay_frame_objects
		WHERE frame_id = ? AND instance_id = ?
	`, want.frameID, want.instanceID).Scan(
		&got.frameID,
		&got.instanceID,
		&got.cardID,
		&got.ownerSeatID,
		&got.controllerSeatID,
		&got.zoneID,
		&got.zoneType,
		&got.zonePosition,
		&got.visibility,
		&got.power,
		&got.toughness,
		&got.isTapped,
		&got.hasSummoningSickness,
		&got.attackState,
		&attackTargetID,
		&got.blockState,
		&blockAttackerIDsJSON,
		&got.counterSummaryJSON,
		&got.detailsJSON,
		&got.isToken,
	)
	if err != nil {
		t.Fatalf("query replay object columns: %v", err)
	}

	if attackTargetID.Valid {
		got.attackTargetID = attackTargetID.Int64
	}
	if blockAttackerIDsJSON.Valid {
		got.blockAttackerIDsJSON = blockAttackerIDsJSON.String
	}

	if got != want {
		t.Fatalf("replay object = %+v, want %+v", got, want)
	}
}

func assertTableHasColumn(t *testing.T, db *sql.DB, tableName, columnName string) {
	t.Helper()

	hasColumn, err := tableHasColumn(context.Background(), db, tableName, columnName)
	if err != nil {
		t.Fatalf("tableHasColumn(%s.%s): %v", tableName, columnName, err)
	}
	if !hasColumn {
		t.Fatalf("expected %s.%s to exist after migration", tableName, columnName)
	}
}

func mustExec(t *testing.T, db *sql.DB, stmt string) {
	t.Helper()

	if _, err := db.Exec(stmt); err != nil {
		t.Fatalf("exec %s: %v", fmt.Sprintf("%.80s", stmt), err)
	}
}
