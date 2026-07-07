package api

import (
	"net/http"
	"path/filepath"
	"strings"

	"github.com/cschnabel/mtgdata/internal/appstate"
)

// Desktop provides native-shell integrations (file dialogs, revealing files in
// Finder) when the API runs inside the desktop app. Nil in headless serve mode.
type Desktop interface {
	// PickLogFile opens a native file dialog and returns the chosen path, or
	// "" if the user cancelled.
	PickLogFile() (string, error)
	// RevealPath shows the file or directory in the system file manager.
	RevealPath(path string) error
}

// SetDesktop enables the desktop-only runtime endpoints. Call before Handler.
func (s *Server) SetDesktop(desktop Desktop) {
	s.desktop = desktop
}

func (s *Server) handleRuntimePickLog(w http.ResponseWriter, r *http.Request) {
	if s.appState == nil {
		writeError(w, http.StatusNotFound, "runtime controls unavailable")
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.desktop == nil {
		writeError(w, http.StatusBadRequest, "file picker is only available in the desktop app")
		return
	}

	path, err := s.desktop.PickLogFile()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": path})
}

func (s *Server) handleRuntimeReveal(w http.ResponseWriter, r *http.Request) {
	if s.appState == nil {
		writeError(w, http.StatusNotFound, "runtime controls unavailable")
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.desktop == nil {
		writeError(w, http.StatusBadRequest, "reveal is only available in the desktop app")
		return
	}

	payload := struct {
		Path string `json:"path"`
	}{}
	if err := decodeJSONBody(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if !revealablePath(s.appState.Status(), payload.Path) {
		writeError(w, http.StatusBadRequest, "path is not revealable")
		return
	}

	if err := s.desktop.RevealPath(payload.Path); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// revealablePath allows only paths the status payload itself advertises, so
// the endpoint can't be used to poke at arbitrary filesystem locations.
func revealablePath(status appstate.Status, requested string) bool {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return false
	}
	cleaned := filepath.Clean(requested)
	for _, allowed := range status.RevealablePaths() {
		allowed = strings.TrimSpace(allowed)
		if allowed != "" && filepath.Clean(allowed) == cleaned {
			return true
		}
	}
	return false
}
