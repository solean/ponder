package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/cschnabel/mtgdata/internal/model"
)

func (s *Store) ReplaceMatchReplayFrame(
	ctx context.Context,
	tx *sql.Tx,
	arenaMatchID string,
	gameNumber, gameStateID, prevGameStateID, turnNumber int64,
	gameStateType, gameStage, phase, winningPlayerSide, winReason, recordedAt, source string,
	playerLifeTotalsJSON, actionsJSON, annotationsJSON []byte,
	objects []model.MatchReplayFrameObjectRow,
) (int64, error) {
	arenaMatchID = strings.TrimSpace(arenaMatchID)
	if arenaMatchID == "" || gameStateID <= 0 {
		return 0, nil
	}
	if gameNumber <= 0 {
		gameNumber = 1
	}

	actionsText := strings.TrimSpace(string(actionsJSON))
	annotationsText := strings.TrimSpace(string(annotationsJSON))
	playerLifeTotalsText := strings.TrimSpace(string(playerLifeTotalsJSON))

	_, err := tx.ExecContext(ctx, `
		INSERT INTO match_replay_frames (
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
			m.id, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
		FROM matches m
		WHERE m.arena_match_id = ?
		ON CONFLICT(match_id, game_number, game_state_id) DO UPDATE SET
			prev_game_state_id = COALESCE(excluded.prev_game_state_id, match_replay_frames.prev_game_state_id),
			game_state_type = COALESCE(excluded.game_state_type, match_replay_frames.game_state_type),
			game_stage = COALESCE(excluded.game_stage, match_replay_frames.game_stage),
			turn_number = COALESCE(excluded.turn_number, match_replay_frames.turn_number),
			phase = COALESCE(excluded.phase, match_replay_frames.phase),
			player_life_totals_json = COALESCE(excluded.player_life_totals_json, match_replay_frames.player_life_totals_json),
			winning_player_side = COALESCE(excluded.winning_player_side, match_replay_frames.winning_player_side),
			win_reason = COALESCE(excluded.win_reason, match_replay_frames.win_reason),
			source = COALESCE(excluded.source, match_replay_frames.source),
			recorded_at = COALESCE(excluded.recorded_at, match_replay_frames.recorded_at),
			actions_json = COALESCE(excluded.actions_json, match_replay_frames.actions_json),
			annotations_json = COALESCE(excluded.annotations_json, match_replay_frames.annotations_json)
	`,
		gameNumber,
		gameStateID,
		nullableInt(prevGameStateID),
		nullIfEmpty(strings.TrimSpace(gameStateType)),
		nullIfEmpty(strings.TrimSpace(gameStage)),
		nullableInt(turnNumber),
		nullIfEmpty(strings.TrimSpace(phase)),
		nullIfEmpty(playerLifeTotalsText),
		nullIfEmpty(strings.TrimSpace(winningPlayerSide)),
		nullIfEmpty(strings.TrimSpace(winReason)),
		nullIfEmpty(strings.TrimSpace(source)),
		nullIfEmpty(normalizeTS(recordedAt)),
		nullIfEmpty(actionsText),
		nullIfEmpty(annotationsText),
		nowUTC(),
		arenaMatchID,
	)
	if err != nil {
		return 0, fmt.Errorf("upsert match replay frame: %w", err)
	}

	var frameID int64
	err = tx.QueryRowContext(ctx, `
		SELECT f.id
		FROM match_replay_frames f
		JOIN matches m ON m.id = f.match_id
		WHERE m.arena_match_id = ?
			AND f.game_number = ?
			AND f.game_state_id = ?
		LIMIT 1
	`, arenaMatchID, gameNumber, gameStateID).Scan(&frameID)
	if err != nil {
		return 0, fmt.Errorf("lookup match replay frame: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM match_replay_frame_objects WHERE frame_id = ?`, frameID); err != nil {
		return 0, fmt.Errorf("clear match replay frame objects: %w", err)
	}

	for _, obj := range objects {
		if obj.InstanceID <= 0 || obj.CardID <= 0 || strings.TrimSpace(obj.ZoneType) == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO match_replay_frame_objects (
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
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			frameID,
			obj.InstanceID,
			obj.CardID,
			nullableReplayInt(obj.OwnerSeatID),
			nullableReplayInt(obj.ControllerSeatID),
			nullableReplayInt(obj.ZoneID),
			strings.TrimSpace(obj.ZoneType),
			nullableReplayInt(obj.ZonePosition),
			nullIfEmpty(strings.TrimSpace(obj.Visibility)),
			nullableReplayInt(obj.Power),
			nullableReplayInt(obj.Toughness),
			boolToInt(obj.IsTapped),
			boolToInt(obj.HasSummoningSickness),
			nullIfEmpty(strings.TrimSpace(obj.AttackState)),
			nullableReplayInt(obj.AttackTargetID),
			nullIfEmpty(strings.TrimSpace(obj.BlockState)),
			nullIfEmpty(strings.TrimSpace(obj.BlockAttackerIDsJSON)),
			nullIfEmpty(strings.TrimSpace(obj.CounterSummaryJSON)),
			nullIfEmpty(strings.TrimSpace(obj.DetailsJSON)),
			boolToInt(obj.IsToken),
			nowUTC(),
		); err != nil {
			return 0, fmt.Errorf("insert match replay frame object: %w", err)
		}
	}

	return frameID, nil
}

func (s *Store) LoadLatestMatchReplayFrameState(
	ctx context.Context,
	tx *sql.Tx,
	arenaMatchID string,
	gameNumber int64,
) (int64, int64, []model.MatchReplayFrameObjectRow, map[int64]int64, error) {
	arenaMatchID = strings.TrimSpace(arenaMatchID)
	if arenaMatchID == "" {
		return 0, 0, nil, nil, nil
	}
	if gameNumber <= 0 {
		gameNumber = 1
	}

	var (
		frameID             int64
		gameStateID         sql.NullInt64
		turnNumber          sql.NullInt64
		playerLifeTotalsRaw sql.NullString
	)
	err := tx.QueryRowContext(ctx, `
		SELECT f.id, f.game_state_id, f.turn_number, f.player_life_totals_json
		FROM match_replay_frames f
		JOIN matches m ON m.id = f.match_id
		WHERE m.arena_match_id = ?
			AND f.game_number = ?
		ORDER BY COALESCE(f.game_state_id, 0) DESC, f.id DESC
		LIMIT 1
	`, arenaMatchID, gameNumber).Scan(&frameID, &gameStateID, &turnNumber, &playerLifeTotalsRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, nil, nil, nil
	}
	if err != nil {
		return 0, 0, nil, nil, fmt.Errorf("lookup latest match replay frame: %w", err)
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT
			o.id,
			o.frame_id,
			o.instance_id,
			o.card_id,
			o.owner_seat_id,
			o.controller_seat_id,
			o.zone_id,
			COALESCE(o.zone_type, ''),
			o.zone_position,
			COALESCE(o.visibility, ''),
			o.power,
			o.toughness,
			o.is_tapped,
			o.has_summoning_sickness,
			COALESCE(o.attack_state, ''),
			o.attack_target_id,
			COALESCE(o.block_state, ''),
			COALESCE(o.block_attacker_ids_json, ''),
			COALESCE(o.counter_summary_json, ''),
			COALESCE(o.details_json, ''),
			o.is_token
		FROM match_replay_frame_objects o
		WHERE o.frame_id = ?
		ORDER BY COALESCE(o.zone_id, 0) ASC, COALESCE(o.zone_position, 1000000) ASC, o.instance_id ASC
	`, frameID)
	if err != nil {
		return 0, 0, nil, nil, fmt.Errorf("load latest match replay frame objects: %w", err)
	}
	defer rows.Close()

	objects := make([]model.MatchReplayFrameObjectRow, 0)
	for rows.Next() {
		var (
			row                  model.MatchReplayFrameObjectRow
			ownerSeatID          sql.NullInt64
			controllerSeatID     sql.NullInt64
			zoneID               sql.NullInt64
			zonePosition         sql.NullInt64
			power                sql.NullInt64
			toughness            sql.NullInt64
			attackTargetID       sql.NullInt64
			isTapped             int64
			hasSummoningSickness int64
			isToken              int64
		)
		if err := rows.Scan(
			&row.ID,
			&row.FrameID,
			&row.InstanceID,
			&row.CardID,
			&ownerSeatID,
			&controllerSeatID,
			&zoneID,
			&row.ZoneType,
			&zonePosition,
			&row.Visibility,
			&power,
			&toughness,
			&isTapped,
			&hasSummoningSickness,
			&row.AttackState,
			&attackTargetID,
			&row.BlockState,
			&row.BlockAttackerIDsJSON,
			&row.CounterSummaryJSON,
			&row.DetailsJSON,
			&isToken,
		); err != nil {
			return 0, 0, nil, nil, fmt.Errorf("scan latest match replay frame object: %w", err)
		}
		row.OwnerSeatID = replayPtrFromNullInt64(ownerSeatID)
		row.ControllerSeatID = replayPtrFromNullInt64(controllerSeatID)
		row.ZoneID = replayPtrFromNullInt64(zoneID)
		row.ZonePosition = replayPtrFromNullInt64(zonePosition)
		row.Power = replayPtrFromNullInt64(power)
		row.Toughness = replayPtrFromNullInt64(toughness)
		row.AttackTargetID = replayPtrFromNullInt64(attackTargetID)
		row.IsTapped = isTapped > 0
		row.HasSummoningSickness = hasSummoningSickness > 0
		row.IsToken = isToken > 0
		objects = append(objects, row)
	}
	if err := rows.Err(); err != nil {
		return 0, 0, nil, nil, fmt.Errorf("iterate latest match replay frame objects: %w", err)
	}

	return gameStateID.Int64, turnNumber.Int64, objects, parseReplayPlayerLifeTotalsJSON(playerLifeTotalsRaw.String), nil
}

func (s *Store) ListMatchReplayFrames(ctx context.Context, matchID int64) ([]model.MatchReplayFrameRow, error) {
	live, err := s.listLiveMatchReplayFrames(ctx, matchID)
	if err != nil {
		return nil, err
	}
	archived, err := s.loadArchivedMatchReplayFrames(ctx, matchID)
	if err != nil {
		return nil, err
	}

	frames := mergeReplayFrameRows(archived, live)
	populateReplayFrameChanges(frames)
	return frames, nil
}

// mergeReplayFrameRows combines archived frames with live rows for the same
// match. Live rows win on conflict because they reflect the most recent parse.
func mergeReplayFrameRows(archived, live []model.MatchReplayFrameRow) []model.MatchReplayFrameRow {
	if len(archived) == 0 {
		return live
	}
	if len(live) == 0 {
		return archived
	}

	liveKeys := make(map[string]bool, len(live))
	for i := range live {
		liveKeys[replayArchiveFrameKey(live[i].GameNumber, live[i].GameStateID)] = true
	}

	merged := make([]model.MatchReplayFrameRow, 0, len(archived)+len(live))
	for i := range archived {
		if liveKeys[replayArchiveFrameKey(archived[i].GameNumber, archived[i].GameStateID)] {
			continue
		}
		merged = append(merged, archived[i])
	}
	merged = append(merged, live...)
	sort.SliceStable(merged, func(i, j int) bool {
		gi, gj := int64(1), int64(1)
		if merged[i].GameNumber != nil && *merged[i].GameNumber > 0 {
			gi = *merged[i].GameNumber
		}
		if merged[j].GameNumber != nil && *merged[j].GameNumber > 0 {
			gj = *merged[j].GameNumber
		}
		if gi != gj {
			return gi < gj
		}
		si, sj := int64(0), int64(0)
		if merged[i].GameStateID != nil {
			si = *merged[i].GameStateID
		}
		if merged[j].GameStateID != nil {
			sj = *merged[j].GameStateID
		}
		if si != sj {
			return si < sj
		}
		return merged[i].ID < merged[j].ID
	})
	return merged
}

func (s *Store) listLiveMatchReplayFrames(ctx context.Context, matchID int64) ([]model.MatchReplayFrameRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			f.id,
			f.game_number,
			f.game_state_id,
			f.prev_game_state_id,
			COALESCE(f.game_state_type, ''),
			COALESCE(f.game_stage, ''),
			f.turn_number,
			COALESCE(f.phase, ''),
			COALESCE(f.player_life_totals_json, ''),
			COALESCE(f.winning_player_side, ''),
			COALESCE(f.win_reason, ''),
			m.player_seat_id,
			COALESCE(f.recorded_at, ''),
			COALESCE(f.actions_json, ''),
			COALESCE(f.annotations_json, '')
		FROM match_replay_frames f
		JOIN matches m ON m.id = f.match_id
		WHERE f.match_id = ?
		ORDER BY f.game_number ASC, COALESCE(f.game_state_id, 0) ASC, f.id ASC
	`, matchID)
	if err != nil {
		return nil, fmt.Errorf("list match replay frames: %w", err)
	}
	defer rows.Close()

	frames := make([]model.MatchReplayFrameRow, 0)
	frameIndexByID := make(map[int64]int)
	for rows.Next() {
		var (
			row             model.MatchReplayFrameRow
			gameNumber      sql.NullInt64
			gameStateID     sql.NullInt64
			prevGameStateID sql.NullInt64
			turnNumber      sql.NullInt64
			playerSeatID    sql.NullInt64
			playerLifeRaw   string
		)
		if err := rows.Scan(
			&row.ID,
			&gameNumber,
			&gameStateID,
			&prevGameStateID,
			&row.GameStateType,
			&row.GameStage,
			&turnNumber,
			&row.Phase,
			&playerLifeRaw,
			&row.WinningPlayerSide,
			&row.WinReason,
			&playerSeatID,
			&row.RecordedAt,
			&row.ActionsJSON,
			&row.AnnotationsJSON,
		); err != nil {
			return nil, fmt.Errorf("scan match replay frame: %w", err)
		}
		row.GameNumber = replayPtrFromNullInt64(gameNumber)
		row.GameStateID = replayPtrFromNullInt64(gameStateID)
		row.PrevGameStateID = replayPtrFromNullInt64(prevGameStateID)
		row.TurnNumber = replayPtrFromNullInt64(turnNumber)
		row.SelfLifeTotal, row.OpponentLifeTotal = replayFrameLifeTotals(
			parseReplayPlayerLifeTotalsJSON(playerLifeRaw),
			replayPtrFromNullInt64(playerSeatID),
		)
		frameIndexByID[row.ID] = len(frames)
		frames = append(frames, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate match replay frames: %w", err)
	}
	if len(frames) == 0 {
		return frames, nil
	}

	objectRows, err := s.db.QueryContext(ctx, `
		SELECT
			o.id,
			o.frame_id,
			o.instance_id,
			o.card_id,
			COALESCE(cc.name, ''),
			o.owner_seat_id,
			o.controller_seat_id,
			CASE
				WHEN m.player_seat_id IS NOT NULL AND COALESCE(o.controller_seat_id, o.owner_seat_id) = m.player_seat_id THEN 'self'
				WHEN COALESCE(o.controller_seat_id, o.owner_seat_id) IS NOT NULL AND COALESCE(o.controller_seat_id, o.owner_seat_id) > 0 THEN 'opponent'
				ELSE 'unknown'
			END AS player_side,
			o.zone_id,
			COALESCE(o.zone_type, ''),
			o.zone_position,
			COALESCE(o.visibility, ''),
			o.power,
			o.toughness,
			o.is_tapped,
			o.has_summoning_sickness,
			COALESCE(o.attack_state, ''),
			o.attack_target_id,
			COALESCE(o.block_state, ''),
			COALESCE(o.block_attacker_ids_json, ''),
			COALESCE(o.counter_summary_json, ''),
			COALESCE(o.details_json, ''),
			o.is_token
		FROM match_replay_frame_objects o
		JOIN match_replay_frames f ON f.id = o.frame_id
		JOIN matches m ON m.id = f.match_id
		LEFT JOIN card_catalog cc ON cc.arena_id = o.card_id
		WHERE f.match_id = ?
		ORDER BY
			f.game_number ASC,
			COALESCE(f.game_state_id, 0) ASC,
			f.id ASC,
			COALESCE(o.zone_id, 0) ASC,
			COALESCE(o.zone_position, 1000000) ASC,
			o.instance_id ASC
	`, matchID)
	if err != nil {
		return nil, fmt.Errorf("list match replay frame objects: %w", err)
	}
	defer objectRows.Close()

	for objectRows.Next() {
		var (
			row                  model.MatchReplayFrameObjectRow
			ownerSeatID          sql.NullInt64
			controllerSeatID     sql.NullInt64
			zoneID               sql.NullInt64
			zonePosition         sql.NullInt64
			power                sql.NullInt64
			toughness            sql.NullInt64
			attackTargetID       sql.NullInt64
			isTapped             int64
			hasSummoningSickness int64
			isToken              int64
		)
		if err := objectRows.Scan(
			&row.ID,
			&row.FrameID,
			&row.InstanceID,
			&row.CardID,
			&row.CardName,
			&ownerSeatID,
			&controllerSeatID,
			&row.PlayerSide,
			&zoneID,
			&row.ZoneType,
			&zonePosition,
			&row.Visibility,
			&power,
			&toughness,
			&isTapped,
			&hasSummoningSickness,
			&row.AttackState,
			&attackTargetID,
			&row.BlockState,
			&row.BlockAttackerIDsJSON,
			&row.CounterSummaryJSON,
			&row.DetailsJSON,
			&isToken,
		); err != nil {
			return nil, fmt.Errorf("scan match replay frame object: %w", err)
		}
		row.OwnerSeatID = replayPtrFromNullInt64(ownerSeatID)
		row.ControllerSeatID = replayPtrFromNullInt64(controllerSeatID)
		row.ZoneID = replayPtrFromNullInt64(zoneID)
		row.ZonePosition = replayPtrFromNullInt64(zonePosition)
		row.Power = replayPtrFromNullInt64(power)
		row.Toughness = replayPtrFromNullInt64(toughness)
		row.AttackTargetID = replayPtrFromNullInt64(attackTargetID)
		row.IsTapped = isTapped > 0
		row.HasSummoningSickness = hasSummoningSickness > 0
		row.IsToken = isToken > 0

		index, ok := frameIndexByID[row.FrameID]
		if !ok {
			continue
		}
		frames[index].Objects = append(frames[index].Objects, row)
	}
	if err := objectRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate match replay frame objects: %w", err)
	}

	return frames, nil
}

func nullableReplayInt(v *int64) any {
	if v == nil {
		return nil
	}
	return nullableInt(*v)
}

func replayPtrFromNullInt64(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	out := v.Int64
	return &out
}

func parseReplayPlayerLifeTotalsJSON(raw string) map[int64]int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	parsed := make(map[string]int64)
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil
	}

	out := make(map[int64]int64, len(parsed))
	for key, lifeTotal := range parsed {
		seatID, err := strconv.ParseInt(strings.TrimSpace(key), 10, 64)
		if err != nil || seatID <= 0 {
			continue
		}
		out[seatID] = lifeTotal
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func replayFrameLifeTotals(lifeTotals map[int64]int64, selfSeatID *int64) (*int64, *int64) {
	if len(lifeTotals) == 0 {
		return nil, nil
	}

	var selfLifeTotal *int64
	if selfSeatID != nil && *selfSeatID > 0 {
		if value, ok := lifeTotals[*selfSeatID]; ok {
			out := value
			selfLifeTotal = &out
		}
	}

	var opponentLifeTotal *int64
	seatIDs := make([]int, 0, len(lifeTotals))
	for seatID := range lifeTotals {
		seatIDs = append(seatIDs, int(seatID))
	}
	sort.Ints(seatIDs)
	for _, rawSeatID := range seatIDs {
		seatID := int64(rawSeatID)
		if selfSeatID != nil && seatID == *selfSeatID {
			continue
		}
		value := lifeTotals[seatID]
		out := value
		opponentLifeTotal = &out
		break
	}

	return selfLifeTotal, opponentLifeTotal
}

func populateReplayFrameChanges(frames []model.MatchReplayFrameRow) {
	if len(frames) == 0 {
		return
	}

	var (
		prevGameNumber int64
		prevObjects    map[int64]model.MatchReplayFrameObjectRow
		havePrev       bool
	)

	for i := range frames {
		gameNumber := int64(1)
		if frames[i].GameNumber != nil && *frames[i].GameNumber > 0 {
			gameNumber = *frames[i].GameNumber
		}
		if !havePrev || gameNumber != prevGameNumber {
			prevObjects = nil
			havePrev = true
			prevGameNumber = gameNumber
		}

		currentObjects := make(map[int64]model.MatchReplayFrameObjectRow, len(frames[i].Objects))
		for _, obj := range frames[i].Objects {
			currentObjects[obj.InstanceID] = obj
		}

		if prevObjects != nil {
			for _, obj := range frames[i].Objects {
				prev, ok := prevObjects[obj.InstanceID]
				if !ok {
					frames[i].Changes = append(frames[i].Changes, model.MatchReplayChangeRow{
						InstanceID:     obj.InstanceID,
						CardID:         obj.CardID,
						CardName:       obj.CardName,
						OwnerSeatID:    obj.OwnerSeatID,
						PlayerSide:     obj.PlayerSide,
						Action:         "enter_public",
						ToZoneID:       obj.ZoneID,
						ToZoneType:     obj.ZoneType,
						ToZonePosition: obj.ZonePosition,
						IsToken:        obj.IsToken,
					})
					continue
				}
				if !sameReplayZone(prev, obj) {
					frames[i].Changes = append(frames[i].Changes, model.MatchReplayChangeRow{
						InstanceID:       obj.InstanceID,
						CardID:           obj.CardID,
						CardName:         obj.CardName,
						OwnerSeatID:      obj.OwnerSeatID,
						PlayerSide:       obj.PlayerSide,
						Action:           "move_public",
						FromZoneID:       prev.ZoneID,
						FromZoneType:     prev.ZoneType,
						FromZonePosition: prev.ZonePosition,
						ToZoneID:         obj.ZoneID,
						ToZoneType:       obj.ZoneType,
						ToZonePosition:   obj.ZonePosition,
						IsToken:          obj.IsToken,
					})
				}
				if !sameReplayIntPtr(prev.ControllerSeatID, obj.ControllerSeatID) {
					frames[i].Changes = append(frames[i].Changes, replayStateChangeRow(obj, "controller_change"))
				}
				if prev.IsTapped != obj.IsTapped {
					action := "untap"
					if obj.IsTapped {
						action = "tap"
					}
					frames[i].Changes = append(frames[i].Changes, replayStateChangeRow(obj, action))
				}
				if !sameReplayCombatState(prev, obj) {
					// Only role transitions are narrated; substate shifts on a
					// creature already in combat (declared -> attacking, or an
					// attacker becoming blocked/unblocked) are bookkeeping.
					action := ""
					switch {
					case replayIsBlocking(obj) && !replayIsBlocking(prev):
						action = "block"
					case replayIsAttacking(obj) && !replayIsAttacking(prev):
						action = "attack"
					case replayIsBlocking(prev) && !replayIsBlocking(obj):
						action = "stop_block"
					case replayIsAttacking(prev) && !replayIsAttacking(obj):
						action = "stop_attack"
					}
					if action != "" {
						frames[i].Changes = append(frames[i].Changes, replayStateChangeRow(obj, action))
					}
				}
				if prev.HasSummoningSickness != obj.HasSummoningSickness {
					frames[i].Changes = append(frames[i].Changes, replayStateChangeRow(obj, "summoning_sickness_change"))
				}
				if !sameReplayIntPtr(prev.Power, obj.Power) || !sameReplayIntPtr(prev.Toughness, obj.Toughness) {
					frames[i].Changes = append(frames[i].Changes, replayStateChangeRow(obj, "stat_change"))
				}
				if !sameReplayText(prev.CounterSummaryJSON, obj.CounterSummaryJSON) {
					frames[i].Changes = append(frames[i].Changes, replayStateChangeRow(obj, "counters_change"))
				}
			}
			departedIDs := make([]int64, 0, len(prevObjects))
			for instanceID := range prevObjects {
				if _, ok := currentObjects[instanceID]; !ok {
					departedIDs = append(departedIDs, instanceID)
				}
			}
			sort.Slice(departedIDs, func(a, b int) bool { return departedIDs[a] < departedIDs[b] })
			for _, instanceID := range departedIDs {
				obj := prevObjects[instanceID]
				frames[i].Changes = append(frames[i].Changes, model.MatchReplayChangeRow{
					InstanceID:       obj.InstanceID,
					CardID:           obj.CardID,
					CardName:         obj.CardName,
					OwnerSeatID:      obj.OwnerSeatID,
					PlayerSide:       obj.PlayerSide,
					Action:           "leave_public",
					FromZoneID:       obj.ZoneID,
					FromZoneType:     obj.ZoneType,
					FromZonePosition: obj.ZonePosition,
					IsToken:          obj.IsToken,
				})
			}
		}

		prevObjects = currentObjects
	}
}

func sameReplayZone(a, b model.MatchReplayFrameObjectRow) bool {
	return sameReplayIntPtr(a.ZoneID, b.ZoneID) &&
		strings.EqualFold(strings.TrimSpace(a.ZoneType), strings.TrimSpace(b.ZoneType)) &&
		sameReplayIntPtr(a.ZonePosition, b.ZonePosition)
}

func sameReplayCombatState(a, b model.MatchReplayFrameObjectRow) bool {
	return sameReplayText(a.AttackState, b.AttackState) &&
		sameReplayIntPtr(a.AttackTargetID, b.AttackTargetID) &&
		sameReplayText(a.BlockState, b.BlockState) &&
		sameReplayText(a.BlockAttackerIDsJSON, b.BlockAttackerIDsJSON)
}

func replayIsAttacking(object model.MatchReplayFrameObjectRow) bool {
	return strings.TrimSpace(object.AttackState) != ""
}

func replayIsBlocking(object model.MatchReplayFrameObjectRow) bool {
	// Attackers also carry a blockState ("blocked"/"unblocked" once blockers
	// are declared); only "declared"/"blocking" mark an actual blocker.
	state := strings.ToLower(strings.TrimSpace(object.BlockState))
	return state == "blocking" || state == "declared"
}

func replayStateChangeRow(object model.MatchReplayFrameObjectRow, action string) model.MatchReplayChangeRow {
	return model.MatchReplayChangeRow{
		InstanceID:     object.InstanceID,
		CardID:         object.CardID,
		CardName:       object.CardName,
		OwnerSeatID:    object.OwnerSeatID,
		PlayerSide:     object.PlayerSide,
		Action:         action,
		ToZoneID:       object.ZoneID,
		ToZoneType:     object.ZoneType,
		ToZonePosition: object.ZonePosition,
		IsToken:        object.IsToken,
	}
}

func sameReplayIntPtr(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func sameReplayText(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}
