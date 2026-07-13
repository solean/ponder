package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cschnabel/mtgdata/internal/model"
)

func (s *Store) EnsureDraftSession(ctx context.Context, tx *sql.Tx, eventName string, draftID *string, isBot bool, ts string) (int64, error) {
	isBotInt := 0
	if isBot {
		isBotInt = 1
	}
	ts = normalizeTS(ts)

	var sessionID int64
	if draftID != nil && *draftID != "" {
		err := tx.QueryRowContext(ctx, `SELECT id FROM draft_sessions WHERE draft_id = ? AND is_bot_draft = ?`, *draftID, isBotInt).Scan(&sessionID)
		if err == nil {
			_, _ = tx.ExecContext(ctx, `
				UPDATE draft_sessions
				SET event_name = COALESCE(?, event_name), started_at = COALESCE(started_at, ?), updated_at = ?
				WHERE id = ?
			`, nullIfEmpty(eventName), nullIfEmpty(ts), nowUTC(), sessionID)
			return sessionID, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("select draft session by draft_id: %w", err)
		}
	}

	if draftID == nil || *draftID == "" {
		// For bot drafts (or unknown IDs), reuse active session for same event if incomplete.
		err := tx.QueryRowContext(ctx, `
			SELECT id
			FROM draft_sessions
			WHERE event_name = ? AND is_bot_draft = ? AND completed_at IS NULL
			ORDER BY id DESC
			LIMIT 1
		`, eventName, isBotInt).Scan(&sessionID)
		if err == nil {
			_, _ = tx.ExecContext(ctx, `
				UPDATE draft_sessions
				SET started_at = COALESCE(started_at, ?), updated_at = ?
				WHERE id = ?
			`, nullIfEmpty(ts), nowUTC(), sessionID)
			return sessionID, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("select active draft session: %w", err)
		}
	}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO draft_sessions (event_name, draft_id, is_bot_draft, started_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, nullIfEmpty(eventName), nullDraftID(draftID), isBotInt, nullIfEmpty(ts), nowUTC(), nowUTC())
	if err != nil {
		return 0, fmt.Errorf("insert draft_session: %w", err)
	}

	if err := tx.QueryRowContext(ctx, `SELECT last_insert_rowid()`).Scan(&sessionID); err != nil {
		return 0, fmt.Errorf("last_insert_rowid draft_session: %w", err)
	}

	return sessionID, nil
}

func nullDraftID(v *string) any {
	if v == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*v)
	if trimmed == "" {
		return nil
	}
	return trimmed
}

