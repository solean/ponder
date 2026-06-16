package api

import (
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cschnabel/mtgdata/internal/appstate"
	"github.com/cschnabel/mtgdata/internal/db"
	"github.com/cschnabel/mtgdata/internal/model"
	"github.com/cschnabel/mtgdata/internal/version"
)

type Server struct {
	store      *db.Store
	staticDir  string
	appState   *appstate.Service
	httpClient *http.Client
}

func NewServer(store *db.Store, staticDir string, appState *appstate.Service) *Server {
	return &Server{
		store:     store,
		staticDir: staticDir,
		appState:  appState,
		httpClient: &http.Client{
			Timeout: 8 * time.Second,
		},
	}
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/overview", s.handleOverview)
	mux.HandleFunc("/api/rank-history", s.handleRankHistory)
	mux.HandleFunc("/api/matches", s.handleMatches)
	mux.HandleFunc("/api/matches/", s.handleMatchDetail)
	mux.HandleFunc("/api/decks", s.handleDecks)
	mux.HandleFunc("/api/decks/", s.handleDeckDetail)
	mux.HandleFunc("/api/drafts", s.handleDrafts)
	mux.HandleFunc("/api/drafts/", s.handleDraftPicks)
	mux.HandleFunc("/api/sets", s.handleSets)
	mux.HandleFunc("/api/live", s.handleLive)
	if s.appState != nil {
		mux.HandleFunc("/api/runtime/status", s.handleRuntimeStatus)
		mux.HandleFunc("/api/runtime/config", s.handleRuntimeConfig)
		mux.HandleFunc("/api/runtime/import", s.handleRuntimeImport)
		mux.HandleFunc("/api/runtime/live/start", s.handleRuntimeLiveStart)
		mux.HandleFunc("/api/runtime/live/stop", s.handleRuntimeLiveStop)
		mux.HandleFunc("/api/runtime/autostart", s.handleRuntimeAutostart)
		mux.HandleFunc("/api/runtime/update-check", s.handleRuntimeUpdateCheck)
	}

	if s.staticDir != "" {
		if fi, err := os.Stat(s.staticDir); err == nil && fi.IsDir() {
			fs := http.FileServer(http.Dir(s.staticDir))
			mux.Handle("/", fs)
		} else {
			mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("mtgdata API is running. Frontend build not found."))
			})
		}
	}

	return withCORS(withGzip(mux))
}

// Handler exposes the full route handler so the desktop shell can mount the
// API on the Wails asset server (same-origin, no listening port).
func (s *Server) Handler() http.Handler {
	return s.routes()
}

type gzipResponseWriter struct {
	http.ResponseWriter
	gz *gzip.Writer
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) {
	return g.gz.Write(b)
}

// withGzip compresses API responses. Replay payloads in particular are large
// and highly repetitive, so this is a big win for the desktop webview.
// Static assets are left alone because http.FileServer sets Content-Length.
func withGzip(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") ||
			!strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		gz := gzip.NewWriter(w)
		defer gz.Close()
		next.ServeHTTP(&gzipResponseWriter{ResponseWriter: w, gz: gz}, r)
	})
}

func (s *Server) Run(ctx context.Context, addr string) error {
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           withHostCheck(s.routes()),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("HTTP server listening on %s", addr)
		err := httpServer.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		return err
	}
}

// isLocalDevOrigin reports whether a browser Origin belongs to a local dev
// server (e.g. Vite on http://localhost:5173). Cross-origin access is only
// granted to those; arbitrary websites must not be able to read the API.
func isLocalDevOrigin(origin string) bool {
	parsed, err := url.Parse(origin)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	host := parsed.Hostname()
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && isLocalDevOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Add("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS, POST")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// withHostCheck rejects requests whose Host header is a non-local hostname.
// DNS rebinding attacks reach a localhost server through a hostname the
// attacker controls; direct IP and localhost access are unaffected.
func withHostCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if split, _, err := net.SplitHostPort(host); err == nil {
			host = split
		}
		host = strings.ToLower(strings.Trim(host, "[]"))
		if host == "localhost" || host == "wails.localhost" || host == "wails" || net.ParseIP(host) != nil {
			next.ServeHTTP(w, r)
			return
		}
		http.Error(w, "forbidden host", http.StatusForbidden)
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	if status >= http.StatusInternalServerError {
		log.Printf("http %d: %s", status, message)
	}
	writeJSON(w, status, map[string]any{
		"error": message,
	})
}

