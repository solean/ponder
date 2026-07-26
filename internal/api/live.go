package api

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/solean/ponder/internal/model"
)

// openingHandSize is subtracted from the deck total when estimating how many
// cards remain in the library mid-match. The MTGA log doesn't expose your hand,
// so this (and the per-turn draw subtraction) makes the draw odds an estimate.
const openingHandSize = 7

// livePlayerDrawCount estimates how many normal draw steps the player has
// taken from Arena's per-player turn ordinal. The player on the play skips the
// first draw step; the player on the draw does not. Unknown play/draw data uses
// completed full turns as the conservative fallback.
func livePlayerDrawCount(arenaTurn int64, playDraw string) int64 {
	if arenaTurn <= 0 {
		return 0
	}
	switch playDraw {
	case "play":
		return (arenaTurn - 1) / 2
	case "draw":
		return arenaTurn / 2
	default:
		return arenaTurn / 2
	}
}

func liveLibraryEstimate(deckTotal, arenaTurn int64, playDraw string) int64 {
	estimate := deckTotal - openingHandSize - livePlayerDrawCount(arenaTurn, playDraw)
	if estimate < 1 {
		return 1
	}
	return estimate
}

// handleLive returns the match currently in progress, enriched with opponent
// revealed cards, your decklist, game/turn state, and a library-size estimate
// the frontend uses for draw odds. Responds {"live": null} when nothing is
// being played.
func (s *Server) handleLive(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/live" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	ctx := r.Context()
	id, ok, err := s.store.GetLiveMatchID(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"live": nil})
		return
	}

	detail, err := s.store.GetMatchDetail(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusOK, map[string]any{"live": nil})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.enrichOpponentObservedCardNames(ctx, detail.OpponentObservedCards)
	matchRows := []model.MatchRow{detail.Match}
	s.enrichMatchDeckColors(ctx, matchRows)

	// Always emit JSON arrays (not null) so the frontend can treat these as
	// lists unconditionally — early in a game both are empty.
	opponentCards := detail.OpponentObservedCards
	if opponentCards == nil {
		opponentCards = []model.OpponentObservedCardRow{}
	}

	live := model.LiveMatch{
		Match:                 matchRows[0],
		OpponentObservedCards: opponentCards,
		Deck:                  []model.DeckCardRow{},
	}

	if detail.Match.DeckID != nil && *detail.Match.DeckID > 0 {
		cards, err := s.store.ListDeckCards(ctx, *detail.Match.DeckID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		mainboard := make([]model.DeckCardRow, 0, len(cards))
		cardIDs := make([]int64, 0, len(cards))
		for _, c := range cards {
			if c.Section == "main" {
				mainboard = append(mainboard, c)
				cardIDs = append(cardIDs, c.CardID)
				live.DeckTotal += c.Quantity
			}
		}
		s.enrichDeckCardNames(ctx, mainboard)
		live.Deck = mainboard

		typeLines := s.resolveCardTypeLines(ctx, cardIDs)
		for _, c := range mainboard {
			if isLandTypeLine(typeLines[c.CardID]) || isBasicLandName(c.CardName) {
				live.LandCount += c.Quantity
			}
		}
	}

	game, arenaTurn, err := s.store.GetLiveProgress(ctx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	live.GameNumber = game
	live.TurnNumber = model.ArenaTurnToFullTurn(arenaTurn)

	live.LibraryEstimate = liveLibraryEstimate(live.DeckTotal, arenaTurn, detail.Match.PlayDraw)

	writeJSON(w, http.StatusOK, map[string]any{"live": live})
}
