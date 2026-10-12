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

// resolveCardTypeLines returns type lines for the given cards, reading the
// local cache first, then the MTGA client's card database, then Scryfall, and
// caching what it finds. The local MTGA source matters for new sets: Scryfall
// lists their cards days before it fills in their Arena IDs.
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

	newlyResolved := make(map[int64]string)
	unresolved := unresolvedCardIDs(cardIDs, resolved)
	if len(unresolved) > 0 {
		local, localErr := s.fetchCardTypeLinesFromMTGARaw(ctx, unresolved)
		if localErr != nil {
			log.Printf("local MTGA card type lookup failed: %v", localErr)
		}
		for id, typeLine := range local {
			resolved[id] = typeLine
			newlyResolved[id] = typeLine
		}
		unresolved = unresolvedCardIDs(unresolved, resolved)
	}

	if len(unresolved) > 0 {
		fetched, fetchErr := s.fetchCardTypeLinesFromScryfall(ctx, unresolved)
		if fetchErr != nil {
			logScryfallError("scryfall card type lookup failed: %v", fetchErr)
		}
		for id, typeLine := range fetched {
			resolved[id] = typeLine
			newlyResolved[id] = typeLine
		}
	}

	if len(newlyResolved) > 0 {
		if err := s.store.UpsertCardTypeLines(ctx, newlyResolved); err != nil {
			log.Printf("card type cache upsert failed: %v", err)
		}
	}

	return resolved
}

// fetchCardTypeLinesFromMTGARaw builds Scryfall-style type lines ("Legendary
// Artifact Creature — Dragon Construct") from the MTGA client's card database.
// Localized strings come in several Formatted variants; 0 is plain text, while
// higher variants may carry markup such as <nobr>.
func (s *Server) fetchCardTypeLinesFromMTGARaw(ctx context.Context, cardIDs []int64) (map[int64]string, error) {
	out := make(map[int64]string, len(cardIDs))
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
			SELECT
				c.GrpId,
				COALESCE((SELECT l.Loc FROM Localizations_enUS l WHERE l.LocId = c.TypeTextId ORDER BY l.Formatted LIMIT 1), ''),
				COALESCE((SELECT l.Loc FROM Localizations_enUS l WHERE l.LocId = c.SubtypeTextId ORDER BY l.Formatted LIMIT 1), '')
			FROM Cards c
			WHERE c.GrpId IN (%s)
		`, strings.Join(placeholders, ","))

		rows, err := rawDB.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("query MTGA raw card types: %w", err)
		}

		for rows.Next() {
			var cardID int64
			var typeText, subtypeText string
			if err := rows.Scan(&cardID, &typeText, &subtypeText); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan MTGA raw card type row: %w", err)
			}
			if typeLine := mtgaTypeLine(typeText, subtypeText); cardID > 0 && typeLine != "" {
				out[cardID] = typeLine
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("iterate MTGA raw card type rows: %w", err)
		}
		rows.Close()
	}

	return out, nil
}

// mtgaTypeLine joins MTGA's separate type and subtype strings the way Scryfall
// formats type_line. A card with no type text is left unresolved.
func mtgaTypeLine(typeText, subtypeText string) string {
	typeText = strings.TrimSpace(typeText)
	subtypeText = strings.TrimSpace(subtypeText)
	if typeText == "" {
		return ""
	}
	if subtypeText == "" {
		return typeText
	}
	return typeText + " — " + subtypeText
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
	req.Header.Set("User-Agent", "ponder/0.1 (local tracker)")

	res, err := s.doScryfallRequest(req)
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
		nextReq.Header.Set("User-Agent", "ponder/0.1 (local tracker)")

		nextRes, err := s.doScryfallRequest(nextReq)
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
