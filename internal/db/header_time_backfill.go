package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Arena's Oct 20 2026 update started writing bare Unity log headers in UTC,
// which ingest kept reading as local time, so header-derived columns written
// since then sit one local UTC offset off (+7h on MST). Shift them back once,
// before the corrected parser writes anything.
const utcLogHeaderBackfillMetadataKey = "utc_log_header_backfill_v1"

type headerTimeColumn struct {
	table, column string
	// insertedAt is a Ponder-clock column set when the row was ingested, or
	// empty when the table has none that can be trusted.
	insertedAt string
}

var headerTimeColumns = []headerTimeColumn{
	{"draft_picks", "pick_ts", "created_at"},
	{"draft_sessions", "started_at", "created_at"},
	{"economy_snapshots", "observed_at", "created_at"},
	{"economy_transactions", "observed_at", "created_at"},
	{"match_rank_snapshots", "observed_at", "created_at"},
	{"draft_sessions", "completed_at", ""},
	{"event_runs", "started_at", ""},
	{"event_runs", "ended_at", ""},
	{"event_courses", "first_observed_at", ""},
	{"event_courses", "observed_at", ""},
}

// Header times have whole seconds; GRE and Ponder-clock times carry fractions.
const headerTimeGlob = `'[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]Z'`

func backfillUTCLogHeaderTimes(ctx context.Context, db dbConn, loc *time.Location) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin utc log header backfill: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var markerCount int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM app_metadata WHERE key = ?
	`, utcLogHeaderBackfillMetadataKey).Scan(&markerCount); err != nil {
		return fmt.Errorf("check utc log header backfill marker: %w", err)
	}
	if markerCount > 0 {
		return tx.Commit()
	}

	// A correct header time can't be later than the row's own ingest, so a
	// value well past it was misread. The earliest such row marks the switch.
	var switchedAt time.Time
	for _, col := range headerTimeColumns {
		if col.insertedAt == "" {
			continue
		}
		var earliest sql.NullString
		if err := tx.QueryRowContext(ctx, fmt.Sprintf(`
			SELECT MIN(%[2]s) FROM %[1]s
			WHERE %[2]s GLOB %[4]s
			  AND julianday(%[2]s) > julianday(%[3]s) + 1.0 / 24
		`, col.table, col.column, col.insertedAt, headerTimeGlob)).Scan(&earliest); err != nil {
			return fmt.Errorf("find misread %s.%s: %w", col.table, col.column, err)
		}
		if !earliest.Valid {
			continue
		}
		parsed, err := time.Parse(time.RFC3339, earliest.String)
		if err != nil {
			continue
		}
		if fixed := unshiftLocalReading(parsed, loc); switchedAt.IsZero() || fixed.Before(switchedAt) {
			switchedAt = fixed
		}
	}

	if !switchedAt.IsZero() {
		for _, col := range headerTimeColumns {
			// Without an ingest time, every header time after the switch was
			// misread: nothing has been written by the corrected parser yet.
			where := fmt.Sprintf(`%s >= ?`, col.column)
			args := []any{switchedAt.Format(time.RFC3339)}
			if col.insertedAt != "" {
				where = fmt.Sprintf(`julianday(%s) > julianday(%s) + 1.0 / 24`, col.column, col.insertedAt)
				args = nil
			}
			if err := shiftHeaderTimeColumn(ctx, tx, col, where, args, loc); err != nil {
				return err
			}
		}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO app_metadata (key, value, updated_at)
		VALUES (?, 'complete', ?)
	`, utcLogHeaderBackfillMetadataKey, nowUTC()); err != nil {
		return fmt.Errorf("save utc log header backfill marker: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit utc log header backfill: %w", err)
	}
	return nil
}

func shiftHeaderTimeColumn(ctx context.Context, tx *sql.Tx, col headerTimeColumn, where string, args []any, loc *time.Location) error {
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`
		SELECT rowid, %[2]s FROM %[1]s
		WHERE %[2]s GLOB %[3]s AND %[4]s
	`, col.table, col.column, headerTimeGlob, where), args...)
	if err != nil {
		return fmt.Errorf("select misread %s.%s: %w", col.table, col.column, err)
	}
	type fix struct {
		rowID int64
		value string
	}
	var fixes []fix
	for rows.Next() {
		var rowID int64
		var value string
		if err := rows.Scan(&rowID, &value); err != nil {
			rows.Close()
			return fmt.Errorf("scan misread %s.%s: %w", col.table, col.column, err)
		}
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			continue
		}
		fixes = append(fixes, fix{rowID, unshiftLocalReading(parsed, loc).Format(time.RFC3339)})
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read misread %s.%s: %w", col.table, col.column, err)
	}

	// OR IGNORE: economy snapshots are unique per log line and time, and a
	// replay may already have stored the corrected twin.
	update := fmt.Sprintf(`UPDATE OR IGNORE %s SET %s = ? WHERE rowid = ?`, col.table, col.column)
	for _, f := range fixes {
		if _, err := tx.ExecContext(ctx, update, f.value, f.rowID); err != nil {
			return fmt.Errorf("shift %s.%s: %w", col.table, col.column, err)
		}
	}
	return nil
}

// unshiftLocalReading undoes reading a UTC wall-clock time as local time.
func unshiftLocalReading(stored time.Time, loc *time.Location) time.Time {
	wall := stored.In(loc)
	return time.Date(wall.Year(), wall.Month(), wall.Day(), wall.Hour(), wall.Minute(), wall.Second(), wall.Nanosecond(), time.UTC)
}
