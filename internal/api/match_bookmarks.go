package api

import (
	"database/sql"
	"errors"
	"net/http"
)

// handleMatchBookmark saves or unsaves a match: POST {"saved": bool}.
func (s *Server) handleMatchBookmark(w http.ResponseWriter, r *http.Request, matchID int64) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	payload := struct {
		Saved *bool `json:"saved"`
	}{}
	if err := decodeJSONBody(r, &payload); err != nil {
		writeJSONBodyError(w, err)
		return
	}
	if payload.Saved == nil {
		writeError(w, http.StatusBadRequest, `missing "saved"`)
		return
	}
	err := s.store.SetMatchBookmark(r.Context(), matchID, *payload.Saved)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "match not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": *payload.Saved})
}