func (s *Store) InsertDraftPick(ctx context.Context, tx *sql.Tx, sessionID int64, packNo, pickNo int64, pickedIDs []int64, packIDs []int64, ts string) error {
	pickedJSON, _ := json.Marshal(pickedIDs)
	packJSON := []byte("[]")
	if len(packIDs) > 0 {
		packJSON, _ = json.Marshal(packIDs)
	}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO draft_picks (
			draft_session_id, pack_number, pick_number, picked_card_ids, pack_card_ids, pick_ts, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(draft_session_id, pack_number, pick_number) DO UPDATE SET
			picked_card_ids = excluded.picked_card_ids,
			pack_card_ids = excluded.pack_card_ids,
			pick_ts = COALESCE(excluded.pick_ts, draft_picks.pick_ts)
	`, sessionID, packNo, pickNo, string(pickedJSON), string(packJSON), nullIfEmpty(normalizeTS(ts)), nowUTC())
	if err != nil {
		return fmt.Errorf("insert draft_pick: %w", err)
	}

	_, _ = tx.ExecContext(ctx, `UPDATE draft_sessions SET updated_at = ? WHERE id = ?`, nowUTC(), sessionID)
	return nil
}

func (s *Store) CompleteDraftSession(ctx context.Context, tx *sql.Tx, eventName string, draftID *string, isBot bool, ts string) error {
	isBotInt := 0
	if isBot {
		isBotInt = 1
	}
	ts = normalizeTS(ts)

	if draftID != nil && strings.TrimSpace(*draftID) != "" {
		_, err := tx.ExecContext(ctx, `
			UPDATE draft_sessions
			SET completed_at = COALESCE(completed_at, ?), updated_at = ?
			WHERE draft_id = ? AND is_bot_draft = ?
		`, nullIfEmpty(ts), nowUTC(), strings.TrimSpace(*draftID), isBotInt)
		if err != nil {
			return fmt.Errorf("complete draft session by draft_id: %w", err)
		}
		return nil
	}

	if eventName != "" {
		res, err := tx.ExecContext(ctx, `
			UPDATE draft_sessions
			SET event_name = COALESCE(?, event_name), completed_at = COALESCE(completed_at, ?), updated_at = ?
			WHERE id = (
				SELECT id FROM draft_sessions
				WHERE is_bot_draft = ?
				  AND completed_at IS NULL
				  AND (
					event_name = ?
					OR COALESCE(event_name, '') = ''
				  )
				ORDER BY CASE
					WHEN event_name = ? THEN 0
					WHEN COALESCE(event_name, '') = '' THEN 1
					ELSE 2
				END,
				COALESCE(started_at, updated_at, created_at) DESC,
				id DESC
				LIMIT 1
			)
		`, nullIfEmpty(eventName), nullIfEmpty(ts), nowUTC(), isBotInt, eventName, eventName)
		if err != nil {
			return fmt.Errorf("complete draft session by event_name: %w", err)
		}
		if rowsAffected, rowsErr := res.RowsAffected(); rowsErr == nil && rowsAffected > 0 {
			return nil
		}
	}

	return nil
}

func (s *Store) RepairDraftDataFromRawEvents(ctx context.Context) error {
	now := nowUTC()

	_, err := s.db.ExecContext(ctx, `
		UPDATE draft_sessions
		SET
			event_name = COALESCE(
				NULLIF(draft_sessions.event_name, ''),
				(
					SELECT COALESCE(
						NULLIF(json_extract(er.payload_json, '$.EventId'), ''),
						NULLIF(json_extract(er.payload_json, '$.EventName'), '')
					)
					FROM events_raw er
					WHERE er.kind = 'outgoing'
					  AND er.method_name = 'LogBusinessEvents'
					  AND json_extract(er.payload_json, '$.EventType') = 24
					  AND json_extract(er.payload_json, '$.DraftId') = draft_sessions.draft_id
					ORDER BY COALESCE(NULLIF(json_extract(er.payload_json, '$.EventTime'), ''), er.created_at) DESC, er.id DESC
					LIMIT 1
				),
				(
					SELECT NULLIF(json_extract(er.payload_json, '$.EventName'), '')
					FROM events_raw er
					WHERE er.kind = 'outgoing'
					  AND er.method_name = 'DraftCompleteDraft'
					  AND json_extract(er.payload_json, '$.IsBotDraft') = draft_sessions.is_bot_draft
					  AND er.created_at >= draft_sessions.updated_at
					ORDER BY er.created_at ASC, er.id ASC
					LIMIT 1
				),
				(
					SELECT NULLIF(json_extract(er.payload_json, '$.EventName'), '')
					FROM events_raw er
					WHERE er.kind = 'outgoing'
					  AND er.method_name = 'EventSetDeckV2'
					  AND LOWER(COALESCE(json_extract(er.payload_json, '$.EventName'), '')) LIKE '%draft%'
					  AND er.created_at >= draft_sessions.updated_at
					ORDER BY er.created_at ASC, er.id ASC
					LIMIT 1
				)
			),
			started_at = COALESCE(
				NULLIF(draft_sessions.started_at, ''),
				(
					SELECT MIN(COALESCE(NULLIF(json_extract(er.payload_json, '$.EventTime'), ''), er.created_at))
					FROM events_raw er
					WHERE er.kind = 'outgoing'
					  AND er.method_name = 'LogBusinessEvents'
					  AND json_extract(er.payload_json, '$.EventType') = 24
					  AND json_extract(er.payload_json, '$.DraftId') = draft_sessions.draft_id
				),
				(
					SELECT MIN(er.created_at)
					FROM events_raw er
					WHERE er.kind = 'outgoing'
					  AND er.method_name = 'EventPlayerDraftMakePick'
					  AND json_extract(er.payload_json, '$.DraftId') = draft_sessions.draft_id
				)
			),
			completed_at = COALESCE(
				NULLIF(draft_sessions.completed_at, ''),
				(
					SELECT MAX(COALESCE(NULLIF(json_extract(er.payload_json, '$.EventTime'), ''), er.created_at))
					FROM events_raw er
					WHERE er.kind = 'outgoing'
					  AND er.method_name = 'LogBusinessEvents'
					  AND json_extract(er.payload_json, '$.EventType') = 24
					  AND json_extract(er.payload_json, '$.DraftId') = draft_sessions.draft_id
				),
				(
					SELECT er.created_at
					FROM events_raw er
					WHERE er.kind = 'outgoing'
					  AND er.method_name = 'DraftCompleteDraft'
					  AND json_extract(er.payload_json, '$.IsBotDraft') = draft_sessions.is_bot_draft
					  AND er.created_at >= draft_sessions.updated_at
					ORDER BY er.created_at ASC, er.id ASC
					LIMIT 1
				),
				(
					SELECT MAX(er.created_at)
					FROM events_raw er
					WHERE er.kind = 'outgoing'
					  AND er.method_name = 'EventPlayerDraftMakePick'
					  AND json_extract(er.payload_json, '$.DraftId') = draft_sessions.draft_id
				)
			),
			updated_at = ?
		WHERE draft_sessions.draft_id IS NOT NULL
		  AND (
			COALESCE(draft_sessions.event_name, '') = ''
			OR COALESCE(draft_sessions.started_at, '') = ''
			OR COALESCE(draft_sessions.completed_at, '') = ''
		  )
		  AND EXISTS (
			SELECT 1
			FROM events_raw er
			WHERE er.kind = 'outgoing'
			  AND (
				(
					er.method_name = 'LogBusinessEvents'
					AND json_extract(er.payload_json, '$.EventType') = 24
					AND json_extract(er.payload_json, '$.DraftId') = draft_sessions.draft_id
				)
				OR (
					er.method_name = 'EventPlayerDraftMakePick'
					AND json_extract(er.payload_json, '$.DraftId') = draft_sessions.draft_id
				)
				OR (
					er.method_name = 'DraftCompleteDraft'
					AND json_extract(er.payload_json, '$.IsBotDraft') = draft_sessions.is_bot_draft
					AND er.created_at >= draft_sessions.updated_at
				)
			  )
		  )
	`, now)
	if err != nil {
		return fmt.Errorf("repair draft sessions from raw events: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		UPDATE draft_picks
		SET
			pick_ts = COALESCE(
				NULLIF(draft_picks.pick_ts, ''),
				(
					SELECT COALESCE(NULLIF(json_extract(er.payload_json, '$.EventTime'), ''), er.created_at)
					FROM events_raw er
					JOIN draft_sessions ds ON ds.draft_id = json_extract(er.payload_json, '$.DraftId')
					WHERE er.kind = 'outgoing'
					  AND er.method_name = 'LogBusinessEvents'
					  AND json_extract(er.payload_json, '$.EventType') = 24
					  AND ds.id = draft_picks.draft_session_id
					  AND CAST(json_extract(er.payload_json, '$.PackNumber') AS INTEGER) = draft_picks.pack_number
					  AND CAST(json_extract(er.payload_json, '$.PickNumber') AS INTEGER) = draft_picks.pick_number
					ORDER BY er.id DESC
					LIMIT 1
				),
				(
					SELECT er.created_at
					FROM events_raw er
					JOIN draft_sessions ds ON ds.draft_id = json_extract(er.payload_json, '$.DraftId')
					WHERE er.kind = 'outgoing'
					  AND er.method_name = 'EventPlayerDraftMakePick'
					  AND ds.id = draft_picks.draft_session_id
					  AND CAST(json_extract(er.payload_json, '$.Pack') AS INTEGER) = draft_picks.pack_number
					  AND CAST(json_extract(er.payload_json, '$.Pick') AS INTEGER) = draft_picks.pick_number
					ORDER BY er.id DESC
					LIMIT 1
				)
			),
			pack_card_ids = CASE
				WHEN COALESCE(draft_picks.pack_card_ids, '') IN ('', '[]') THEN COALESCE(
					(
						SELECT json_extract(er.payload_json, '$.CardsInPack')
						FROM events_raw er
						JOIN draft_sessions ds ON ds.draft_id = json_extract(er.payload_json, '$.DraftId')
						WHERE er.kind = 'outgoing'
						  AND er.method_name = 'LogBusinessEvents'
						  AND json_extract(er.payload_json, '$.EventType') = 24
						  AND ds.id = draft_picks.draft_session_id
						  AND CAST(json_extract(er.payload_json, '$.PackNumber') AS INTEGER) = draft_picks.pack_number
						  AND CAST(json_extract(er.payload_json, '$.PickNumber') AS INTEGER) = draft_picks.pick_number
						ORDER BY er.id DESC
						LIMIT 1
					),
					draft_picks.pack_card_ids
				)
				ELSE draft_picks.pack_card_ids
			END
		WHERE (
			COALESCE(draft_picks.pick_ts, '') = ''
			OR COALESCE(draft_picks.pack_card_ids, '') IN ('', '[]')
		)
		  AND EXISTS (
			SELECT 1
			FROM events_raw er
			JOIN draft_sessions ds ON ds.draft_id = json_extract(er.payload_json, '$.DraftId')
			WHERE er.kind = 'outgoing'
			  AND (
				(
					er.method_name = 'LogBusinessEvents'
					AND json_extract(er.payload_json, '$.EventType') = 24
					AND ds.id = draft_picks.draft_session_id
					AND CAST(json_extract(er.payload_json, '$.PackNumber') AS INTEGER) = draft_picks.pack_number
					AND CAST(json_extract(er.payload_json, '$.PickNumber') AS INTEGER) = draft_picks.pick_number
				)
				OR (
					er.method_name = 'EventPlayerDraftMakePick'
					AND ds.id = draft_picks.draft_session_id
					AND CAST(json_extract(er.payload_json, '$.Pack') AS INTEGER) = draft_picks.pack_number
					AND CAST(json_extract(er.payload_json, '$.Pick') AS INTEGER) = draft_picks.pick_number
				)
			  )
		  )
	`)
	if err != nil {
		return fmt.Errorf("repair draft picks from raw events: %w", err)
	}

	return nil
}

