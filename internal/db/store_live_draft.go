package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/solean/ponder/internal/model"
)

const (
	// liveDraftPickWindow bounds how long an unanswered pack keeps a draft
	// live. Player drafts time out picks in about a minute; Quick Draft has
	// no timer, so this is generous enough to cover a slow bot-draft pick.
	liveDraftPickWindow = 20 * time.Minute
	// liveDraftWaitWindow bounds how long a made pick keeps a draft live
	// while the next pack is passed along.
	liveDraftWaitWindow = 2 * time.Minute
	// Arena drafts are three packs.
	liveDraftPackCount = 3
)

// GetLiveDraft returns the draft pick Arena is showing at now, or nil when no
// draft is in progress. Session completion cannot decide this: raw-event
// repair stamps a player draft's completed_at from its latest pick while the
// draft is still running. Instead the newest pick row decides: a pack with no
// pick yet is on screen, and a made pick is followed by another unless it
// emptied the final pack. A match started since that row means the draft was
// left behind.
func (s *Store) GetLiveDraft(ctx context.Context, now time.Time) (*model.LiveDraft, error) {
	var (
		draft                model.LiveDraft
		isBot                bool
		pickedJSON, packJSON string
		lastActivity         string
		matchSince           bool
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT
			ds.id,
			ds.is_bot_draft,
			-- Player drafts learn their event name only once the draft ends;
			-- until then, associate a recent join using Arena's log clock on
			-- both sides. Comparing the join with Ponder's created_at mixes
			-- clocks, while insertion order can select unrelated old events.
			COALESCE(
				NULLIF(ds.event_name, ''),
				(SELECT er.event_name FROM event_runs er WHERE er.draft_session_id = ds.id),
				(
					SELECT er.event_name
					FROM event_runs er
					WHERE er.draft_session_id IS NULL
					  AND er.event_name LIKE '%Draft%'
					  AND julianday(er.started_at) <= julianday(ds.started_at)
					  AND (julianday(ds.started_at) - julianday(er.started_at)) * 1440 <= ?
					ORDER BY julianday(er.started_at) DESC, er.id DESC
					LIMIT 1
				),
				''
			),
			dp.pack_number,
			dp.pick_number,
			COALESCE(dp.picked_card_ids, ''),
			COALESCE(dp.pack_card_ids, ''),
			MAX(dp.created_at, ds.updated_at),
			-- Require both a later observation and a later match event.
			-- Insertion alone mistakes historical backfills for a new match;
			-- event time alone lets an older row with a skewed clock hide a draft.
			EXISTS (
				SELECT 1 FROM matches m
				WHERE m.created_at > dp.created_at
				  AND julianday(m.started_at) >= julianday(dp.created_at)
			)
		FROM (SELECT * FROM draft_picks ORDER BY id DESC LIMIT 1) dp
		JOIN draft_sessions ds ON ds.id = dp.draft_session_id
	`, economyEventLinkWindowMinutes).Scan(
		&draft.SessionID,
		&isBot,
		&draft.EventName,
		&draft.PackNumber,
		&draft.PickNumber,
		&pickedJSON,
		&packJSON,
		&lastActivity,
		&matchSince,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get live draft: %w", err)
	}
	if matchSince {
		return nil, nil
	}

	// Bot drafts number packs and picks from zero.
	if isBot {
		draft.PackNumber++
		draft.PickNumber++
	}
	picked := decodeDraftCardIDs(pickedJSON)
	pack := decodeDraftCardIDs(packJSON)

	window := liveDraftPickWindow
	if len(picked) > 0 {
		window = liveDraftWaitWindow
		draft.Waiting = true
		if len(pack) <= len(picked) && draft.PackNumber >= liveDraftPackCount {
			return nil, nil
		}
	} else if len(pack) == 0 {
		return nil, nil
	}
	if active, ok := parseStoredTime(lastActivity); !ok || now.Sub(active) > window {
		return nil, nil
	}

	draft.Pack = make([]model.DraftPickCard, 0, len(pack))
	if !draft.Waiting {
		for _, cardID := range pack {
			draft.Pack = append(draft.Pack, model.DraftPickCard{CardID: cardID})
		}
	}
	return &draft, nil
}

func decodeDraftCardIDs(raw string) []int64 {
	var ids []int64
	if raw == "" || json.Unmarshal([]byte(raw), &ids) != nil {
		return nil
	}
	return ids
}