func decodeJSONBody(r *http.Request, dst any) error {
	if r.Body == nil {
		return nil
	}
	defer r.Body.Close()

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read request body: %w", err)
	}
	if strings.TrimSpace(string(body)) == "" {
		return nil
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return fmt.Errorf("decode request body: %w", err)
	}
	return nil
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleRuntimeStatus(w http.ResponseWriter, r *http.Request) {
	if s.appState == nil {
		writeError(w, http.StatusNotFound, "runtime controls unavailable")
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, s.appState.Status())
}

func (s *Server) handleRuntimeConfig(w http.ResponseWriter, r *http.Request) {
	if s.appState == nil {
		writeError(w, http.StatusNotFound, "runtime controls unavailable")
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var input appstate.Config
	if err := decodeJSONBody(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	status, err := s.appState.UpdateConfig(input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleRuntimeImport(w http.ResponseWriter, r *http.Request) {
	if s.appState == nil {
		writeError(w, http.StatusNotFound, "runtime controls unavailable")
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	payload := struct {
		Resume *bool `json:"resume"`
	}{}
	if err := decodeJSONBody(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	resume := true
	if payload.Resume != nil {
		resume = *payload.Resume
	}

	result, err := s.appState.ParseNow(r.Context(), resume)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleRuntimeLiveStart(w http.ResponseWriter, r *http.Request) {
	if s.appState == nil {
		writeError(w, http.StatusNotFound, "runtime controls unavailable")
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	status, err := s.appState.StartLive()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleRuntimeLiveStop(w http.ResponseWriter, r *http.Request) {
	if s.appState == nil {
		writeError(w, http.StatusNotFound, "runtime controls unavailable")
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	status, err := s.appState.StopLive()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleRuntimeAutostart(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, appstate.GetAutostartStatus())
	case http.MethodPost:
		payload := struct {
			Enabled bool `json:"enabled"`
		}{}
		if err := decodeJSONBody(r, &payload); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		status, err := appstate.SetAutostart(payload.Enabled)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, status)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

const updateCheckRepo = "solean/mtga-tracker"

type updateCheckResponse struct {
	CurrentVersion  string `json:"currentVersion"`
	LatestVersion   string `json:"latestVersion,omitempty"`
	UpdateAvailable bool   `json:"updateAvailable"`
	ReleaseURL      string `json:"releaseUrl,omitempty"`
	Note            string `json:"note,omitempty"`
}

func (s *Server) handleRuntimeUpdateCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	out := updateCheckResponse{CurrentVersion: version.Version}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet,
		"https://api.github.com/repos/"+updateCheckRepo+"/releases/latest", nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		out.Note = fmt.Sprintf("update check failed: %v", err)
		writeJSON(w, http.StatusOK, out)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		out.Note = "no releases published yet"
		writeJSON(w, http.StatusOK, out)
		return
	}
	if resp.StatusCode != http.StatusOK {
		out.Note = fmt.Sprintf("update check failed: GitHub returned %d", resp.StatusCode)
		writeJSON(w, http.StatusOK, out)
		return
	}

	release := struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}{}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&release); err != nil {
		out.Note = fmt.Sprintf("update check failed: %v", err)
		writeJSON(w, http.StatusOK, out)
		return
	}

	out.LatestVersion = strings.TrimPrefix(strings.TrimSpace(release.TagName), "v")
	out.ReleaseURL = release.HTMLURL
	out.UpdateAvailable = isNewerVersion(out.LatestVersion, strings.TrimPrefix(out.CurrentVersion, "v"))
	writeJSON(w, http.StatusOK, out)
}

// isNewerVersion compares dotted numeric versions; non-numeric segments fall
// back to string comparison.
func isNewerVersion(latest, current string) bool {
	if latest == "" || current == "" || latest == current {
		return false
	}
	latestParts := strings.Split(latest, ".")
	currentParts := strings.Split(currentBaseVersion(current), ".")
	for i := 0; i < len(latestParts) || i < len(currentParts); i++ {
		var l, c string
		if i < len(latestParts) {
			l = latestParts[i]
		}
		if i < len(currentParts) {
			c = currentParts[i]
		}
		ln, lerr := strconv.ParseInt(l, 10, 64)
		cn, cerr := strconv.ParseInt(c, 10, 64)
		if lerr != nil || cerr != nil {
			if l != c {
				return l > c
			}
			continue
		}
		if ln != cn {
			return ln > cn
		}
	}
	return false
}

// currentBaseVersion strips pre-release/build suffixes ("0.1.0-dev" -> "0.1.0").
func currentBaseVersion(v string) string {
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		return v[:i]
	}
	return v
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	limit := int64(20)
	if raw := strings.TrimSpace(r.URL.Query().Get("recent")); raw != "" {
		if v, err := strconv.ParseInt(raw, 10, 64); err == nil {
			limit = v
		}
	}
	out, err := s.store.Overview(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.enrichMatchDeckColors(r.Context(), out.Recent)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleRankHistory(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.ListRankHistory(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (s *Server) handleMatches(w http.ResponseWriter, r *http.Request) {
	limit := int64(200)
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if v, err := strconv.ParseInt(raw, 10, 64); err == nil {
			limit = v
		}
	}
	event := strings.TrimSpace(r.URL.Query().Get("event"))
	result := strings.TrimSpace(r.URL.Query().Get("result"))

	rows, err := s.store.ListMatches(r.Context(), limit, event, result)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.enrichMatchDeckColors(r.Context(), rows)
	writeJSON(w, http.StatusOK, rows)
}

func (s *Server) handleMatchDetail(w http.ResponseWriter, r *http.Request) {
	prefix := "/api/matches/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	rawPath := strings.TrimSpace(strings.Trim(strings.TrimPrefix(r.URL.Path, prefix), "/"))
	if rawPath == "" {
		writeError(w, http.StatusBadRequest, "missing match id")
		return
	}
	parts := strings.Split(rawPath, "/")
	if len(parts) > 2 {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid match id")
		return
	}
	if len(parts) == 2 {
		switch parts[1] {
		case "timeline":
			rows, err := s.store.ListMatchCardPlays(r.Context(), id)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			s.enrichMatchCardPlayNames(r.Context(), rows)
			writeJSON(w, http.StatusOK, rows)
			return
		case "replay":
			frames, err := s.store.ListMatchReplayFrames(r.Context(), id)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			s.enrichMatchReplayNames(r.Context(), frames)
			writeJSON(w, http.StatusOK, frames)
			return
		default:
			writeError(w, http.StatusNotFound, "not found")
			return
		}
	}

	out, err := s.store.GetMatchDetail(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "match not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.enrichOpponentObservedCardNames(r.Context(), out.OpponentObservedCards)
	s.enrichMatchCardPlayNames(r.Context(), out.CardPlays)
	matchRows := []model.MatchRow{out.Match}
	s.enrichMatchDeckColors(r.Context(), matchRows)
	out.Match = matchRows[0]
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDecks(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/decks" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	scope := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("scope")))
	switch scope {
	case "", "constructed", "draft", "all":
	default:
		writeError(w, http.StatusBadRequest, "invalid scope (use constructed, draft, or all)")
		return
	}

	rows, err := s.store.ListDecksByScope(r.Context(), scope)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (s *Server) handleDeckDetail(w http.ResponseWriter, r *http.Request) {
	prefix := "/api/decks/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	idStr := strings.TrimPrefix(r.URL.Path, prefix)
	idStr = strings.Trim(idStr, "/")
	if idStr == "" {
		writeError(w, http.StatusBadRequest, "missing deck id")
		return
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid deck id")
		return
	}

	out, err := s.store.GetDeckDetail(r.Context(), id, 50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.enrichDeckCardNames(r.Context(), out.Cards)
	s.enrichMatchDeckColors(r.Context(), out.Matches)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDrafts(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/drafts" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	rows, err := s.store.ListDraftSessions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (s *Server) handleDraftPicks(w http.ResponseWriter, r *http.Request) {
	prefix := "/api/drafts/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, prefix), "/")
	if len(parts) != 2 || parts[1] != "picks" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid draft id")
		return
	}
	rows, err := s.store.ListDraftPicks(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.enrichDraftPickCardNames(r.Context(), rows)
	writeJSON(w, http.StatusOK, rows)
}

func DefaultStaticDir(repoRoot string) string {
	if repoRoot == "" {
		return ""
	}
	return filepath.Join(repoRoot, "web", "dist")
}

func ParseAddr(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("address is empty")
	}
	return raw, nil
}

const (
	scryfallSearchURL      = "https://api.scryfall.com/cards/search"
	scryfallSearchBatchMax = 40
	mtgaRawCardDBEnvVar    = "MTGA_RAW_CARD_DB"
)

func parseDraftCardIDs(raw string) []int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return nil
	}

	var ids []int64
	if err := json.Unmarshal([]byte(raw), &ids); err == nil {
		out := make([]int64, 0, len(ids))
		for _, id := range ids {
			if id > 0 {
				out = append(out, id)
			}
		}
		return out
	}

	var stringIDs []string
	if err := json.Unmarshal([]byte(raw), &stringIDs); err == nil {
		out := make([]int64, 0, len(stringIDs))
		for _, rawID := range stringIDs {
			id, err := strconv.ParseInt(strings.TrimSpace(rawID), 10, 64)
			if err == nil && id > 0 {
				out = append(out, id)
			}
		}
		return out
	}

	return nil
}

func uniqueCardIDs(cardIDs []int64) []int64 {
	if len(cardIDs) == 0 {
		return nil
	}

	out := make([]int64, 0, len(cardIDs))
	seen := make(map[int64]struct{}, len(cardIDs))
	for _, cardID := range cardIDs {
		if cardID <= 0 {
			continue
		}
		if _, ok := seen[cardID]; ok {
			continue
		}
		seen[cardID] = struct{}{}
		out = append(out, cardID)
	}
	return out
}

func (s *Server) resolveCardNames(ctx context.Context, cardIDs []int64) map[int64]string {
	cardIDs = uniqueCardIDs(cardIDs)
	if len(cardIDs) == 0 {
		return map[int64]string{}
	}

	resolvedNames, err := s.store.LookupCardNames(ctx, cardIDs)
	if err != nil {
		log.Printf("card name lookup failed: %v", err)
		resolvedNames = map[int64]string{}
	}

	newlyResolved := make(map[int64]string, len(cardIDs))
	unresolved := unresolvedCardIDs(cardIDs, resolvedNames)

	if len(unresolved) > 0 {
		localNames, localErr := s.fetchCardNamesFromMTGARaw(ctx, unresolved)
		if localErr != nil {
			log.Printf("local MTGA card lookup failed: %v", localErr)
		}
		for cardID, name := range localNames {
			trimmed := strings.TrimSpace(name)
			if trimmed == "" {
				continue
			}
			resolvedNames[cardID] = trimmed
			newlyResolved[cardID] = trimmed
		}

		unresolved = unresolvedCardIDs(cardIDs, resolvedNames)
	}

	if len(unresolved) > 0 {
		fetchedNames, fetchErr := s.fetchCardNamesFromScryfall(ctx, unresolved)
		if fetchErr != nil {
			log.Printf("scryfall card name lookup failed: %v", fetchErr)
		}
		for cardID, name := range fetchedNames {
			trimmed := strings.TrimSpace(name)
			if trimmed == "" {
				continue
			}
			resolvedNames[cardID] = trimmed
			newlyResolved[cardID] = trimmed
		}
	}

	if len(newlyResolved) > 0 {
		if err := s.store.UpsertCardNames(ctx, newlyResolved); err != nil {
			log.Printf("card name cache upsert failed: %v", err)
		}
	}

	return resolvedNames
}

func (s *Server) enrichDraftPickCardNames(ctx context.Context, picks []model.DraftPickRow) {
	if len(picks) == 0 {
		return
	}

	allCardIDs := make([]int64, 0, len(picks)*10)
	for i := range picks {
		pickedIDs := parseDraftCardIDs(picks[i].PickedCardIDs)
		packIDs := parseDraftCardIDs(picks[i].PackCardIDs)

		picks[i].PickedCards = make([]model.DraftPickCard, 0, len(pickedIDs))
		for _, cardID := range pickedIDs {
			picks[i].PickedCards = append(picks[i].PickedCards, model.DraftPickCard{CardID: cardID})
			allCardIDs = append(allCardIDs, cardID)
		}

		picks[i].PackCards = make([]model.DraftPickCard, 0, len(packIDs))
		for _, cardID := range packIDs {
			picks[i].PackCards = append(picks[i].PackCards, model.DraftPickCard{CardID: cardID})
			allCardIDs = append(allCardIDs, cardID)
		}
	}

	resolvedNames := s.resolveCardNames(ctx, allCardIDs)
	if len(resolvedNames) == 0 {
		return
	}

	for i := range picks {
		for j := range picks[i].PickedCards {
			cardID := picks[i].PickedCards[j].CardID
			if name, ok := resolvedNames[cardID]; ok {
				picks[i].PickedCards[j].CardName = name
			}
		}

		for j := range picks[i].PackCards {
			cardID := picks[i].PackCards[j].CardID
			if name, ok := resolvedNames[cardID]; ok {
				picks[i].PackCards[j].CardName = name
			}
		}
	}
}

func (s *Server) enrichDeckCardNames(ctx context.Context, cards []model.DeckCardRow) {
	if len(cards) == 0 {
		return
	}

	unique := make(map[int64]struct{}, len(cards))
	missingCardIDs := make([]int64, 0, len(cards))
	for _, card := range cards {
		if strings.TrimSpace(card.CardName) != "" {
			continue
		}
		if _, seen := unique[card.CardID]; seen {
			continue
		}
		unique[card.CardID] = struct{}{}
		missingCardIDs = append(missingCardIDs, card.CardID)
	}
	if len(missingCardIDs) == 0 {
		return
	}

	resolvedNames, err := s.store.LookupCardNames(ctx, missingCardIDs)
	if err != nil {
		log.Printf("card name lookup failed: %v", err)
		resolvedNames = map[int64]string{}
	}
	newlyResolved := make(map[int64]string, len(missingCardIDs))

	unresolved := make([]int64, 0, len(missingCardIDs))
	for _, cardID := range missingCardIDs {
		if _, ok := resolvedNames[cardID]; !ok {
			unresolved = append(unresolved, cardID)
		}
	}

	if len(unresolved) > 0 {
		localNames, localErr := s.fetchCardNamesFromMTGARaw(ctx, unresolved)
		if localErr != nil {
			log.Printf("local MTGA card lookup failed: %v", localErr)
		}
		for cardID, name := range localNames {
			resolvedNames[cardID] = name
			newlyResolved[cardID] = name
		}

		unresolved = unresolvedCardIDs(missingCardIDs, resolvedNames)
	}

	if len(unresolved) > 0 {
		fetchedNames, fetchErr := s.fetchCardNamesFromScryfall(ctx, unresolved)
		if fetchErr != nil {
			log.Printf("scryfall card name lookup failed: %v", fetchErr)
		}
		if len(fetchedNames) > 0 {
			for cardID, name := range fetchedNames {
				resolvedNames[cardID] = name
				newlyResolved[cardID] = name
			}
		}
	}
	if len(newlyResolved) > 0 {
		if err := s.store.UpsertCardNames(ctx, newlyResolved); err != nil {
			log.Printf("card name cache upsert failed: %v", err)
		}
	}

	for i := range cards {
		if strings.TrimSpace(cards[i].CardName) != "" {
			continue
		}
		if name, ok := resolvedNames[cards[i].CardID]; ok {
			cards[i].CardName = name
		}
	}
}

func (s *Server) enrichOpponentObservedCardNames(ctx context.Context, cards []model.OpponentObservedCardRow) {
	if len(cards) == 0 {
		return
	}

	unique := make(map[int64]struct{}, len(cards))
	missingCardIDs := make([]int64, 0, len(cards))
	for _, card := range cards {
		if strings.TrimSpace(card.CardName) != "" {
			continue
		}
		if _, seen := unique[card.CardID]; seen {
			continue
		}
		unique[card.CardID] = struct{}{}
		missingCardIDs = append(missingCardIDs, card.CardID)
	}
	if len(missingCardIDs) == 0 {
		return
	}

	resolvedNames, err := s.store.LookupCardNames(ctx, missingCardIDs)
	if err != nil {
		log.Printf("card name lookup failed: %v", err)
		resolvedNames = map[int64]string{}
	}
	newlyResolved := make(map[int64]string, len(missingCardIDs))

	unresolved := make([]int64, 0, len(missingCardIDs))
	for _, cardID := range missingCardIDs {
		if _, ok := resolvedNames[cardID]; !ok {
			unresolved = append(unresolved, cardID)
		}
	}

	if len(unresolved) > 0 {
		localNames, localErr := s.fetchCardNamesFromMTGARaw(ctx, unresolved)
		if localErr != nil {
			log.Printf("local MTGA card lookup failed: %v", localErr)
		}
		for cardID, name := range localNames {
			resolvedNames[cardID] = name
			newlyResolved[cardID] = name
		}

		unresolved = unresolvedCardIDs(missingCardIDs, resolvedNames)
	}

	if len(unresolved) > 0 {
		fetchedNames, fetchErr := s.fetchCardNamesFromScryfall(ctx, unresolved)
		if fetchErr != nil {
			log.Printf("scryfall card name lookup failed: %v", fetchErr)
		}
		for cardID, name := range fetchedNames {
			resolvedNames[cardID] = name
			newlyResolved[cardID] = name
		}
	}

	if len(newlyResolved) > 0 {
		if err := s.store.UpsertCardNames(ctx, newlyResolved); err != nil {
			log.Printf("card name cache upsert failed: %v", err)
		}
	}

	for i := range cards {
		if strings.TrimSpace(cards[i].CardName) != "" {
			continue
		}
		if name, ok := resolvedNames[cards[i].CardID]; ok {
			cards[i].CardName = name
		}
	}
}

func (s *Server) enrichMatchCardPlayNames(ctx context.Context, plays []model.MatchCardPlayRow) {
	if len(plays) == 0 {
		return
	}

	unique := make(map[int64]struct{}, len(plays))
	missingCardIDs := make([]int64, 0, len(plays))
	for _, play := range plays {
		if strings.TrimSpace(play.CardName) != "" {
			continue
		}
		if _, seen := unique[play.CardID]; seen {
			continue
		}
		unique[play.CardID] = struct{}{}
		missingCardIDs = append(missingCardIDs, play.CardID)
	}
	if len(missingCardIDs) == 0 {
		return
	}

	resolvedNames, err := s.store.LookupCardNames(ctx, missingCardIDs)
	if err != nil {
		log.Printf("card name lookup failed: %v", err)
		resolvedNames = map[int64]string{}
	}
	newlyResolved := make(map[int64]string, len(missingCardIDs))

	unresolved := make([]int64, 0, len(missingCardIDs))
	for _, cardID := range missingCardIDs {
		if _, ok := resolvedNames[cardID]; !ok {
			unresolved = append(unresolved, cardID)
		}
	}

	if len(unresolved) > 0 {
		localNames, localErr := s.fetchCardNamesFromMTGARaw(ctx, unresolved)
		if localErr != nil {
			log.Printf("local MTGA card lookup failed: %v", localErr)
		}
		for cardID, name := range localNames {
			resolvedNames[cardID] = name
			newlyResolved[cardID] = name
		}

		unresolved = unresolvedCardIDs(missingCardIDs, resolvedNames)
	}

	if len(unresolved) > 0 {
		fetchedNames, fetchErr := s.fetchCardNamesFromScryfall(ctx, unresolved)
		if fetchErr != nil {
			log.Printf("scryfall card name lookup failed: %v", fetchErr)
		}
		for cardID, name := range fetchedNames {
			resolvedNames[cardID] = name
			newlyResolved[cardID] = name
		}
	}

	if len(newlyResolved) > 0 {
		if err := s.store.UpsertCardNames(ctx, newlyResolved); err != nil {
			log.Printf("card name cache upsert failed: %v", err)
		}
	}

	for i := range plays {
		if strings.TrimSpace(plays[i].CardName) != "" {
			continue
		}
		if name, ok := resolvedNames[plays[i].CardID]; ok {
			plays[i].CardName = name
		}
	}
}

func (s *Server) enrichMatchReplayNames(ctx context.Context, frames []model.MatchReplayFrameRow) {
	if len(frames) == 0 {
		return
	}

	unique := make(map[int64]struct{})
	missingCardIDs := make([]int64, 0)
	for i := range frames {
		for _, obj := range frames[i].Objects {
			if strings.TrimSpace(obj.CardName) != "" {
				continue
			}
			if _, seen := unique[obj.CardID]; seen {
				continue
			}
			unique[obj.CardID] = struct{}{}
			missingCardIDs = append(missingCardIDs, obj.CardID)
		}
		for _, change := range frames[i].Changes {
			if strings.TrimSpace(change.CardName) != "" {
				continue
			}
			if _, seen := unique[change.CardID]; seen {
				continue
			}
			unique[change.CardID] = struct{}{}
			missingCardIDs = append(missingCardIDs, change.CardID)
		}
	}
	if len(missingCardIDs) == 0 {
		return
	}

	resolvedNames, err := s.store.LookupCardNames(ctx, missingCardIDs)
	if err != nil {
		log.Printf("card name lookup failed: %v", err)
		resolvedNames = map[int64]string{}
	}
	newlyResolved := make(map[int64]string, len(missingCardIDs))

	unresolved := make([]int64, 0, len(missingCardIDs))
	for _, cardID := range missingCardIDs {
		if _, ok := resolvedNames[cardID]; !ok {
			unresolved = append(unresolved, cardID)
		}
	}

	if len(unresolved) > 0 {
		localNames, localErr := s.fetchCardNamesFromMTGARaw(ctx, unresolved)
		if localErr != nil {
			log.Printf("local MTGA card lookup failed: %v", localErr)
		}
		for cardID, name := range localNames {
			resolvedNames[cardID] = name
			newlyResolved[cardID] = name
		}
		unresolved = unresolvedCardIDs(missingCardIDs, resolvedNames)
	}

	if len(unresolved) > 0 {
		fetchedNames, fetchErr := s.fetchCardNamesFromScryfall(ctx, unresolved)
		if fetchErr != nil {
			log.Printf("scryfall card name lookup failed: %v", fetchErr)
		}
		for cardID, name := range fetchedNames {
			resolvedNames[cardID] = name
			newlyResolved[cardID] = name
		}
	}

	if len(newlyResolved) > 0 {
		if err := s.store.UpsertCardNames(ctx, newlyResolved); err != nil {
			log.Printf("card name cache upsert failed: %v", err)
		}
	}

	for i := range frames {
		for j := range frames[i].Objects {
			if strings.TrimSpace(frames[i].Objects[j].CardName) != "" {
				continue
			}
			if name, ok := resolvedNames[frames[i].Objects[j].CardID]; ok {
				frames[i].Objects[j].CardName = name
			}
		}
		for j := range frames[i].Changes {
			if strings.TrimSpace(frames[i].Changes[j].CardName) != "" {
				continue
			}
			if name, ok := resolvedNames[frames[i].Changes[j].CardID]; ok {
				frames[i].Changes[j].CardName = name
			}
		}
	}
}

func unresolvedCardIDs(cardIDs []int64, resolved map[int64]string) []int64 {
	unresolved := make([]int64, 0, len(cardIDs))
	for _, cardID := range cardIDs {
		if _, ok := resolved[cardID]; !ok {
			unresolved = append(unresolved, cardID)
		}
	}
	return unresolved
}

func (s *Server) fetchCardNamesFromMTGARaw(ctx context.Context, cardIDs []int64) (map[int64]string, error) {
	out := make(map[int64]string, len(cardIDs))
	if len(cardIDs) == 0 {
		return out, nil
	}

	rawDBPath := discoverMTGARawCardDBPath()
	if strings.TrimSpace(rawDBPath) == "" {
		return out, nil
	}

	rawDB, err := sql.Open("sqlite", rawDBPath)
	if err != nil {
		return nil, fmt.Errorf("open MTGA raw card db %q: %w", rawDBPath, err)
	}
	defer rawDB.Close()
	rawDB.SetMaxOpenConns(1)
	rawDB.SetMaxIdleConns(1)

	if err := rawDB.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping MTGA raw card db %q: %w", rawDBPath, err)
	}

	placeholders := make([]string, 0, len(cardIDs))
	args := make([]any, 0, len(cardIDs))
	for _, id := range cardIDs {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}

	query := fmt.Sprintf(`
		SELECT
			c.GrpId,
			COALESCE(
				NULLIF(TRIM(l1.Loc), ''),
				NULLIF(TRIM(l2.Loc), ''),
				NULLIF(TRIM(l3.Loc), '')
			) AS name
		FROM Cards c
		LEFT JOIN Localizations_enUS l1 ON l1.LocId = c.TitleId
		LEFT JOIN Localizations_enUS l2 ON l2.LocId = c.AltTitleId
		LEFT JOIN Localizations_enUS l3 ON l3.LocId = c.InterchangeableTitleId
		WHERE c.GrpId IN (%s)
	`, strings.Join(placeholders, ","))

	rows, err := rawDB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query MTGA raw card db: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var cardID int64
		var name string
		if err := rows.Scan(&cardID, &name); err != nil {
			return nil, fmt.Errorf("scan MTGA raw card row: %w", err)
		}
		name = strings.TrimSpace(name)
		if cardID <= 0 || name == "" {
			continue
		}
		out[cardID] = name
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate MTGA raw card rows: %w", err)
	}

	return out, nil
}

func discoverMTGARawCardDBPath() string {
	explicit := strings.TrimSpace(os.Getenv(mtgaRawCardDBEnvVar))
	if explicit != "" {
		if fi, err := os.Stat(explicit); err == nil && !fi.IsDir() {
			return explicit
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	patterns := []string{
		filepath.Join(home, "Library", "Application Support", "com.wizards.mtga", "Downloads", "Raw", "Raw_CardDatabase*.mtga"),
		filepath.Join(home, "AppData", "LocalLow", "Wizards Of The Coast", "MTGA", "Downloads", "Raw", "Raw_CardDatabase*.mtga"),
	}

	var newestPath string
	var newestMod time.Time
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, match := range matches {
			fi, err := os.Stat(match)
			if err != nil || fi.IsDir() {
				continue
			}
			if newestPath == "" || fi.ModTime().After(newestMod) {
				newestPath = match
				newestMod = fi.ModTime()
			}
		}
	}

	return newestPath
}

func (s *Server) fetchCardNamesFromScryfall(ctx context.Context, cardIDs []int64) (map[int64]string, error) {
	out := make(map[int64]string, len(cardIDs))
	if len(cardIDs) == 0 {
		return out, nil
	}

	var firstErr error
	for start := 0; start < len(cardIDs); start += scryfallSearchBatchMax {
		end := min(start+scryfallSearchBatchMax, len(cardIDs))
		batchNames, err := s.fetchCardNameBatch(ctx, cardIDs[start:end])
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for cardID, name := range batchNames {
			out[cardID] = name
		}
	}
	return out, firstErr
}

func (s *Server) fetchCardNameBatch(ctx context.Context, cardIDs []int64) (map[int64]string, error) {
	type responseCard struct {
		ArenaID int64  `json:"arena_id"`
		Name    string `json:"name"`
	}
	type responsePayload struct {
		Data     []responseCard `json:"data"`
		HasMore  bool           `json:"has_more"`
		NextPage string         `json:"next_page"`
	}

	if len(cardIDs) == 0 {
		return map[int64]string{}, nil
	}

	terms := make([]string, 0, len(cardIDs))
	for _, cardID := range cardIDs {
		terms = append(terms, fmt.Sprintf("arenaid:%d", cardID))
	}

	query := strings.Join(terms, " or ")
	searchURL := fmt.Sprintf("%s?q=%s&unique=cards", scryfallSearchURL, url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build scryfall request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "mtgdata/0.1 (local tracker)")

	res, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request scryfall: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotFound {
		return map[int64]string{}, nil
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
		return nil, fmt.Errorf("scryfall status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}

	var decoded responsePayload
	if err := json.NewDecoder(res.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode scryfall response: %w", err)
	}

	names := make(map[int64]string, len(decoded.Data))
	addCards := func(cards []responseCard) {
		for _, card := range cards {
			if card.ArenaID <= 0 || strings.TrimSpace(card.Name) == "" {
				continue
			}
			names[card.ArenaID] = card.Name
		}
	}
	addCards(decoded.Data)

	nextPage := decoded.NextPage
	for decoded.HasMore && strings.TrimSpace(nextPage) != "" {
		nextReq, err := http.NewRequestWithContext(ctx, http.MethodGet, nextPage, nil)
		if err != nil {
			return names, fmt.Errorf("build scryfall next page request: %w", err)
		}
		nextReq.Header.Set("Accept", "application/json")
		nextReq.Header.Set("User-Agent", "mtgdata/0.1 (local tracker)")

		nextRes, err := s.httpClient.Do(nextReq)
		if err != nil {
			return names, fmt.Errorf("request scryfall next page: %w", err)
		}

		var nextDecoded responsePayload
		if nextRes.StatusCode >= 200 && nextRes.StatusCode < 300 {
			err = json.NewDecoder(nextRes.Body).Decode(&nextDecoded)
		} else {
			body, _ := io.ReadAll(io.LimitReader(nextRes.Body, 1024))
			err = fmt.Errorf("scryfall next page status %d: %s", nextRes.StatusCode, strings.TrimSpace(string(body)))
		}
		nextRes.Body.Close()
		if err != nil {
			return names, err
		}
		addCards(nextDecoded.Data)
		decoded = nextDecoded
		nextPage = nextDecoded.NextPage
	}
	return names, nil
}
