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

	"github.com/solean/ponder/internal/model"
)

const scryfallSetURL = "https://api.scryfall.com/sets"

// handleSets resolves friendly metadata (name + icon) for the set codes passed
// as ?codes=tmt,fin,ecl. Unknown codes are simply omitted from the response.
func (s *Server) handleSets(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/sets" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	codes := parseSetCodes(r.URL.Query().Get("codes"))
	if len(codes) == 0 {
		writeJSON(w, http.StatusOK, map[string]model.SetInfo{})
		return
	}

	writeJSON(w, http.StatusOK, s.resolveSets(r.Context(), codes))
}

func parseSetCodes(raw string) []string {
	seen := make(map[string]struct{})
	codes := make([]string, 0)
	for _, part := range strings.Split(raw, ",") {
		code := strings.ToLower(strings.TrimSpace(part))
		if code == "" {
			continue
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		codes = append(codes, code)
	}
	return codes
}

// resolveSets returns set metadata, reading from the local cache first and
// falling back to Scryfall for any misses (which are then cached).
func (s *Server) resolveSets(ctx context.Context, codes []string) map[string]model.SetInfo {
	resolved, err := s.store.LookupSets(ctx, codes)
	if err != nil {
		log.Printf("set lookup failed: %v", err)
		resolved = map[string]model.SetInfo{}
	}

	newlyResolved := make(map[string]model.SetInfo)
	for _, code := range codes {
		if _, ok := resolved[code]; ok {
			continue
		}
		info, fetchErr := s.fetchSetFromScryfall(ctx, code)
		if fetchErr != nil {
			logScryfallError(fmt.Sprintf("scryfall set lookup failed for %q: %%v", code), fetchErr)
			continue
		}
		if info == nil {
			continue
		}
		resolved[code] = *info
		newlyResolved[code] = *info
	}

	if len(newlyResolved) > 0 {
		if err := s.store.UpsertSets(ctx, newlyResolved); err != nil {
			log.Printf("set cache upsert failed: %v", err)
		}
	}

	return resolved
}

// fetchSetFromScryfall looks up a single set by code. A 404 (unknown code)
// returns (nil, nil) so callers can skip it without treating it as an error.
func (s *Server) fetchSetFromScryfall(ctx context.Context, code string) (*model.SetInfo, error) {
	requestURL := fmt.Sprintf("%s/%s", scryfallSetURL, url.PathEscape(code))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build scryfall set request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ponder/0.1 (local tracker)")

	res, err := s.doScryfallRequest(req)
	if err != nil {
		return nil, fmt.Errorf("request scryfall set: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
		return nil, fmt.Errorf("scryfall set status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}

	var decoded struct {
		Code       string `json:"code"`
		Name       string `json:"name"`
		IconSvgURI string `json:"icon_svg_uri"`
		ReleasedAt string `json:"released_at"`
	}
	if err := json.NewDecoder(res.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode scryfall set response: %w", err)
	}
	if strings.TrimSpace(decoded.Name) == "" {
		return nil, nil
	}

	return &model.SetInfo{
		Code:       code,
		Name:       strings.TrimSpace(decoded.Name),
		IconSvgURI: strings.TrimSpace(decoded.IconSvgURI),
		ReleasedAt: strings.TrimSpace(decoded.ReleasedAt),
	}, nil
}
