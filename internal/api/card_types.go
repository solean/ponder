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
)

// isLandTypeLine reports whether a Scryfall type line denotes a land. Type lines
// look like "Basic Land — Island" or "Artifact Land"; a substring check is
// enough (and treats e.g. "Land Creature" as a land, which is correct for
// "how likely is my next draw a land").
func isLandTypeLine(typeLine string) bool {
	return strings.Contains(strings.ToLower(typeLine), "land")
}

var basicLandNames = map[string]struct{}{
	"plains": {}, "island": {}, "swamp": {}, "mountain": {}, "forest": {}, "wastes": {},
}

// isBasicLandName recognizes basic lands (including Snow-Covered variants) by
// name. Scryfall's arenaid search doesn't reliably resolve basic-land Arena IDs,
// so we classify them by name to keep land odds accurate.
func isBasicLandName(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	normalized = strings.TrimPrefix(normalized, "snow-covered ")
	_, ok := basicLandNames[normalized]
	return ok
}

// resolveCardTypeLines returns Scryfall type lines for the given cards, reading
// the local cache first and fetching+caching any misses. Mirrors
// resolveCardNames so the live banner's land odds work offline after one fetch.
func (s *Server) resolveCardTypeLines(ctx context.Context, cardIDs []int64) map[int64]string {
	cardIDs = uniqueCardIDs(cardIDs)
	if len(cardIDs) == 0 {
		return map[int64]string{}
	}

	resolved, err := s.store.LookupCardTypeLines(ctx, cardIDs)
	if err != nil {
		log.Printf("card type lookup failed: %v", err)
		resolved = map[int64]string{}
	}

	unresolved := make([]int64, 0, len(cardIDs))
	for _, id := range cardIDs {
		if _, ok := resolved[id]; !ok {
			unresolved = append(unresolved, id)
		}
	}
	if len(unresolved) == 0 {
		return resolved
	}

	fetched, fetchErr := s.fetchCardTypeLinesFromScryfall(ctx, unresolved)
	if fetchErr != nil {
		log.Printf("scryfall card type lookup failed: %v", fetchErr)
	}
	for id, typeLine := range fetched {
		trimmed := strings.TrimSpace(typeLine)
		if trimmed == "" {
			continue
		}
		resolved[id] = trimmed
	}

	if len(fetched) > 0 {
		if err := s.store.UpsertCardTypeLines(ctx, fetched); err != nil {
			log.Printf("card type cache upsert failed: %v", err)
		}
	}

	return resolved
}

func (s *Server) fetchCardTypeLinesFromScryfall(ctx context.Context, cardIDs []int64) (map[int64]string, error) {
	out := make(map[int64]string, len(cardIDs))
	if len(cardIDs) == 0 {
		return out, nil
	}

	var firstErr error
	for start := 0; start < len(cardIDs); start += scryfallSearchBatchMax {
		end := min(start+scryfallSearchBatchMax, len(cardIDs))
		batch, err := s.fetchCardTypeLineBatch(ctx, cardIDs[start:end])
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for cardID, typeLine := range batch {
			out[cardID] = typeLine
		}
	}
	return out, firstErr
}

func (s *Server) fetchCardTypeLineBatch(ctx context.Context, cardIDs []int64) (map[int64]string, error) {
	type responseCard struct {
		ArenaID  int64  `json:"arena_id"`
		TypeLine string `json:"type_line"`
	}
	type responsePayload struct {
		Data     []responseCard `json:"data"`
		HasMore  bool           `json:"has_more"`
		NextPage string         `json:"next_page"`
	}

	out := make(map[int64]string, len(cardIDs))
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
		return nil, fmt.Errorf("build scryfall type request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "mtgdata/0.1 (local tracker)")

	res, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request scryfall types: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotFound {
		return out, nil
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
		return nil, fmt.Errorf("scryfall type status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}

	var decoded responsePayload
	if err := json.NewDecoder(res.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode scryfall type response: %w", err)
	}

	addCards := func(cards []responseCard) {
		for _, card := range cards {
			if card.ArenaID <= 0 || strings.TrimSpace(card.TypeLine) == "" {
				continue
			}
			out[card.ArenaID] = strings.TrimSpace(card.TypeLine)
		}
	}
	addCards(decoded.Data)

	nextPage := decoded.NextPage
	for decoded.HasMore && strings.TrimSpace(nextPage) != "" {
		nextReq, err := http.NewRequestWithContext(ctx, http.MethodGet, nextPage, nil)
		if err != nil {
			return out, fmt.Errorf("build scryfall type next page request: %w", err)
		}
		nextReq.Header.Set("Accept", "application/json")
		nextReq.Header.Set("User-Agent", "mtgdata/0.1 (local tracker)")

		nextRes, err := s.httpClient.Do(nextReq)
		if err != nil {
			return out, fmt.Errorf("request scryfall type next page: %w", err)
		}

		var nextDecoded responsePayload
		if nextRes.StatusCode >= 200 && nextRes.StatusCode < 300 {
			err = json.NewDecoder(nextRes.Body).Decode(&nextDecoded)
		} else {
			body, _ := io.ReadAll(io.LimitReader(nextRes.Body, 1024))
			err = fmt.Errorf("scryfall type next page status %d: %s", nextRes.StatusCode, strings.TrimSpace(string(body)))
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
