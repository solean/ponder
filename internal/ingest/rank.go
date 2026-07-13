package ingest

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"github.com/cschnabel/mtgdata/internal/db"
	"github.com/cschnabel/mtgdata/internal/model"
)

func (p *Parser) enqueueCompletedMatch(arenaMatchID string) {
	arenaMatchID = strings.TrimSpace(arenaMatchID)
	if arenaMatchID == "" {
		return
	}
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	for _, pending := range p.pendingCompletedMatches {
		if pending == arenaMatchID {
			return
		}
	}
	p.pendingCompletedMatches = append(p.pendingCompletedMatches, arenaMatchID)
	if len(p.pendingCompletedMatches) > 32 {
		p.pendingCompletedMatches = append([]string(nil), p.pendingCompletedMatches[len(p.pendingCompletedMatches)-32:]...)
	}
}

func (p *Parser) dequeueCompletedMatch() string {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	if len(p.pendingCompletedMatches) == 0 {
		return ""
	}
	matchID := p.pendingCompletedMatches[0]
	p.pendingCompletedMatches = p.pendingCompletedMatches[1:]
	return matchID
}

type combinedRankInfoResponse struct {
	ConstructedSeasonOrdinal *int64 `json:"constructedSeasonOrdinal"`
	ConstructedClass         string `json:"constructedClass"`
	ConstructedLevel         *int64 `json:"constructedLevel"`
	ConstructedStep          *int64 `json:"constructedStep"`
	ConstructedMatchesWon    *int64 `json:"constructedMatchesWon"`
	ConstructedMatchesLost   *int64 `json:"constructedMatchesLost"`
	LimitedSeasonOrdinal     *int64 `json:"limitedSeasonOrdinal"`
	LimitedClass             string `json:"limitedClass"`
	LimitedLevel             *int64 `json:"limitedLevel"`
	LimitedStep              *int64 `json:"limitedStep"`
	LimitedMatchesWon        *int64 `json:"limitedMatchesWon"`
	LimitedMatchesLost       *int64 `json:"limitedMatchesLost"`
}

func (p *Parser) queueCompletedMatchIfRankPending(ctx context.Context, tx *sql.Tx, arenaMatchID, result string, terminalChange bool) error {
	if result != "win" && result != "loss" {
		return nil
	}
	if terminalChange {
		p.enqueueCompletedMatch(arenaMatchID)
		return nil
	}

	hasSnapshot, err := p.store.MatchHasRankSnapshot(ctx, tx, arenaMatchID)
	if err != nil {
		return err
	}
	if !hasSnapshot {
		p.enqueueCompletedMatch(arenaMatchID)
	}
	return nil
}

func (p *Parser) handleMethodResponse(ctx context.Context, tx *sql.Tx, stats *model.ParseStats, state *parseState, logPath string, lineNo, byteOffset int64, line string) error {
	method := state.pendingResponseMethod
	requestID := state.pendingResponseRequestID
	observedAt := state.pendingResponseObservedAt
	state.clearPendingResponse()

	if stored, err := p.store.InsertRawEvent(ctx, tx, logPath, lineNo, byteOffset, "method_result", method, requestID, []byte(line), ""); err != nil {
		return err
	} else if stored {
		stats.RawEventsStored++
	}

	if method != "RankGetCombinedRankInfo" {
		return nil
	}

	var payload combinedRankInfoResponse
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		return nil
	}

	arenaMatchID := p.dequeueCompletedMatch()
	if arenaMatchID == "" {
		return nil
	}

	if err := p.store.UpsertMatchRankSnapshot(ctx, tx, arenaMatchID, db.MatchRankSnapshot{
		ObservedAt:               observedAt,
		PayloadJSON:              line,
		ConstructedSeasonOrdinal: payload.ConstructedSeasonOrdinal,
		ConstructedRankClass:     payload.ConstructedClass,
		ConstructedLevel:         payload.ConstructedLevel,
		ConstructedStep:          payload.ConstructedStep,
		ConstructedMatchesWon:    payload.ConstructedMatchesWon,
		ConstructedMatchesLost:   payload.ConstructedMatchesLost,
		LimitedSeasonOrdinal:     payload.LimitedSeasonOrdinal,
		LimitedRankClass:         payload.LimitedClass,
		LimitedLevel:             payload.LimitedLevel,
		LimitedStep:              payload.LimitedStep,
		LimitedMatchesWon:        payload.LimitedMatchesWon,
		LimitedMatchesLost:       payload.LimitedMatchesLost,
	}); err != nil {
		return err
	}
	stats.RankSnapshots++
	return nil
}
