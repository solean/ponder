package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/cschnabel/mtgdata/internal/model"
)

// Replay frames are written as relational rows while a match is live so the
// parser can resume incrementally, then compacted into a single
// zstd-compressed JSON blob per match once the match completes. Consecutive
// frames are nearly identical, so whole-match compression reclaims an order
// of magnitude of space compared to the row form.

const replayArchiveSchemaVersion = 1

// staleness cutoff for matches that never received an end-of-match event;
// their replay rows are still safe to compact because reads merge the
// archive with any rows written afterwards.
const replayArchiveStaleMatchAge = 48 * time.Hour

type replayArchivePayload struct {
	SchemaVersion int64                `json:"schemaVersion"`
	Frames        []replayArchiveFrame `json:"frames"`
}

type replayArchiveFrame struct {
	ID                   int64                 `json:"id"`
	GameNumber           *int64                `json:"gameNumber,omitempty"`
	GameStateID          *int64                `json:"gameStateId,omitempty"`
	PrevGameStateID      *int64                `json:"prevGameStateId,omitempty"`
	GameStateType        string                `json:"gameStateType,omitempty"`
	GameStage            string                `json:"gameStage,omitempty"`
	TurnNumber           *int64                `json:"turnNumber,omitempty"`
	Phase                string                `json:"phase,omitempty"`
	PlayerLifeTotalsJSON string                `json:"playerLifeTotalsJson,omitempty"`
	WinningPlayerSide    string                `json:"winningPlayerSide,omitempty"`
	WinReason            string                `json:"winReason,omitempty"`
	Source               string                `json:"source,omitempty"`
	RecordedAt           string                `json:"recordedAt,omitempty"`
	ActionsJSON          string                `json:"actionsJson,omitempty"`
	AnnotationsJSON      string                `json:"annotationsJson,omitempty"`
	CreatedAt            string                `json:"createdAt,omitempty"`
	Objects              []replayArchiveObject `json:"objects,omitempty"`
}

type replayArchiveObject struct {
	ID                   int64  `json:"id"`
	InstanceID           int64  `json:"instanceId"`
	CardID               int64  `json:"cardId"`
	OwnerSeatID          *int64 `json:"ownerSeatId,omitempty"`
	ControllerSeatID     *int64 `json:"controllerSeatId,omitempty"`
	ZoneID               *int64 `json:"zoneId,omitempty"`
	ZoneType             string `json:"zoneType"`
	ZonePosition         *int64 `json:"zonePosition,omitempty"`
	Visibility           string `json:"visibility,omitempty"`
	Power                *int64 `json:"power,omitempty"`
	Toughness            *int64 `json:"toughness,omitempty"`
	IsTapped             bool   `json:"isTapped,omitempty"`
	HasSummoningSickness bool   `json:"hasSummoningSickness,omitempty"`
	AttackState          string `json:"attackState,omitempty"`
	AttackTargetID       *int64 `json:"attackTargetId,omitempty"`
	BlockState           string `json:"blockState,omitempty"`
	BlockAttackerIDsJSON string `json:"blockAttackerIdsJson,omitempty"`
	CounterSummaryJSON   string `json:"counterSummaryJson,omitempty"`
	DetailsJSON          string `json:"detailsJson,omitempty"`
	IsToken              bool   `json:"isToken,omitempty"`
}

var (
	zstdEncoderOnce sync.Once
	zstdEncoder     *zstd.Encoder
	zstdDecoderOnce sync.Once
	zstdDecoder     *zstd.Decoder
)

func getZstdEncoder() *zstd.Encoder {
	zstdEncoderOnce.Do(func() {
		// Archives are written once per completed match, so encode speed is
		// irrelevant; best level measured ~30% smaller than BetterCompression.
		enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedBestCompression))
		if err != nil {
			panic(fmt.Sprintf("init zstd encoder: %v", err))
		}
		zstdEncoder = enc
	})
	return zstdEncoder
}

