package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/solean/ponder/internal/model"
)

func migrateSpellCopyColumns(ctx context.Context, conn dbConn) error {
	for _, table := range []string{"match_replay_frame_objects", "match_card_plays"} {
		for _, column := range []string{"is_copy", "cast_confirmed"} {
			has, err := tableHasColumn(ctx, conn, table, column)
			if err != nil {
				return err
			}
			if !has {
				if _, err := conn.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+column+` INTEGER NOT NULL DEFAULT 0`); err != nil {
					return fmt.Errorf("add %s.%s: %w", table, column, err)
				}
			}
		}
	}
	// Legacy archives retain isCopy in detailsJson and cast evidence in frame
	// annotations. RefreshMatchAnalytics reconciles those facts before deriving.
	return invalidateAnalyticsCoverageOnce(ctx, conn, "spell_copies_backfill_v1")
}

// MarkMatchCardPlayCopy preserves the timeline but removes physical-card evidence,
// including observations recorded before copy identity arrived.
func (s *Store) MarkMatchCardPlayCopy(ctx context.Context, tx *sql.Tx, arenaMatchID string, gameNumber, instanceID int64, castConfirmed bool) error {
	var matchID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM matches WHERE arena_match_id = ?`, arenaMatchID).Scan(&matchID); err != nil {
		return err
	}
	return markMatchCardPlayCopy(ctx, tx, matchID, gameNumber, instanceID, castConfirmed)
}

func markMatchCardPlayCopy(ctx context.Context, tx *sql.Tx, matchID, gameNumber, instanceID int64, castConfirmed bool) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE match_card_plays SET is_copy = 1, cast_confirmed = MAX(cast_confirmed, ?)
		WHERE match_id = ? AND game_number = ? AND instance_id = ?
	`, boolToInt(castConfirmed), matchID, gameNumber, instanceID); err != nil {
		return fmt.Errorf("mark copy timeline observation: %w", err)
	}
	// A match page may have derived counts before a sparse diff revealed this
	// was a copy. Do not let that cached count resurrect it in live reads.
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM match_opponent_card_counts WHERE match_id = ? AND card_id IN (
			SELECT card_id FROM match_card_plays WHERE match_id = ? AND game_number = ? AND instance_id = ?
			UNION
			SELECT o.card_id FROM match_replay_frame_objects o
			JOIN match_replay_frames f ON f.id = o.frame_id
			WHERE f.match_id = ? AND f.game_number = ? AND o.instance_id = ?
		)
	`, matchID, matchID, gameNumber, instanceID, matchID, gameNumber, instanceID); err != nil {
		return fmt.Errorf("invalidate copied card count: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM match_analytics_coverage WHERE match_id = ?`, matchID); err != nil {
		return fmt.Errorf("invalidate copy analytics: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM match_opponent_card_instances
		WHERE match_id = ? AND game_number = ? AND instance_id = ?
	`, matchID, gameNumber, instanceID); err != nil {
		return fmt.Errorf("remove copy from opponent observations: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE match_replay_frame_objects SET is_copy = 1, cast_confirmed = MAX(cast_confirmed, ?)
		WHERE instance_id = ? AND frame_id IN (
			SELECT id FROM match_replay_frames WHERE match_id = ? AND game_number = ?
		) AND (is_copy = 0 OR cast_confirmed < ?)
	`, boolToInt(castConfirmed), instanceID, matchID, gameNumber, boolToInt(castConfirmed)); err != nil {
		return fmt.Errorf("backfill replay copy flags: %w", err)
	}
	return nil
}

// reconcileMatchCopyFacts repairs historical observations from retained replay
// evidence. Unknown legacy objects remain unchanged. The caller holds the writer
// slot; this runs before analytics reads the card-play tables.
func (s *Store) reconcileMatchCopyFacts(ctx context.Context, matchID int64, frames []model.MatchReplayFrameRow) error {
	type key struct{ game, instance int64 }
	copies := make(map[key]bool)
	for _, frame := range frames {
		game := int64(1)
		if frame.GameNumber != nil {
			game = *frame.GameNumber
		}
		for _, object := range frame.Objects {
			if object.IsCopy {
				k := key{game, object.InstanceID}
				copies[k] = copies[k] || object.CastConfirmed
			}
		}
	}
	if len(copies) == 0 {
		return nil
	}
	tx, err := s.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for k, cast := range copies {
		if err := markMatchCardPlayCopy(ctx, tx, matchID, k.game, k.instance, cast); err != nil {
			return err
		}
	}
	archived, found, err := loadReplayArchivePayload(ctx, tx, matchID)
	if err != nil && !strings.Contains(err.Error(), "decode match replay archive") {
		return fmt.Errorf("load archive for copy backfill: %w", err)
	}
	changed := false
	for i := range archived {
		game := int64(1)
		if archived[i].GameNumber != nil {
			game = *archived[i].GameNumber
		}
		for j := range archived[i].Objects {
			object := &archived[i].Objects[j]
			if cast, copy := copies[key{game, object.InstanceID}]; copy && (!object.IsCopy || (cast && !object.CastConfirmed)) {
				object.IsCopy, object.CastConfirmed = true, object.CastConfirmed || cast
				changed = true
			}
		}
	}
	if found && changed {
		raw, err := json.Marshal(replayArchivePayload{SchemaVersion: replayArchiveSchemaVersion, Frames: archived})
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE match_replay_archives SET payload_zstd = ?, updated_at = ? WHERE match_id = ?`, getZstdEncoder().EncodeAll(raw, nil), nowUTC(), matchID); err != nil {
			return fmt.Errorf("backfill archived copy flags: %w", err)
		}
	}
	return tx.Commit()
}
