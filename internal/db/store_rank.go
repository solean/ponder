package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/solean/ponder/internal/model"
)

type MatchRankSnapshot struct {
	ObservedAt                  string
	PayloadJSON                 string
	ConstructedSeasonOrdinal    *int64
	ConstructedRankClass        string
	ConstructedLevel            *int64
	ConstructedStep             *int64
	ConstructedPercentile       *float64
	ConstructedLeaderboardPlace *int64
	ConstructedMatchesWon       *int64
	ConstructedMatchesLost      *int64
	LimitedSeasonOrdinal        *int64
	LimitedRankClass            string
	LimitedLevel                *int64
	LimitedStep                 *int64
	LimitedPercentile           *float64
	LimitedLeaderboardPlace     *int64
	LimitedMatchesWon           *int64
	LimitedMatchesLost          *int64
}

func (s *Store) MatchHasRankSnapshot(ctx context.Context, tx *sql.Tx, arenaMatchID string) (bool, error) {
	arenaMatchID = strings.TrimSpace(arenaMatchID)
	if arenaMatchID == "" {
		return false, nil
	}

	err := tx.QueryRowContext(ctx, `
		SELECT 1
		FROM matches m
		JOIN match_rank_snapshots mrs ON mrs.match_id = m.id
		WHERE m.arena_match_id = ?
		LIMIT 1
	`, arenaMatchID).Scan(new(int64))
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check match rank snapshot: %w", err)
	}
	return true, nil
}

