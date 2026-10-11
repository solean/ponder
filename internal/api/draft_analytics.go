package api

import (
	"net/http"

	"github.com/solean/ponder/internal/model"
)

func draftAnalyticsSet(r *http.Request) *string {
	if !r.URL.Query().Has("set") {
		return nil
	}
	set := r.URL.Query().Get("set")
	return &set
}

func (s *Server) handleDraftAnalytics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	ctx := r.Context()
	scope, err := s.store.LoadDraftAnalyticsScope(ctx, draftAnalyticsSet(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if cards, err := s.store.ListDraftKeptHandCardIDs(ctx, scope); err == nil {
		s.ensureCardTypeLines(ctx, cards)
	}
	analytics, err := s.store.GetDraftAnalytics(ctx, scope)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.enrichAnalyticsCardNames(ctx, &analytics)
	writeJSON(w, http.StatusOK, model.DraftAnalyticsResponse{Analytics: analytics, Sets: scope.Sets})
}

func (s *Server) handleDraftAnalyticsGames(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	scope, err := s.store.LoadDraftAnalyticsScope(r.Context(), draftAnalyticsSet(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rows, err := s.store.ListDraftAnalyticsGames(r.Context(), scope, analyticsGamesQuery(r))
	writeAnalyticsGames(w, rows, err)
}
