package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/solean/ponder/internal/model"
)

// Player records in GRE updates replace their pool; an omitted manaPool means
// empty. Players absent from a diff retain their last observed pool.
func (s *Store) UpdateLiveManaPool(ctx context.Context, tx *sql.Tx, matchID string, game, seat, state int64, pool []model.ManaPoolEntry) error {
	raw, err := json.Marshal(pool)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
 INSERT INTO live_mana_pools (match_id, game_number, seat_id, game_state_id, pool_json)
 SELECT id, ?, ?, ?, ? FROM matches WHERE arena_match_id = ?
 ON CONFLICT(match_id, game_number, seat_id) DO UPDATE SET
 game_state_id = excluded.game_state_id, pool_json = excluded.pool_json
 WHERE excluded.game_state_id >= live_mana_pools.game_state_id
 `, game, seat, state, string(raw), matchID)
	if err != nil {
		return fmt.Errorf("update live mana pool: %w", err)
	}
	return nil
}

// GetLiveManaState reads just the latest game's controlled battlefield and pool
// in one SQLite snapshot. Ownership is intentionally not used for stolen lands.
func (s *Store) GetLiveManaState(ctx context.Context, matchID, game int64) (objects []model.MatchReplayFrameObjectRow, pool []model.ManaPoolEntry, available, poolKnown bool, err error) {
	rows, err := s.db.QueryContext(ctx, `
 WITH latest AS (
 SELECT f.id, f.game_state_id, m.player_seat_id FROM match_replay_frames f
 JOIN matches m ON m.id = f.match_id
 WHERE f.match_id = ? AND f.game_number = ? AND m.player_seat_id > 0
 ORDER BY COALESCE(f.game_state_id, 0) DESC, f.id DESC LIMIT 1
 )
 SELECT latest.id, o.instance_id, o.card_id, o.is_tapped,
 o.has_summoning_sickness, o.details_json, p.pool_json
 FROM latest
 LEFT JOIN match_replay_frame_objects o ON o.frame_id = latest.id
 AND o.zone_type = 'battlefield'
 AND COALESCE(o.controller_seat_id, o.owner_seat_id) = latest.player_seat_id
 LEFT JOIN live_mana_pools p ON p.match_id = ? AND p.game_number = ?
 AND p.seat_id = latest.player_seat_id AND p.game_state_id <= latest.game_state_id
 `, matchID, max(game, 1), matchID, max(game, 1))
	if err != nil {
		return nil, nil, false, false, fmt.Errorf("get live mana state: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var frameID int64
		var instance, card sql.NullInt64
		var tapped, sick sql.NullBool
		var details, rawPool sql.NullString
		if err = rows.Scan(&frameID, &instance, &card, &tapped, &sick, &details, &rawPool); err != nil {
			return
		}
		available = true
		if rawPool.Valid && !poolKnown {
			if err = json.Unmarshal([]byte(rawPool.String), &pool); err != nil {
				return
			}
			poolKnown = true
		}
		if instance.Valid {
			objects = append(objects, model.MatchReplayFrameObjectRow{InstanceID: instance.Int64, CardID: card.Int64,
				IsTapped: tapped.Bool, HasSummoningSickness: sick.Bool, DetailsJSON: details.String})
		}
	}
	err = rows.Err()
	return
}