// ListDraftSessions reads current draft rows; RepairDraftDataFromRawEvents
// runs after ingest and during startup maintenance, not on this read path.
func (s *Store) ListDraftSessions(ctx context.Context) ([]model.DraftSessionRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			ds.id,
			COALESCE(ds.event_name, ''),
			ds.draft_id,
			ds.is_bot_draft,
			COALESCE(ds.started_at, ''),
			COALESCE(ds.completed_at, ''),
			COUNT(dp.id) AS picks
		FROM draft_sessions ds
		LEFT JOIN draft_picks dp ON dp.draft_session_id = ds.id
		GROUP BY ds.id, ds.event_name, ds.draft_id, ds.is_bot_draft, ds.started_at, ds.completed_at
		ORDER BY ds.id DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list draft sessions: %w", err)
	}
	defer rows.Close()

	var out []model.DraftSessionRow
	for rows.Next() {
		var row model.DraftSessionRow
		var isBotInt int64
		if err := rows.Scan(&row.ID, &row.EventName, &row.DraftID, &isBotInt, &row.StartedAt, &row.CompletedAt, &row.Picks); err != nil {
			return nil, fmt.Errorf("scan draft session row: %w", err)
		}
		row.IsBotDraft = isBotInt == 1
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate draft sessions: %w", err)
	}

	if err := s.enrichDraftSessionsWithDeckResults(ctx, out); err != nil {
		return nil, err
	}

	return out, nil
}

