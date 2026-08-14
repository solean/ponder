package db

import (
	"context"
	"fmt"
)

func migrateAnalyticsTables(ctx context.Context, conn dbConn) error {
	hasDeckVersionID, err := tableHasColumn(ctx, conn, "match_decks", "deck_version_id")
	if err != nil {
		return fmt.Errorf("inspect match_decks deck version schema: %w", err)
	}
	if !hasDeckVersionID {
		if _, err := conn.ExecContext(ctx, `
			ALTER TABLE match_decks
			ADD COLUMN deck_version_id INTEGER REFERENCES deck_versions(id) ON DELETE SET NULL
		`); err != nil {
			return fmt.Errorf("add match deck version: %w", err)
		}
	}
	if _, err := conn.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_match_decks_version ON match_decks(deck_version_id)
	`); err != nil {
		return fmt.Errorf("index match deck versions: %w", err)
	}

	// Game-shape columns arrived with turn-stat analytics; older databases
	// already have the tables, so add the columns in place.
	shapeColumns := []struct {
		table  string
		column string
		ddl    string
	}{
		{"games", "min_self_life", `ALTER TABLE games ADD COLUMN min_self_life INTEGER`},
		{"games", "min_opponent_life", `ALTER TABLE games ADD COLUMN min_opponent_life INTEGER`},
		{"match_analytics_coverage", "games_with_turn_stats",
			`ALTER TABLE match_analytics_coverage ADD COLUMN games_with_turn_stats INTEGER NOT NULL DEFAULT 0`},
	}
	for _, migration := range shapeColumns {
		hasColumn, err := tableHasColumn(ctx, conn, migration.table, migration.column)
		if err != nil {
			return fmt.Errorf("inspect %s %s schema: %w", migration.table, migration.column, err)
		}
		if hasColumn {
			continue
		}
		if _, err := conn.ExecContext(ctx, migration.ddl); err != nil {
			return fmt.Errorf("add %s.%s: %w", migration.table, migration.column, err)
		}
	}
	return nil
}

// v2: v1 was briefly consumable by a build that created the marker before the
// card-stat derivation existed, leaving game_card_stats empty.
const cardStatsBackfillMetadataKey = "card_stats_backfill_v2"

const gameShapeBackfillMetadataKey = "game_shape_backfill_v1"

// v2: v1 counted simultaneous instances across every zone, which still
// overcounted a recast card whose stale instances linger in graveyard/exile/
// limbo; v2 restricts the count to the clean battlefield/hand zones.
const opponentCopiesBackfillMetadataKey = "opponent_copies_backfill_v2"

// prepareGameShapeBackfill invalidates analytics coverage exactly once after
// per-turn game shape stats were introduced, so the next maintenance pass (or
// per-match EnsureMatchAnalytics) re-derives every match and populates
// game_turn_stats and the min-life columns from archived replays.
func prepareGameShapeBackfill(ctx context.Context, conn dbConn) error {
	return invalidateAnalyticsCoverageOnce(ctx, conn, gameShapeBackfillMetadataKey)
}

// prepareOpponentCopiesBackfill invalidates analytics coverage once so every
// match re-derives and fills match_opponent_card_counts from archived replays.
func prepareOpponentCopiesBackfill(ctx context.Context, conn dbConn) error {
	return invalidateAnalyticsCoverageOnce(ctx, conn, opponentCopiesBackfillMetadataKey)
}

// v2: v1 was consumable by a build that still credited a kept hand to a game
// conceded inside its mulligan sequence.
const mulliganBoundaryBackfillMetadataKey = "mulligan_boundary_backfill_v2"

// prepareMulliganBoundaryBackfill invalidates analytics coverage once after the
// pre-game frame fix. Games after the first inherited the previous game's turn
// number on their mulligan frames, so every one of them derived zero mulligans
// and a turn count borrowed from the game before it.
func prepareMulliganBoundaryBackfill(ctx context.Context, conn dbConn) error {
	return invalidateAnalyticsCoverageOnce(ctx, conn, mulliganBoundaryBackfillMetadataKey)
}

// invalidateAnalyticsCoverageOnce clears match_analytics_coverage exactly once,
// keyed by markerKey, so the next maintenance pass (or per-match
// EnsureMatchAnalytics) re-derives every match from archived replays. Used
// whenever a new derived field is added to the analytics pipeline.
func invalidateAnalyticsCoverageOnce(ctx context.Context, conn dbConn, markerKey string) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin %s migration: %w", markerKey, err)
	}
	defer func() { _ = tx.Rollback() }()

	var markerCount int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM app_metadata WHERE key = ?
	`, markerKey).Scan(&markerCount); err != nil {
		return fmt.Errorf("check %s marker: %w", markerKey, err)
	}
	if markerCount > 0 {
		return tx.Commit()
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM match_analytics_coverage`); err != nil {
		return fmt.Errorf("invalidate analytics coverage for %s: %w", markerKey, err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO app_metadata (key, value, updated_at)
		VALUES (?, 'complete', ?)
	`, markerKey, nowUTC()); err != nil {
		return fmt.Errorf("save %s marker: %w", markerKey, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit %s migration: %w", markerKey, err)
	}
	return nil
}

