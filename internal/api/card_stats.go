package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/solean/ponder/internal/db"
	"github.com/solean/ponder/internal/model"
)

const (
	seventeenLandsCardDataURL = "https://www.17lands.com/api/card_data"
	seventeenLandsCardPageURL = "https://www.17lands.com/card_data"
	// Ratings move slowly once a format settles; one fetch per set and
	// format per day keeps load on 17Lands negligible.
	cardStatsTTL = 24 * time.Hour
	// A failed refresh is not retried sooner, so an outage costs one slow
	// request rather than one per page load.
	cardStatsRetryDelay = 15 * time.Minute
	// A full set is ~200 KB; anything far larger is not a card list.
	cardStatsMaxBody = 16 << 20
)

// seventeenLandsEventTypes maps an Arena draft type, with the set and date
// tokens removed and the rest lowercased ("PremierDraft_FRA_20260929" ->
// "premierdraft", "FIN_Quick_Draft" -> "quickdraft"), to the 17Lands
// event_type. Exact matches only: other events have no comparable ratings.
var seventeenLandsEventTypes = map[string]string{
	"premierdraft":     "PremierDraft",
	"traddraft":        "TradDraft",
	"traditionaldraft": "TradDraft",
	"quickdraft":       "QuickDraft",
	"picktwodraft":     "PickTwoDraft",
}

// seventeenLandsFormat resolves an Arena event name to a 17Lands expansion
// and event type; ok is false for events 17Lands has no draft ratings for.
func seventeenLandsFormat(eventName string) (expansion, eventType string, ok bool) {
	expansion = model.LimitedSetCode(eventName)
	if expansion == "" {
		return "", "", false
	}
	var kind strings.Builder
	for _, token := range strings.Split(eventName, "_") {
		if token == expansion || eventDateToken(token) {
			continue
		}
		kind.WriteString(strings.ToLower(token))
	}
	eventType, ok = seventeenLandsEventTypes[kind.String()]
	if !ok {
		return "", "", false
	}
	return expansion, eventType, true
}