func (s *Store) UpsertMatchRankSnapshot(ctx context.Context, tx *sql.Tx, arenaMatchID string, snapshot MatchRankSnapshot) error {
	arenaMatchID = strings.TrimSpace(arenaMatchID)
	if arenaMatchID == "" {
		return nil
	}

	var matchID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM matches
		WHERE arena_match_id = ?
	`, arenaMatchID).Scan(&matchID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("lookup rank snapshot match: %w", err)
	}

	err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM match_rank_snapshots
		WHERE match_id = ?
	`, matchID).Scan(new(int64))
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("lookup existing rank snapshot: %w", err)
	}

	prevSnapshotID := any(nil)
	if errors.Is(err, sql.ErrNoRows) {
		var prevID int64
		err = tx.QueryRowContext(ctx, `
			SELECT id
			FROM match_rank_snapshots
			ORDER BY COALESCE(observed_at, created_at) DESC, id DESC
			LIMIT 1
		`).Scan(&prevID)
		if err == nil {
			prevSnapshotID = prevID
		} else if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("lookup previous rank snapshot: %w", err)
		}
	}

	snapshot.ObservedAt = normalizeTS(snapshot.ObservedAt)
	now := nowUTC()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO match_rank_snapshots (
			match_id,
			prev_snapshot_id,
			observed_at,
			payload_json,
			constructed_season_ordinal,
			constructed_rank_class,
			constructed_level,
			constructed_step,
			constructed_percentile,
			constructed_leaderboard_place,
			constructed_matches_won,
			constructed_matches_lost,
			limited_season_ordinal,
			limited_rank_class,
			limited_level,
			limited_step,
			limited_percentile,
			limited_leaderboard_place,
			limited_matches_won,
			limited_matches_lost,
			created_at,
			updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(match_id) DO UPDATE SET
			prev_snapshot_id = COALESCE(match_rank_snapshots.prev_snapshot_id, excluded.prev_snapshot_id),
			observed_at = COALESCE(excluded.observed_at, match_rank_snapshots.observed_at),
			payload_json = excluded.payload_json,
			constructed_season_ordinal = COALESCE(excluded.constructed_season_ordinal, match_rank_snapshots.constructed_season_ordinal),
			constructed_rank_class = COALESCE(excluded.constructed_rank_class, match_rank_snapshots.constructed_rank_class),
			constructed_level = COALESCE(excluded.constructed_level, match_rank_snapshots.constructed_level),
			constructed_step = COALESCE(excluded.constructed_step, match_rank_snapshots.constructed_step),
			constructed_percentile = COALESCE(excluded.constructed_percentile, match_rank_snapshots.constructed_percentile),
			constructed_leaderboard_place = COALESCE(excluded.constructed_leaderboard_place, match_rank_snapshots.constructed_leaderboard_place),
			constructed_matches_won = COALESCE(excluded.constructed_matches_won, match_rank_snapshots.constructed_matches_won),
			constructed_matches_lost = COALESCE(excluded.constructed_matches_lost, match_rank_snapshots.constructed_matches_lost),
			limited_season_ordinal = COALESCE(excluded.limited_season_ordinal, match_rank_snapshots.limited_season_ordinal),
			limited_rank_class = COALESCE(excluded.limited_rank_class, match_rank_snapshots.limited_rank_class),
			limited_level = COALESCE(excluded.limited_level, match_rank_snapshots.limited_level),
			limited_step = COALESCE(excluded.limited_step, match_rank_snapshots.limited_step),
			limited_percentile = COALESCE(excluded.limited_percentile, match_rank_snapshots.limited_percentile),
			limited_leaderboard_place = COALESCE(excluded.limited_leaderboard_place, match_rank_snapshots.limited_leaderboard_place),
			limited_matches_won = COALESCE(excluded.limited_matches_won, match_rank_snapshots.limited_matches_won),
			limited_matches_lost = COALESCE(excluded.limited_matches_lost, match_rank_snapshots.limited_matches_lost),
			updated_at = excluded.updated_at
	`, matchID, prevSnapshotID, nullIfEmpty(snapshot.ObservedAt), snapshot.PayloadJSON,
		nullableIntPtr(snapshot.ConstructedSeasonOrdinal), nullIfEmpty(snapshot.ConstructedRankClass),
		nullableIntPtr(snapshot.ConstructedLevel), nullableIntPtr(snapshot.ConstructedStep),
		snapshot.ConstructedPercentile, nullableIntPtr(snapshot.ConstructedLeaderboardPlace),
		nullableIntPtr(snapshot.ConstructedMatchesWon), nullableIntPtr(snapshot.ConstructedMatchesLost),
		nullableIntPtr(snapshot.LimitedSeasonOrdinal), nullIfEmpty(snapshot.LimitedRankClass),
		nullableIntPtr(snapshot.LimitedLevel), nullableIntPtr(snapshot.LimitedStep),
		snapshot.LimitedPercentile, nullableIntPtr(snapshot.LimitedLeaderboardPlace),
		nullableIntPtr(snapshot.LimitedMatchesWon), nullableIntPtr(snapshot.LimitedMatchesLost),
		now, now)
	if err != nil {
		return fmt.Errorf("upsert match rank snapshot: %w", err)
	}

	return nil
}

func (s *Store) ListRankHistory(ctx context.Context) ([]model.RankHistoryPoint, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			m.id,
			m.arena_match_id,
			COALESCE(m.event_name, ''),
			COALESCE(m.opponent_name, ''),
			COALESCE(m.result, 'unknown'),
			COALESCE(mrs.observed_at, ''),
			COALESCE(m.ended_at, ''),
			COALESCE(m.format, ''),
			m.seconds_count,
			md.deck_id,
			COALESCE(d.name, ''),
			mrs.constructed_season_ordinal,
			COALESCE(mrs.constructed_rank_class, ''),
			mrs.constructed_level,
			mrs.constructed_step,
			mrs.constructed_percentile,
			mrs.constructed_leaderboard_place,
			mrs.constructed_matches_won,
			mrs.constructed_matches_lost,
			mrs.limited_season_ordinal,
			COALESCE(mrs.limited_rank_class, ''),
			mrs.limited_level,
			mrs.limited_step,
			mrs.limited_percentile,
			mrs.limited_leaderboard_place,
			mrs.limited_matches_won,
			mrs.limited_matches_lost
		FROM match_rank_snapshots mrs
		JOIN matches m ON m.id = mrs.match_id
		LEFT JOIN (
			SELECT match_id, MIN(deck_id) AS deck_id
			FROM match_decks
			GROUP BY match_id
		) md ON md.match_id = m.id
		LEFT JOIN decks d ON d.id = md.deck_id
		ORDER BY COALESCE(mrs.observed_at, m.ended_at, m.started_at, m.updated_at) ASC, mrs.id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list rank history: %w", err)
	}
	defer rows.Close()

	var out []model.RankHistoryPoint
	for rows.Next() {
		var row model.RankHistoryPoint
		var secondsCount sql.NullInt64
		var deckID sql.NullInt64
		var constructedSeasonOrdinal sql.NullInt64
		var constructedLevel sql.NullInt64
		var constructedStep sql.NullInt64
		var constructedPercentile sql.NullFloat64
		var constructedLeaderboard sql.NullInt64
		var constructedMatchesWon sql.NullInt64
		var constructedMatchesLost sql.NullInt64
		var limitedSeasonOrdinal sql.NullInt64
		var limitedLevel sql.NullInt64
		var limitedStep sql.NullInt64
		var limitedPercentile sql.NullFloat64
		var limitedLeaderboard sql.NullInt64
		var limitedMatchesWon sql.NullInt64
		var limitedMatchesLost sql.NullInt64

		if err := rows.Scan(
			&row.MatchID,
			&row.ArenaMatchID,
			&row.EventName,
			&row.Opponent,
			&row.Result,
			&row.ObservedAt,
			&row.EndedAt,
			&row.Format,
			&secondsCount,
			&deckID,
			&row.DeckName,
			&constructedSeasonOrdinal,
			&row.Constructed.RankClass,
			&constructedLevel,
			&constructedStep,
			&constructedPercentile,
			&constructedLeaderboard,
			&constructedMatchesWon,
			&constructedMatchesLost,
			&limitedSeasonOrdinal,
			&row.Limited.RankClass,
			&limitedLevel,
			&limitedStep,
			&limitedPercentile,
			&limitedLeaderboard,
			&limitedMatchesWon,
			&limitedMatchesLost,
		); err != nil {
			return nil, fmt.Errorf("scan rank history row: %w", err)
		}

		row.SecondsCount = nullInt64Ptr(secondsCount)
		row.DeckID = nullInt64Ptr(deckID)
		row.Constructed.SeasonOrdinal = nullInt64Ptr(constructedSeasonOrdinal)
		row.Constructed.Level = nullInt64Ptr(constructedLevel)
		row.Constructed.Step = nullInt64Ptr(constructedStep)
		row.Constructed.Percentile = nullableFloat(constructedPercentile)
		row.Constructed.LeaderboardPlace = nullInt64Ptr(constructedLeaderboard)
		row.Constructed.MatchesWon = nullInt64Ptr(constructedMatchesWon)
		row.Constructed.MatchesLost = nullInt64Ptr(constructedMatchesLost)

		row.Limited.SeasonOrdinal = nullInt64Ptr(limitedSeasonOrdinal)
		row.Limited.Level = nullInt64Ptr(limitedLevel)
		row.Limited.Step = nullInt64Ptr(limitedStep)
		row.Limited.Percentile = nullableFloat(limitedPercentile)
		row.Limited.LeaderboardPlace = nullInt64Ptr(limitedLeaderboard)
		row.Limited.MatchesWon = nullInt64Ptr(limitedMatchesWon)
		row.Limited.MatchesLost = nullInt64Ptr(limitedMatchesLost)

		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rank history: %w", err)
	}

	return out, nil
}

func migrateRankSnapshots(ctx context.Context, conn dbConn) error {
	columns := []struct {
		name string
		ddl  string
	}{
		{"constructed_percentile", `ALTER TABLE match_rank_snapshots ADD COLUMN constructed_percentile REAL`},
		{"constructed_leaderboard_place", `ALTER TABLE match_rank_snapshots ADD COLUMN constructed_leaderboard_place INTEGER`},
		{"limited_percentile", `ALTER TABLE match_rank_snapshots ADD COLUMN limited_percentile REAL`},
		{"limited_leaderboard_place", `ALTER TABLE match_rank_snapshots ADD COLUMN limited_leaderboard_place INTEGER`},
	}
	for _, column := range columns {
		exists, err := tableHasColumn(ctx, conn, "match_rank_snapshots", column.name)
		if err != nil {
			return fmt.Errorf("inspect match rank snapshot %s schema: %w", column.name, err)
		}
		if exists {
			continue
		}
		if _, err := conn.ExecContext(ctx, column.ddl); err != nil {
			return fmt.Errorf("add match rank snapshot %s: %w", column.name, err)
		}
	}

	if _, err := conn.ExecContext(ctx, `
		UPDATE match_rank_snapshots
		SET
			constructed_percentile = COALESCE(
				constructed_percentile,
				CASE WHEN json_valid(payload_json)
					THEN CAST(json_extract(payload_json, '$.constructedPercentile') AS REAL)
				END
			),
			constructed_leaderboard_place = COALESCE(
				constructed_leaderboard_place,
				CASE WHEN json_valid(payload_json)
					THEN CAST(json_extract(payload_json, '$.constructedLeaderboardPlace') AS INTEGER)
				END
			),
			limited_percentile = COALESCE(
				limited_percentile,
				CASE WHEN json_valid(payload_json)
					THEN CAST(json_extract(payload_json, '$.limitedPercentile') AS REAL)
				END
			),
			limited_leaderboard_place = COALESCE(
				limited_leaderboard_place,
				CASE WHEN json_valid(payload_json)
					THEN CAST(json_extract(payload_json, '$.limitedLeaderboardPlace') AS INTEGER)
				END
			)
		WHERE constructed_percentile IS NULL
			OR constructed_leaderboard_place IS NULL
			OR limited_percentile IS NULL
			OR limited_leaderboard_place IS NULL
	`); err != nil {
		return fmt.Errorf("backfill match rank Mythic standing: %w", err)
	}
	return nil
}