// prepareCardStatsBackfill invalidates existing analytics coverage exactly once
// after per-card game stats were introduced, so the next maintenance pass (or
// per-match EnsureMatchAnalytics) re-derives every match and populates
// game_card_stats from archived replays. The marker keeps this one-time.
func prepareCardStatsBackfill(ctx context.Context, conn dbConn) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin card stats backfill migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var markerCount int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM app_metadata WHERE key = ?
	`, cardStatsBackfillMetadataKey).Scan(&markerCount); err != nil {
		return fmt.Errorf("check card stats backfill marker: %w", err)
	}
	if markerCount > 0 {
		return tx.Commit()
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM match_analytics_coverage`); err != nil {
		return fmt.Errorf("invalidate analytics coverage for card stats backfill: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO app_metadata (key, value, updated_at)
		VALUES (?, 'complete', ?)
	`, cardStatsBackfillMetadataKey, nowUTC()); err != nil {
		return fmt.Errorf("save card stats backfill marker: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit card stats backfill migration: %w", err)
	}
	return nil
}

func backfillDeckVersions(ctx context.Context, conn dbConn) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin deck version backfill: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `
		SELECT id, COALESCE(source, ''), COALESCE(last_updated, created_at, '')
		FROM decks
		ORDER BY id
	`)
	if err != nil {
		return fmt.Errorf("list decks for version backfill: %w", err)
	}
	type deckSeed struct {
		id          int64
		source      string
		effectiveAt string
	}
	seeds := make([]deckSeed, 0)
	for rows.Next() {
		var seed deckSeed
		if err := rows.Scan(&seed.id, &seed.source, &seed.effectiveAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan deck version seed: %w", err)
		}
		seeds = append(seeds, seed)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate deck version seeds: %w", err)
	}
	rows.Close()

	for _, seed := range seeds {
		cardRows, err := tx.QueryContext(ctx, `
			SELECT section, card_id, quantity
			FROM deck_cards
			WHERE deck_id = ?
			ORDER BY section, card_id
		`, seed.id)
		if err != nil {
			return fmt.Errorf("list cards for deck version backfill: %w", err)
		}
		cards := make([]DeckCard, 0)
		for cardRows.Next() {
			var card DeckCard
			if err := cardRows.Scan(&card.Section, &card.CardID, &card.Quantity); err != nil {
				cardRows.Close()
				return fmt.Errorf("scan deck card for version backfill: %w", err)
			}
			cards = append(cards, card)
		}
		if err := cardRows.Err(); err != nil {
			cardRows.Close()
			return fmt.Errorf("iterate deck cards for version backfill: %w", err)
		}
		cardRows.Close()

		versionID, err := upsertDeckVersion(ctx, tx, seed.id, seed.source, seed.effectiveAt, cards)
		if err != nil {
			return err
		}
		if versionID > 0 {
			if _, err := tx.ExecContext(ctx, `
				UPDATE match_decks
				SET deck_version_id = ?
				WHERE deck_id = ? AND deck_version_id IS NULL
			`, versionID, seed.id); err != nil {
				return fmt.Errorf("backfill match deck version: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit deck version backfill: %w", err)
	}
	return nil
}
