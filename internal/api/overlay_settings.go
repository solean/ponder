package api

import (
	"github.com/solean/ponder/internal/appstate"
	"net/http"
)

func (s *Server) handleOverlaySettings(w http.ResponseWriter, r *http.Request) {
	if s.appState == nil {
		writeError(w, http.StatusNotFound, "overlay settings unavailable")
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.appState.OverlaySettings())
	case http.MethodPost:
		var input appstate.OverlaySettings
		if err := decodeJSONBody(r, &input); err != nil {
			writeJSONBodyError(w, err)
			return
		}
		settings, err := s.appState.UpdateOverlaySettings(input)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, settings)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}
