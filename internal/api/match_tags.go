package api

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/solean/ponder/internal/db"
)

// handleMatchTags replaces a match's tags: POST {"tags": [...]}. It answers
// with the stored, normalised set.
func (s *Server) handleMatchTags(w http.ResponseWriter, r *http.Request, matchID int64) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	payload := struct {
		Tags *[]string `json:"tags"`
	}{}
	if err := decodeJSONBody(r, &payload); err != nil {
		writeJSONBodyError(w, err)
		return
	}
	if payload.Tags == nil {
		writeError(w, http.StatusBadRequest, `missing "tags"`)
		return
	}
	tags, err := db.NormalizeMatchTags(*payload.Tags)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	err = s.store.SetMatchTags(r.Context(), matchID, tags)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "match not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string][]string{"tags": tags})
}

// handleMatchTagUsage lists every tag in use with its match count.
func (s *Server) handleMatchTagUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	usage, err := s.store.ListMatchTagUsage(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, usage)
}
