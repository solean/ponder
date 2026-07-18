package api

import (
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/solean/ponder/internal/db"
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

func (s *Server) handleDeckAnalytics(w http.ResponseWriter, r *http.Request, deckID int64) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	ctx := r.Context()

	// Land distributions depend on cached type lines; resolve any kept-hand
	// cards that have never been classified before aggregating. Basic lands
	// often fail Scryfall's arenaid search, so fall back to classifying those
	// by name, mirroring the live banner's land-odds handling.
	if keptCardIDs, err := s.store.ListDeckKeptHandCardIDs(ctx, deckID); err == nil && len(keptCardIDs) > 0 {
		typeLines := s.resolveCardTypeLines(ctx, keptCardIDs)
		unresolved := make([]int64, 0)
		for _, cardID := range keptCardIDs {
			if _, ok := typeLines[cardID]; !ok {
				unresolved = append(unresolved, cardID)
			}
		}
		if len(unresolved) > 0 {
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
	}

	out, err := s.store.GetDeckAnalytics(ctx, deckID, queryInt64(r, "version"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

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

	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDeckAnalyticsGames(w http.ResponseWriter, r *http.Request, deckID int64) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	rows, err := s.store.ListDeckAnalyticsGames(r.Context(), db.DeckAnalyticsGamesQuery{
		DeckID:        deckID,
		DeckVersionID: queryInt64(r, "version"),
		CardID:        queryInt64(r, "card"),
		Facet:         strings.TrimSpace(r.URL.Query().Get("facet")),
		KeptHandSize:  queryOptionalInt64(r, "keptSize"),
		MulliganCount: queryOptionalInt64(r, "mulligans"),
		GameFilter:    strings.TrimSpace(r.URL.Query().Get("game")),
		PlayDraw:      strings.TrimSpace(r.URL.Query().Get("playDraw")),
		Limit:         queryInt64(r, "limit"),
	})
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