func getZstdDecoder() *zstd.Decoder {
	zstdDecoderOnce.Do(func() {
		dec, err := zstd.NewReader(nil, zstd.WithDecoderConcurrency(0))
		if err != nil {
			panic(fmt.Sprintf("init zstd decoder: %v", err))
		}
		zstdDecoder = dec
	})
	return zstdDecoder
}

type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// ArchiveMatchReplay compacts the replay frame rows of a completed match into
// a compressed archive blob and removes the rows. It merges with any existing
// archive for the match (newer rows win), so it is safe to call repeatedly
// and at any point in the match lifecycle. Returns true when an archive was
// written.
func (s *Store) ArchiveMatchReplay(ctx context.Context, tx *sql.Tx, arenaMatchID string) (bool, error) {
	arenaMatchID = strings.TrimSpace(arenaMatchID)
	if arenaMatchID == "" {
		return false, nil
	}

	var matchID int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM matches WHERE arena_match_id = ?`, arenaMatchID).Scan(&matchID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("lookup match for replay archive: %w", err)
	}

	return s.archiveMatchReplayRows(ctx, tx, matchID)
}

func (s *Store) archiveMatchReplayRows(ctx context.Context, q querier, matchID int64) (bool, error) {
	frames, err := loadReplayArchiveRowFrames(ctx, q, matchID)
	if err != nil {
		return false, err
	}
	if len(frames) == 0 {
		return false, nil
	}

	existing, _, err := loadReplayArchivePayload(ctx, q, matchID)
	if err != nil {
		return false, err
	}
	frames = mergeReplayArchiveFrames(existing, frames)

	payload := replayArchivePayload{
		SchemaVersion: replayArchiveSchemaVersion,
		Frames:        frames,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return false, fmt.Errorf("marshal replay archive payload: %w", err)
	}
	compressed := getZstdEncoder().EncodeAll(raw, nil)

	objectCount := int64(0)
	for i := range frames {
		objectCount += int64(len(frames[i].Objects))
	}

	now := nowUTC()
	if _, err := q.ExecContext(ctx, `
		INSERT INTO match_replay_archives (
			match_id, schema_version, frame_count, object_count, payload_zstd, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(match_id) DO UPDATE SET
			schema_version = excluded.schema_version,
			frame_count = excluded.frame_count,
			object_count = excluded.object_count,
			payload_zstd = excluded.payload_zstd,
			updated_at = excluded.updated_at
	`, matchID, replayArchiveSchemaVersion, int64(len(frames)), objectCount, compressed, now, now); err != nil {
		return false, fmt.Errorf("upsert match replay archive: %w", err)
	}

	if _, err := q.ExecContext(ctx, `
		DELETE FROM match_replay_frame_objects
		WHERE frame_id IN (SELECT id FROM match_replay_frames WHERE match_id = ?)
	`, matchID); err != nil {
		return false, fmt.Errorf("delete archived replay frame objects: %w", err)
	}
	if _, err := q.ExecContext(ctx, `DELETE FROM match_replay_frames WHERE match_id = ?`, matchID); err != nil {
		return false, fmt.Errorf("delete archived replay frames: %w", err)
	}

	return true, nil
}

func loadReplayArchiveRowFrames(ctx context.Context, q querier, matchID int64) ([]replayArchiveFrame, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT
			id,
			game_number,
			game_state_id,
			prev_game_state_id,
			COALESCE(game_state_type, ''),
			COALESCE(game_stage, ''),
			turn_number,
			COALESCE(phase, ''),
			COALESCE(player_life_totals_json, ''),
			COALESCE(winning_player_side, ''),
			COALESCE(win_reason, ''),
			COALESCE(source, ''),
			COALESCE(recorded_at, ''),
			COALESCE(actions_json, ''),
			COALESCE(annotations_json, ''),
			COALESCE(created_at, '')
		FROM match_replay_frames
		WHERE match_id = ?
		ORDER BY game_number ASC, COALESCE(game_state_id, 0) ASC, id ASC
	`, matchID)
	if err != nil {
		return nil, fmt.Errorf("load replay frames for archive: %w", err)
	}
	defer rows.Close()

	frames := make([]replayArchiveFrame, 0)
	frameIndexByID := make(map[int64]int)
	for rows.Next() {
		var (
			frame           replayArchiveFrame
			gameNumber      sql.NullInt64
			gameStateID     sql.NullInt64
			prevGameStateID sql.NullInt64
			turnNumber      sql.NullInt64
		)
		if err := rows.Scan(
			&frame.ID,
			&gameNumber,
			&gameStateID,
			&prevGameStateID,
			&frame.GameStateType,
			&frame.GameStage,
			&turnNumber,
			&frame.Phase,
			&frame.PlayerLifeTotalsJSON,
			&frame.WinningPlayerSide,
			&frame.WinReason,
			&frame.Source,
			&frame.RecordedAt,
			&frame.ActionsJSON,
			&frame.AnnotationsJSON,
			&frame.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan replay frame for archive: %w", err)
		}
		frame.GameNumber = replayPtrFromNullInt64(gameNumber)
		frame.GameStateID = replayPtrFromNullInt64(gameStateID)
		frame.PrevGameStateID = replayPtrFromNullInt64(prevGameStateID)
		frame.TurnNumber = replayPtrFromNullInt64(turnNumber)
		frameIndexByID[frame.ID] = len(frames)
		frames = append(frames, frame)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate replay frames for archive: %w", err)
	}
	if len(frames) == 0 {
		return frames, nil
	}

	objectRows, err := q.QueryContext(ctx, `
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
		JOIN match_replay_frames f ON f.id = o.frame_id
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
		return nil, fmt.Errorf("load replay frame objects for archive: %w", err)
	}
	defer objectRows.Close()

	for objectRows.Next() {
		var (
			obj                  replayArchiveObject
			frameID              int64
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
			&obj.ID,
			&frameID,
			&obj.InstanceID,
			&obj.CardID,
			&ownerSeatID,
			&controllerSeatID,
			&zoneID,
			&obj.ZoneType,
			&zonePosition,
			&obj.Visibility,
			&power,
			&toughness,
			&isTapped,
			&hasSummoningSickness,
			&obj.AttackState,
			&attackTargetID,
			&obj.BlockState,
			&obj.BlockAttackerIDsJSON,
			&obj.CounterSummaryJSON,
			&obj.DetailsJSON,
			&isToken,
		); err != nil {
			return nil, fmt.Errorf("scan replay frame object for archive: %w", err)
		}
		obj.OwnerSeatID = replayPtrFromNullInt64(ownerSeatID)
		obj.ControllerSeatID = replayPtrFromNullInt64(controllerSeatID)
		obj.ZoneID = replayPtrFromNullInt64(zoneID)
		obj.ZonePosition = replayPtrFromNullInt64(zonePosition)
		obj.Power = replayPtrFromNullInt64(power)
		obj.Toughness = replayPtrFromNullInt64(toughness)
		obj.AttackTargetID = replayPtrFromNullInt64(attackTargetID)
		obj.IsTapped = isTapped > 0
		obj.HasSummoningSickness = hasSummoningSickness > 0
		obj.IsToken = isToken > 0

		index, ok := frameIndexByID[frameID]
		if !ok {
			continue
		}
		frames[index].Objects = append(frames[index].Objects, obj)
	}
	if err := objectRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate replay frame objects for archive: %w", err)
	}

	return frames, nil
}

func loadReplayArchivePayload(ctx context.Context, q querier, matchID int64) ([]replayArchiveFrame, bool, error) {
	var compressed []byte
	err := q.QueryRowContext(ctx, `
		SELECT payload_zstd FROM match_replay_archives WHERE match_id = ?
	`, matchID).Scan(&compressed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("load match replay archive: %w", err)
	}

	raw, err := getZstdDecoder().DecodeAll(compressed, nil)
	if err != nil {
		return nil, false, fmt.Errorf("decompress match replay archive: %w", err)
	}

	var payload replayArchivePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, false, fmt.Errorf("decode match replay archive: %w", err)
	}
	return payload.Frames, true, nil
}

func replayArchiveFrameKey(gameNumber, gameStateID *int64) string {
	game := int64(1)
	if gameNumber != nil && *gameNumber > 0 {
		game = *gameNumber
	}
	stateID := int64(0)
	if gameStateID != nil {
		stateID = *gameStateID
	}
	return fmt.Sprintf("%d:%d", game, stateID)
}

// mergeReplayArchiveFrames combines previously archived frames with freshly
// read rows. Row frames win on conflict because they reflect the most recent
// parse of the log.
func mergeReplayArchiveFrames(archived, rows []replayArchiveFrame) []replayArchiveFrame {
	if len(archived) == 0 {
		return rows
	}

	rowKeys := make(map[string]bool, len(rows))
	for i := range rows {
		rowKeys[replayArchiveFrameKey(rows[i].GameNumber, rows[i].GameStateID)] = true
	}

	merged := make([]replayArchiveFrame, 0, len(archived)+len(rows))
	for i := range archived {
		if rowKeys[replayArchiveFrameKey(archived[i].GameNumber, archived[i].GameStateID)] {
			continue
		}
		merged = append(merged, archived[i])
	}
	merged = append(merged, rows...)
	sortReplayArchiveFrames(merged)
	return merged
}

func sortReplayArchiveFrames(frames []replayArchiveFrame) {
	sort.SliceStable(frames, func(i, j int) bool {
		gi, gj := int64(1), int64(1)
		if frames[i].GameNumber != nil && *frames[i].GameNumber > 0 {
			gi = *frames[i].GameNumber
		}
		if frames[j].GameNumber != nil && *frames[j].GameNumber > 0 {
			gj = *frames[j].GameNumber
		}
		if gi != gj {
			return gi < gj
		}
		si, sj := int64(0), int64(0)
		if frames[i].GameStateID != nil {
			si = *frames[i].GameStateID
		}
		if frames[j].GameStateID != nil {
			sj = *frames[j].GameStateID
		}
		if si != sj {
			return si < sj
		}
		return frames[i].ID < frames[j].ID
	})
}

// loadArchivedMatchReplayFrames reconstructs API-shaped replay frames from a
// match's archive blob, recomputing the fields ListMatchReplayFrames derives
// in SQL for live rows (card names, player side, life totals).
func (s *Store) loadArchivedMatchReplayFrames(ctx context.Context, matchID int64) ([]model.MatchReplayFrameRow, error) {
	archived, found, err := loadReplayArchivePayload(ctx, s.db, matchID)
	if err != nil {
		return nil, err
	}
	if !found || len(archived) == 0 {
		return nil, nil
	}

	var playerSeatID sql.NullInt64
	err = s.db.QueryRowContext(ctx, `SELECT player_seat_id FROM matches WHERE id = ?`, matchID).Scan(&playerSeatID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("lookup match seat for replay archive: %w", err)
	}
	selfSeatID := replayPtrFromNullInt64(playerSeatID)

	cardIDSet := make(map[int64]bool)
	for i := range archived {
		for j := range archived[i].Objects {
			cardIDSet[archived[i].Objects[j].CardID] = true
		}
	}
	cardIDs := make([]int64, 0, len(cardIDSet))
	for id := range cardIDSet {
		cardIDs = append(cardIDs, id)
	}
	cardNames, err := s.LookupCardNames(ctx, cardIDs)
	if err != nil {
		return nil, err
	}

	frames := make([]model.MatchReplayFrameRow, 0, len(archived))
	for i := range archived {
		src := &archived[i]
		frame := model.MatchReplayFrameRow{
			ID:                src.ID,
			GameNumber:        src.GameNumber,
			GameStateID:       src.GameStateID,
			PrevGameStateID:   src.PrevGameStateID,
			GameStateType:     src.GameStateType,
			GameStage:         src.GameStage,
			TurnNumber:        src.TurnNumber,
			Phase:             src.Phase,
			WinningPlayerSide: src.WinningPlayerSide,
			WinReason:         src.WinReason,
			RecordedAt:        src.RecordedAt,
			ActionsJSON:       src.ActionsJSON,
			AnnotationsJSON:   src.AnnotationsJSON,
		}
		frame.SelfLifeTotal, frame.OpponentLifeTotal = replayFrameLifeTotals(
			parseReplayPlayerLifeTotalsJSON(src.PlayerLifeTotalsJSON),
			selfSeatID,
		)
		for j := range src.Objects {
			obj := &src.Objects[j]
			frame.Objects = append(frame.Objects, model.MatchReplayFrameObjectRow{
				ID:                   obj.ID,
				FrameID:              src.ID,
				InstanceID:           obj.InstanceID,
				CardID:               obj.CardID,
				CardName:             cardNames[obj.CardID],
				OwnerSeatID:          obj.OwnerSeatID,
				ControllerSeatID:     obj.ControllerSeatID,
				PlayerSide:           replayObjectPlayerSide(obj.ControllerSeatID, obj.OwnerSeatID, selfSeatID),
				ZoneID:               obj.ZoneID,
				ZoneType:             obj.ZoneType,
				ZonePosition:         obj.ZonePosition,
				Visibility:           obj.Visibility,
				Power:                obj.Power,
				Toughness:            obj.Toughness,
				AttackTargetID:       obj.AttackTargetID,
				BlockAttackerIDsJSON: obj.BlockAttackerIDsJSON,
				CounterSummaryJSON:   obj.CounterSummaryJSON,
				DetailsJSON:          obj.DetailsJSON,
				AttackState:          obj.AttackState,
				BlockState:           obj.BlockState,
				IsToken:              obj.IsToken,
				IsTapped:             obj.IsTapped,
				HasSummoningSickness: obj.HasSummoningSickness,
			})
		}
		frames = append(frames, frame)
	}
	return frames, nil
}

// replayObjectPlayerSide mirrors the CASE expression ListMatchReplayFrames
// uses for live rows.
func replayObjectPlayerSide(controllerSeatID, ownerSeatID, selfSeatID *int64) string {
	effective := controllerSeatID
	if effective == nil {
		effective = ownerSeatID
	}
	if selfSeatID != nil && effective != nil && *effective == *selfSeatID {
		return "self"
	}
	if effective != nil && *effective > 0 {
		return "opponent"
	}
	return "unknown"
}

// CompactMatchReplays archives replay frame rows for all matches that are no
// longer live: matches with a recorded result or end time, plus stale matches
// that never received an end event. Each match is archived in its own
// transaction. Returns the number of matches archived.
func (s *Store) CompactMatchReplays(ctx context.Context) (int, error) {
	cutoff := time.Now().UTC().Add(-replayArchiveStaleMatchAge).Format(time.RFC3339)
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT m.id
		FROM matches m
		JOIN match_replay_frames f ON f.match_id = m.id
		WHERE m.result IS NOT NULL
			OR m.ended_at IS NOT NULL
			OR COALESCE(m.started_at, '') < ?
		ORDER BY m.id ASC
	`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("list matches for replay compaction: %w", err)
	}

	matchIDs := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan match for replay compaction: %w", err)
		}
		matchIDs = append(matchIDs, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("iterate matches for replay compaction: %w", err)
	}
	rows.Close()

	archivedCount := 0
	for _, matchID := range matchIDs {
		if err := ctx.Err(); err != nil {
			return archivedCount, err
		}
		tx, err := s.BeginTx(ctx)
		if err != nil {
			return archivedCount, err
		}
		archived, err := s.archiveMatchReplayRows(ctx, tx, matchID)
		if err != nil {
			_ = tx.Rollback()
			return archivedCount, fmt.Errorf("compact replay for match %d: %w", matchID, err)
		}
		if err := tx.Commit(); err != nil {
			return archivedCount, fmt.Errorf("commit replay compaction for match %d: %w", matchID, err)
		}
		if archived {
			archivedCount++
		}
	}
	return archivedCount, nil
}
