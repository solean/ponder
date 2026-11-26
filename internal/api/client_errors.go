package api

import (
	"log"
	"net/http"
	"strings"
)

// clientErrorFieldLimit keeps one frontend crash from flooding the app log.
const clientErrorFieldLimit = 4 << 10

// handleClientError writes a frontend crash to the app log. The crash screen
// points users at the log file, and errors in the web view never reach it
// otherwise.
func (s *Server) handleClientError(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	payload := struct {
		Scope          string `json:"scope"`
		Message        string `json:"message"`
		Stack          string `json:"stack"`
		ComponentStack string `json:"componentStack"`
	}{}
	if err := decodeJSONBody(r, &payload); err != nil {
		writeJSONBodyError(w, err)
		return
	}

	scope := clientErrorField(strings.TrimSpace(payload.Scope), 32)
	if scope == "" {
		scope = "app"
	}
	message := clientErrorField(strings.TrimSpace(payload.Message), clientErrorFieldLimit)
	if message == "" {
		message = "unknown error"
	}
	var b strings.Builder
	b.WriteString("frontend error (" + scope + "): " + message)
	if stack := clientErrorField(payload.Stack, clientErrorFieldLimit); stack != "" {
		b.WriteString("\n" + stack)
	}
	if stack := clientErrorField(payload.ComponentStack, clientErrorFieldLimit); stack != "" {
		b.WriteString("\ncomponent stack:\n" + stack)
	}
	log.Print(b.String())
	w.WriteHeader(http.StatusNoContent)
}

// clientErrorField drops trailing whitespace, keeping a stack's indentation,
// and caps the length.
func clientErrorField(value string, limit int) string {
	value = strings.TrimRight(value, " \t\r\n")
	if len(value) > limit {
		value = strings.ToValidUTF8(value[:limit], "") + "…"
	}
	return value
}