type draftDeckCandidate struct {
	DeckTS        time.Time
	FirstPlayedAt time.Time
	LastPlayedAt  time.Time
	Wins          int64
	Losses        int64
}

func (s *Store) enrichDraftSessionsWithDeckResults(ctx context.Context, sessions []model.DraftSessionRow) error {
	for idx := range sessions {
		wins, losses, ok, err := s.resolveDraftSessionDeckResults(ctx, sessions[idx].EventName, sessions[idx].StartedAt, sessions[idx].CompletedAt)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		sessions[idx].Wins = nullableInt64Ptr(wins)
		sessions[idx].Losses = nullableInt64Ptr(losses)
	}
	return nil
}

func chooseDraftDeckCandidate(candidates []draftDeckCandidate, startedAt, completedAt string) (draftDeckCandidate, bool) {
	if len(candidates) == 0 {
		return draftDeckCandidate{}, false
	}
	if len(candidates) == 1 {
		return candidates[0], true
	}

	anchor, ok := parseStoredTime(completedAt)
	if !ok {
		anchor, ok = parseStoredTime(startedAt)
	}
	if !ok {
		return draftDeckCandidate{}, false
	}

	bestIdx := -1
	bestPriority := 99
	var bestDiff time.Duration
	for idx, candidate := range candidates {
		reference := candidate.DeckTS
		if reference.IsZero() && !candidate.FirstPlayedAt.IsZero() {
			reference = candidate.FirstPlayedAt
		}
		if reference.IsZero() {
			continue
		}

		priority := 2
		if !candidate.DeckTS.IsZero() && !candidate.DeckTS.Before(anchor) {
			priority = 0
		} else if !candidate.FirstPlayedAt.IsZero() && !candidate.FirstPlayedAt.Before(anchor) {
			priority = 1
		}

		diff := reference.Sub(anchor)
		if diff < 0 {
			diff = -diff
		}

		if bestIdx == -1 || priority < bestPriority || (priority == bestPriority && diff < bestDiff) {
			bestIdx = idx
			bestPriority = priority
			bestDiff = diff
		}
	}

	if bestIdx == -1 {
		return draftDeckCandidate{}, false
	}

	return candidates[bestIdx], true
}

