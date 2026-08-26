package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/solean/ponder/internal/model"
)

const rawCardLookupBatchMax = 900
const deckColorSplashThreshold = int64(2)

var deckColorOrder = []string{"W", "U", "B", "R", "G"}

var mtgaRawColorMap = map[string]string{
	"1": "W",
	"2": "U",
	"3": "B",
	"4": "R",
	"5": "G",
}

func (s *Server) enrichMatchDeckColors(ctx context.Context, matches []model.MatchRow) {
	if len(matches) == 0 {
		return
	}

	matchIDs := make([]int64, 0, len(matches))
	for i := range matches {
		if matches[i].ID > 0 {
			matchIDs = append(matchIDs, matches[i].ID)
		}
		matches[i].DeckColors = nil
		matches[i].DeckColorsKnown = false
		matches[i].OpponentDeckColors = nil
		matches[i].OpponentDeckColorsKnown = false
	}

	deckCardQuantitiesByMatch, err := s.store.ListMatchDeckCardQuantities(ctx, matchIDs)
	if err != nil {
		log.Printf("match deck color lookup failed: %v", err)
		deckCardQuantitiesByMatch = map[int64]map[int64]int64{}
	}

	opponentCardQuantitiesByMatch, err := s.store.ListMatchOpponentCardQuantities(ctx, matchIDs)
	if err != nil {
		log.Printf("opponent deck color lookup failed: %v", err)
		opponentCardQuantitiesByMatch = map[int64]map[int64]int64{}
	}

	allCardIDs := make([]int64, 0)
	for _, cardQuantities := range deckCardQuantitiesByMatch {
		for cardID := range cardQuantities {
			allCardIDs = append(allCardIDs, cardID)
		}
	}
	for _, cardQuantities := range opponentCardQuantitiesByMatch {
		for cardID := range cardQuantities {
			allCardIDs = append(allCardIDs, cardID)
		}
	}

	colorIdentityByCardID := s.resolveCardColorIdentities(ctx, allCardIDs)
	for i := range matches {
		matches[i].DeckColors, matches[i].DeckColorsKnown = matchColorsForCardQuantities(deckCardQuantitiesByMatch[matches[i].ID], colorIdentityByCardID)
		matches[i].OpponentDeckColors, matches[i].OpponentDeckColorsKnown = matchColorsForCardQuantities(opponentCardQuantitiesByMatch[matches[i].ID], colorIdentityByCardID)
	}
}

func matchColorsForCardQuantities(cardQuantities map[int64]int64, colorIdentityByCardID map[int64][]string) ([]string, bool) {
	if len(cardQuantities) == 0 {
		return nil, false
	}

	colorsSeen := make(map[string]struct{}, len(deckColorOrder))
	colorTotals := make(map[string]int64, len(deckColorOrder))
	resolvedAny := false
	for cardID, quantity := range cardQuantities {
		if quantity <= 0 {
			continue
		}
		colorIdentity, ok := colorIdentityByCardID[cardID]
		if !ok {
			continue
		}
		resolvedAny = true
		for _, color := range colorIdentity {
			colorsSeen[color] = struct{}{}
			colorTotals[color] += quantity
		}
	}

	if !resolvedAny {
		return nil, false
	}

	out := make([]string, 0, len(colorsSeen))
	for _, color := range deckColorOrder {
		if colorTotals[color] > deckColorSplashThreshold {
			out = append(out, color)
		}
	}

	if len(out) == 0 && len(colorsSeen) > 0 {
		for _, color := range deckColorOrder {
			if _, ok := colorsSeen[color]; ok {
				out = append(out, color)
			}
		}
	}

	return out, true
}

func (s *Server) resolveCardColorIdentities(ctx context.Context, cardIDs []int64) map[int64][]string {
	cardIDs = uniqueCardIDs(cardIDs)
	if len(cardIDs) == 0 {
		return map[int64][]string{}
	}

	resolved := make(map[int64][]string, len(cardIDs))

	localColors, err := s.fetchCardColorIdentitiesFromMTGARaw(ctx, cardIDs)
	if err != nil {
		log.Printf("local MTGA card color lookup failed: %v", err)
	}
	for cardID, colors := range localColors {
		resolved[cardID] = colors
	}

	unresolved := make([]int64, 0, len(cardIDs))
	for _, cardID := range cardIDs {
		if _, ok := resolved[cardID]; !ok {
			unresolved = append(unresolved, cardID)
		}
	}

	if len(unresolved) > 0 {
		fetchedColors, fetchErr := s.fetchCardColorIdentitiesFromScryfall(ctx, unresolved)
		if fetchErr != nil {
			logScryfallError("scryfall card color lookup failed: %v", fetchErr)
		}
		for cardID, colors := range fetchedColors {
			resolved[cardID] = colors
		}
	}

	return resolved
}

