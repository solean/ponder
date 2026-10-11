package api

import (
	"context"
	"log"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/solean/ponder/internal/db"
	"github.com/solean/ponder/internal/model"
)

func queryInt64(r *http.Request, name string) int64 {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return 0
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0
	}
	return value
}

func queryOptionalInt64(r *http.Request, name string) *int64 {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return nil
	}
	return &value
}

// ensureCardTypeLines resolves and caches type lines for the given cards.
// Basic lands often fail Scryfall's arenaid search, so unresolved cards fall
// back to classification by name, mirroring the live banner's land-odds
// handling.
func (s *Server) ensureCardTypeLines(ctx context.Context, cardIDs []int64) {
	if len(cardIDs) == 0 {
		return
	}
	typeLines := s.resolveCardTypeLines(ctx, cardIDs)
	unresolved := make([]int64, 0)
	for _, cardID := range cardIDs {
		if _, ok := typeLines[cardID]; !ok {
			unresolved = append(unresolved, cardID)
		}
	}
	if len(unresolved) == 0 {
		return
	}
	names := s.resolveCardNames(ctx, unresolved)
	basicLandTypeLines := make(map[int64]string)
	for cardID, name := range names {
		if isBasicLandName(name) {
			basicLandTypeLines[cardID] = "Basic Land"
		}
	}
	if len(basicLandTypeLines) > 0 {
		if err := s.store.UpsertCardTypeLines(ctx, basicLandTypeLines); err != nil {
			log.Printf("basic land type cache upsert failed: %v", err)
		}
	}
}

func (s *Server) handleDeckAnalytics(w http.ResponseWriter, r *http.Request, deckID int64) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	ctx := r.Context()

	// Land distributions depend on cached type lines; resolve any kept-hand
	// cards that have never been classified before aggregating.
	if keptCardIDs, err := s.store.ListDeckKeptHandCardIDs(ctx, deckID); err == nil {
		s.ensureCardTypeLines(ctx, keptCardIDs)
	}

	out, err := s.store.GetDeckAnalytics(ctx, deckID, queryInt64(r, "version"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.finalizeAnalyticsCards(ctx, &out)
	writeJSON(w, http.StatusOK, out)
}

// finalizeAnalyticsCards fills in missing card names, then drops basic lands:
// they appear in nearly every game, so their per-card performance only echoes
// the overall record. Filtering by name needs the resolved names.
func (s *Server) finalizeAnalyticsCards(ctx context.Context, out *model.DeckAnalytics) {
	missingNameCardIDs := make([]int64, 0, len(out.Cards))
	for _, card := range out.Cards {
		if strings.TrimSpace(card.CardName) == "" {
			missingNameCardIDs = append(missingNameCardIDs, card.CardID)
		}
	}
	if len(missingNameCardIDs) > 0 {
		resolved := s.resolveCardNames(ctx, missingNameCardIDs)
		for index := range out.Cards {
			if strings.TrimSpace(out.Cards[index].CardName) == "" {
				out.Cards[index].CardName = resolved[out.Cards[index].CardID]
			}
		}
	}
	out.Cards = slices.DeleteFunc(out.Cards, func(card model.DeckCardPerformance) bool {
		return isBasicLandName(card.CardName)
	})
}

func (s *Server) handleDeckAnalyticsGames(w http.ResponseWriter, r *http.Request, deckID int64) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	q := analyticsGamesQuery(r)
	q.DeckID = deckID
	q.DeckVersionID = queryInt64(r, "version")
	rows, err := s.store.ListDeckAnalyticsGames(r.Context(), q)
	writeAnalyticsGames(w, rows, err)
}

func analyticsGamesQuery(r *http.Request) db.DeckAnalyticsGamesQuery {
	return db.DeckAnalyticsGamesQuery{
		CardID:        queryInt64(r, "card"),
		Facet:         strings.TrimSpace(r.URL.Query().Get("facet")),
		KeptHandSize:  queryOptionalInt64(r, "keptSize"),
		MulliganCount: queryOptionalInt64(r, "mulligans"),
		TurnCount:     queryOptionalInt64(r, "turns"),
		GameFilter:    strings.TrimSpace(r.URL.Query().Get("game")),
		PlayDraw:      strings.TrimSpace(r.URL.Query().Get("playDraw")),
		LandDrops:     strings.TrimSpace(r.URL.Query().Get("landDrops")),
		Limit:         queryInt64(r, "limit"),
	}
}

func writeAnalyticsGames(w http.ResponseWriter, rows []model.DeckAnalyticsGameRef, err error) {
	if err != nil {
		if strings.Contains(err.Error(), "unknown") || strings.Contains(err.Error(), "requires") {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rows)
}