func (s *Store) resolveDraftSessionDeckResults(ctx context.Context, eventName, startedAt, completedAt string) (int64, int64, bool, error) {
	eventName = strings.TrimSpace(eventName)
	if eventName == "" {
		return 0, 0, false, nil
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			COALESCE(d.last_updated, d.created_at, ''),
			COALESCE(MIN(COALESCE(m.started_at, m.ended_at)), ''),
			COALESCE(MAX(COALESCE(m.started_at, m.ended_at)), ''),
			COALESCE(SUM(CASE WHEN m.result = 'win' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN m.result = 'loss' THEN 1 ELSE 0 END), 0)
		FROM decks d
		LEFT JOIN match_decks md ON md.deck_id = d.id
		LEFT JOIN matches m ON m.id = md.match_id
		WHERE d.event_name = ?
		  AND (
			LOWER(COALESCE(d.format, '')) = 'draft'
			OR LOWER(COALESCE(d.event_name, '')) LIKE '%draft%'
		  )
		GROUP BY d.id, d.last_updated, d.created_at
	`, eventName)
	if err != nil {
		return 0, 0, false, fmt.Errorf("resolve draft session deck results: %w", err)
	}
	defer rows.Close()

	var candidates []draftDeckCandidate
	for rows.Next() {
		var deckTSRaw, firstPlayedRaw, lastPlayedRaw string
		var candidate draftDeckCandidate
		if err := rows.Scan(&deckTSRaw, &firstPlayedRaw, &lastPlayedRaw, &candidate.Wins, &candidate.Losses); err != nil {
			return 0, 0, false, fmt.Errorf("scan draft deck candidate: %w", err)
		}
		if parsed, ok := parseStoredTime(deckTSRaw); ok {
			candidate.DeckTS = parsed
		}
		if parsed, ok := parseStoredTime(firstPlayedRaw); ok {
			candidate.FirstPlayedAt = parsed
		}
		if parsed, ok := parseStoredTime(lastPlayedRaw); ok {
			candidate.LastPlayedAt = parsed
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return 0, 0, false, fmt.Errorf("iterate draft deck candidates: %w", err)
	}

	candidate, ok := chooseDraftDeckCandidate(candidates, startedAt, completedAt)
	if !ok {
		return 0, 0, false, nil
	}
	return candidate.Wins, candidate.Losses, true, nil
}

func (s *Store) ListDraftPicks(ctx context.Context, draftSessionID int64) ([]model.DraftPickRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, pack_number, pick_number, picked_card_ids, COALESCE(pack_card_ids, '[]'), COALESCE(pick_ts, '')
		FROM draft_picks
		WHERE draft_session_id = ?
		ORDER BY pack_number, pick_number
	`, draftSessionID)
	if err != nil {
		return nil, fmt.Errorf("list draft picks: %w", err)
	}
	defer rows.Close()

	var out []model.DraftPickRow
	for rows.Next() {
		var r model.DraftPickRow
		if err := rows.Scan(&r.ID, &r.PackNumber, &r.PickNumber, &r.PickedCardIDs, &r.PackCardIDs, &r.PickTs); err != nil {
			return nil, fmt.Errorf("scan draft pick row: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate draft picks: %w", err)
	}

	return out, nil
}