func eventDateToken(token string) bool {
	if len(token) != 8 {
		return false
	}
	for _, r := range token {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func seventeenLandsPageURL(expansion, eventType, userGroup string) string {
	query := url.Values{"expansion": {expansion}, "format": {eventType}}
	if userGroup != "" {
		query.Set("user_group", userGroup)
	}
	return seventeenLandsCardPageURL + "?" + query.Encode()
}

// cardStatsSettings returns the 17Lands player group and the event type
// override chosen in Settings; both are empty by default.
func (s *Server) cardStatsSettings() (userGroup, format string) {
	if s.appState == nil {
		return "", ""
	}
	config := s.appState.Config()
	return config.SeventeenLandsUserGroup, config.SeventeenLandsFormat
}

// handleCardStats returns 17Lands card ratings for the draft format of
// ?event=<Arena event name>, keyed by Arena card ID. The configured format
// replaces the event's own for any rated draft, and the configured player
// group narrows the ratings where 17Lands splits them.
func (s *Server) handleCardStats(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/card-stats" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	eventName := strings.TrimSpace(r.URL.Query().Get("event"))
	if eventName == "" {
		writeError(w, http.StatusBadRequest, "event is required")
		return
	}

	expansion, eventType, ok := seventeenLandsFormat(eventName)
	if !ok {
		writeJSON(w, http.StatusOK, model.CardStatsResponse{
			Status: model.CardStatsStatusUnsupported,
			Cards:  []model.CardStat{},
		})
		return
	}
	userGroup, format := s.cardStatsSettings()
	if format != "" {
		eventType = format
	}
	// 17Lands has no player groups for Quick Draft (every group comes back
	// with zero games), so its ratings always cover all players.
	if eventType == "QuickDraft" {
		userGroup = ""
	}

	snapshot, stale, err := s.resolveCardStats(r.Context(), expansion, eventType, userGroup)
	if err != nil {
		log.Printf("17Lands card stats %s %s %q: %v", expansion, eventType, userGroup, err)
		writeError(w, http.StatusBadGateway, "17Lands card ratings are unavailable right now")
		return
	}

	cards := snapshot.Cards
	if cards == nil {
		cards = []model.CardStat{}
	}
	writeJSON(w, http.StatusOK, model.CardStatsResponse{
		Status:         model.CardStatsStatusOK,
		Expansion:      expansion,
		Format:         eventType,
		UserGroup:      userGroup,
		FetchedAt:      snapshot.FetchedAt.UTC().Format(time.RFC3339),
		Stale:          stale,
		SourceURL:      seventeenLandsPageURL(expansion, eventType, userGroup),
		MeanGIHWinRate: meanGIHWinRate(cards),
		Cards:          cards,
	})
}

type cardStatsEntry struct {
	snapshot *db.CardStatSnapshot
	// After a failed refresh, 17Lands is not asked again until retryAt.
	retryAt time.Time
}

// resolveCardStats serves the cached snapshot while it is fresh and refreshes
// it otherwise. A failed refresh falls back to the cached copy (stale=true);
// it is an error only when nothing is cached.
func (s *Server) resolveCardStats(ctx context.Context, expansion, eventType, userGroup string) (*db.CardStatSnapshot, bool, error) {
	// One lock covers lookup and refresh, so concurrent page loads share a
	// single 17Lands request instead of each fetching.
	s.cardStatsMu.Lock()
	defer s.cardStatsMu.Unlock()
	if s.cardStats == nil {
		s.cardStats = make(map[string]*cardStatsEntry)
	}
	key := expansion + "/" + eventType + "/" + userGroup
	entry := s.cardStats[key]
	if entry == nil {
		snapshot, err := s.store.LookupCardStatSnapshot(ctx, expansion, eventType, userGroup)
		if err != nil {
			return nil, false, err
		}
		entry = &cardStatsEntry{snapshot: snapshot}
		s.cardStats[key] = entry
	}
	if entry.snapshot != nil && time.Since(entry.snapshot.FetchedAt) < cardStatsTTL {
		return entry.snapshot, false, nil
	}
	if time.Now().Before(entry.retryAt) {
		if entry.snapshot != nil {
			return entry.snapshot, true, nil
		}
		return nil, false, fmt.Errorf("17Lands request backing off until %s", entry.retryAt.Format(time.RFC3339))
	}

	cards, err := s.fetchSeventeenLandsCardStats(ctx, expansion, eventType, userGroup)
	if err != nil {
		entry.retryAt = time.Now().Add(cardStatsRetryDelay)
		if entry.snapshot != nil {
			log.Printf("refresh 17Lands card stats %s %s %q, serving cached copy: %v", expansion, eventType, userGroup, err)
			return entry.snapshot, true, nil
		}
		return nil, false, err
	}
	fetched := &db.CardStatSnapshot{Cards: cards, FetchedAt: time.Now()}
	s.cardStats[key] = &cardStatsEntry{snapshot: fetched}
	// The write can queue behind log ingest on the store's single writer
	// slot; the in-memory entry already serves later requests.
	go func() {
		writeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := s.store.UpsertCardStatSnapshot(writeCtx, expansion, eventType, userGroup, fetched.Cards, fetched.FetchedAt); err != nil {
			log.Printf("cache 17Lands card stats %s %s %q: %v", expansion, eventType, userGroup, err)
		}
	}()
	return fetched, false, nil
}

func (s *Server) fetchSeventeenLandsCardStats(ctx context.Context, expansion, eventType, userGroup string) ([]model.CardStat, error) {
	query := url.Values{
		"expansion":   {expansion},
		"event_type":  {eventType},
		"time_period": {"ALL_TIME"},
	}
	if userGroup != "" {
		query.Set("user_group", userGroup)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, seventeenLandsCardDataURL+"?"+query.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("build 17Lands request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ponder/0.1 (personal MTGA tracker; daily cached card ratings)")

	res, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request 17Lands card data: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
		return nil, fmt.Errorf("17Lands card data status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}

	var decoded struct {
		Data []struct {
			MTGAID                  int64    `json:"mtga_id"`
			SeenCount               int64    `json:"seen_count"`
			AvgSeen                 *float64 `json:"avg_seen"`
			PickCount               int64    `json:"pick_count"`
			AvgPick                 *float64 `json:"avg_pick"`
			GameCount               int64    `json:"game_count"`
			WinRate                 *float64 `json:"win_rate"`
			EverDrawnGameCount      int64    `json:"ever_drawn_game_count"`
			EverDrawnWinRate        *float64 `json:"ever_drawn_win_rate"`
			DrawnImprovementWinRate *float64 `json:"drawn_improvement_win_rate"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, cardStatsMaxBody)).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode 17Lands card data: %w", err)
	}
	if decoded.Data == nil {
		return nil, fmt.Errorf("17Lands card data response has no data list")
	}

	cards := make([]model.CardStat, 0, len(decoded.Data))
	for _, row := range decoded.Data {
		if row.MTGAID <= 0 {
			continue
		}
		cards = append(cards, model.CardStat{
			ArenaID:      row.MTGAID,
			SeenCount:    row.SeenCount,
			ALSA:         row.AvgSeen,
			PickCount:    row.PickCount,
			ATA:          row.AvgPick,
			GameCount:    row.GameCount,
			GPWinRate:    row.WinRate,
			GIHGameCount: row.EverDrawnGameCount,
			GIHWinRate:   row.EverDrawnWinRate,
			IWD:          row.DrawnImprovementWinRate,
		})
	}
	return cards, nil
}

// meanGIHWinRate is the GIH win rate over every rated card weighted by its
// GIH games, the baseline a single card's GIH WR is read against.
func meanGIHWinRate(cards []model.CardStat) *float64 {
	var wins, games float64
	for _, card := range cards {
		if card.GIHWinRate == nil || card.GIHGameCount <= 0 {
			continue
		}
		games += float64(card.GIHGameCount)
		wins += *card.GIHWinRate * float64(card.GIHGameCount)
	}
	if games == 0 {
		return nil
	}
	mean := wins / games
	return &mean
}