func (s *Server) fetchCardColorIdentitiesFromMTGARaw(ctx context.Context, cardIDs []int64) (map[int64][]string, error) {
	out := make(map[int64][]string, len(cardIDs))
	cardIDs = uniqueCardIDs(cardIDs)
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

	for start := 0; start < len(cardIDs); start += rawCardLookupBatchMax {
		end := min(start+rawCardLookupBatchMax, len(cardIDs))
		batch := cardIDs[start:end]

		placeholders := make([]string, 0, len(batch))
		args := make([]any, 0, len(batch))
		for _, cardID := range batch {
			placeholders = append(placeholders, "?")
			args = append(args, cardID)
		}

		query := fmt.Sprintf(`
			SELECT GrpId, COALESCE(ColorIdentity, '')
			FROM Cards
			WHERE GrpId IN (%s)
		`, strings.Join(placeholders, ","))

		rows, err := rawDB.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("query MTGA raw card colors: %w", err)
		}

		for rows.Next() {
			var cardID int64
			var rawColorIdentity string
			if err := rows.Scan(&cardID, &rawColorIdentity); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan MTGA raw card color row: %w", err)
			}
			out[cardID] = parseMTGARawColorIdentity(rawColorIdentity)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("iterate MTGA raw card color rows: %w", err)
		}
		rows.Close()
	}

	return out, nil
}

func (s *Server) fetchCardColorIdentitiesFromScryfall(ctx context.Context, cardIDs []int64) (map[int64][]string, error) {
	out := make(map[int64][]string, len(cardIDs))
	if len(cardIDs) == 0 {
		return out, nil
	}

	var firstErr error
	for start := 0; start < len(cardIDs); start += scryfallSearchBatchMax {
		end := min(start+scryfallSearchBatchMax, len(cardIDs))
		batchColors, err := s.fetchCardColorBatch(ctx, cardIDs[start:end])
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for cardID, colors := range batchColors {
			out[cardID] = colors
		}
	}

	return out, firstErr
}

func (s *Server) fetchCardColorBatch(ctx context.Context, cardIDs []int64) (map[int64][]string, error) {
	type responseCard struct {
		ArenaID       int64    `json:"arena_id"`
		ColorIdentity []string `json:"color_identity"`
	}
	type responsePayload struct {
		Data     []responseCard `json:"data"`
		HasMore  bool           `json:"has_more"`
		NextPage string         `json:"next_page"`
	}

	out := make(map[int64][]string, len(cardIDs))
	if len(cardIDs) == 0 {
		return out, nil
	}

	terms := make([]string, 0, len(cardIDs))
	for _, cardID := range cardIDs {
		terms = append(terms, fmt.Sprintf("arenaid:%d", cardID))
	}

	query := strings.Join(terms, " or ")
	searchURL := fmt.Sprintf("%s?q=%s&unique=cards", scryfallSearchURL, url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build scryfall color request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ponder/0.1 (local tracker)")

	res, err := s.doScryfallRequest(req)
	if err != nil {
		return nil, fmt.Errorf("request scryfall colors: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotFound {
		return out, nil
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
		return nil, fmt.Errorf("scryfall color status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}

	var decoded responsePayload
	if err := json.NewDecoder(res.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode scryfall color response: %w", err)
	}

	addCards := func(cards []responseCard) {
		for _, card := range cards {
			if card.ArenaID <= 0 {
				continue
			}
			out[card.ArenaID] = normalizeDeckColors(card.ColorIdentity)
		}
	}
	addCards(decoded.Data)

	nextPage := decoded.NextPage
	for decoded.HasMore && strings.TrimSpace(nextPage) != "" {
		nextReq, err := http.NewRequestWithContext(ctx, http.MethodGet, nextPage, nil)
		if err != nil {
			return out, fmt.Errorf("build scryfall color next page request: %w", err)
		}
		nextReq.Header.Set("Accept", "application/json")
		nextReq.Header.Set("User-Agent", "ponder/0.1 (local tracker)")

		nextRes, err := s.doScryfallRequest(nextReq)
		if err != nil {
			return out, fmt.Errorf("request scryfall color next page: %w", err)
		}

		var nextDecoded responsePayload
		if nextRes.StatusCode >= 200 && nextRes.StatusCode < 300 {
			err = json.NewDecoder(nextRes.Body).Decode(&nextDecoded)
		} else {
			body, _ := io.ReadAll(io.LimitReader(nextRes.Body, 1024))
			err = fmt.Errorf("scryfall color next page status %d: %s", nextRes.StatusCode, strings.TrimSpace(string(body)))
		}
		nextRes.Body.Close()
		if err != nil {
			return out, err
		}

		addCards(nextDecoded.Data)
		decoded = nextDecoded
		nextPage = nextDecoded.NextPage
	}

	return out, nil
}

func parseMTGARawColorIdentity(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return make([]string, 0)
	}

	parts := strings.Split(raw, ",")
	colors := make([]string, 0, len(parts))
	for _, part := range parts {
		color, ok := mtgaRawColorMap[strings.TrimSpace(part)]
		if ok {
			colors = append(colors, color)
		}
	}
	return normalizeDeckColors(colors)
}

func normalizeDeckColors(colors []string) []string {
	if len(colors) == 0 {
		return make([]string, 0)
	}

	seen := make(map[string]struct{}, len(colors))
	for _, color := range colors {
		normalized := strings.ToUpper(strings.TrimSpace(color))
		if normalized == "" {
			continue
		}
		seen[normalized] = struct{}{}
	}

	out := make([]string, 0, len(seen))
	for _, color := range deckColorOrder {
		if _, ok := seen[color]; ok {
			out = append(out, color)
		}
	}
	return out
}
