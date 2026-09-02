package api

import (
	"database/sql"
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/solean/ponder/internal/model"
)

func projectLiveDeck(
	cards []model.DeckCardRow,
	knownOutsideLibrary map[int64]int64,
	stateAvailable bool,
) ([]model.LiveDeckCardRow, int64, *int64) {
	deck := make([]model.LiveDeckCardRow, 0, len(cards))
	var deckTotal, libraryCount int64
	for _, card := range cards {
		if card.Section != "main" || card.Quantity <= 0 {
			continue
		}
		row := model.LiveDeckCardRow{
			Section:  card.Section,
			CardID:   card.CardID,
			Quantity: card.Quantity,
			CardName: card.CardName,
		}
		deckTotal += card.Quantity
		if stateAvailable {
			remaining := max(int64(0), card.Quantity-knownOutsideLibrary[card.CardID])
			row.Remaining = &remaining
			libraryCount += remaining
		}
		deck = append(deck, row)
	}
	sort.SliceStable(deck, func(i, j int) bool {
		left := strings.ToLower(strings.TrimSpace(deck[i].CardName))
		right := strings.ToLower(strings.TrimSpace(deck[j].CardName))
		if left != right {
			return left < right
		}
		return deck[i].CardID < deck[j].CardID
	})
	if !stateAvailable || len(deck) == 0 {
		return deck, deckTotal, nil
	}
	return deck, deckTotal, &libraryCount
}

// handleLive returns the match currently in progress with the current
// submitted deck, per-card remaining-library counts, and opponent public
// reveals. Responds {"live": null} when nothing is being played.
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

	opponentCards := detail.OpponentObservedCards
	if opponentCards == nil {
		opponentCards = []model.OpponentObservedCardRow{}
	}

	game, arenaTurn, err := s.store.GetLiveProgress(ctx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	gameForState := game
	if gameForState <= 0 {
		gameForState = 1
	}

	deckCards, submitted, err := s.store.ListMatchGameDeckCards(ctx, id, gameForState)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	deckSource := "submitted"
	if !submitted {
		deckSource = "unavailable"
		if detail.Match.DeckID != nil && *detail.Match.DeckID > 0 {
			deckCards, err = s.store.ListDeckCards(ctx, *detail.Match.DeckID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			deckSource = "linked"
		}
	}
	if deckCards == nil {
		deckCards = []model.DeckCardRow{}
	}
	s.enrichDeckCardNames(ctx, deckCards)

	knownOutsideLibrary, stateAvailable, err := s.store.GetLiveKnownSelfCardCounts(ctx, id, gameForState)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	deck, deckTotal, libraryCount := projectLiveDeck(deckCards, knownOutsideLibrary, stateAvailable)

	live := model.LiveMatch{
		Match:                 matchRows[0],
		OpponentObservedCards: opponentCards,
		Deck:                  deck,
		DeckTotal:             deckTotal,
		LibraryCount:          libraryCount,
		DeckSource:            deckSource,
		GameNumber:            game,
		TurnNumber:            model.ArenaTurnToFullTurn(arenaTurn),
	}
	writeJSON(w, http.StatusOK, map[string]any{"live": live})
}
