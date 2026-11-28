package api

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/solean/ponder/internal/db"
	"github.com/solean/ponder/internal/version"
)

const (
	scryfallCollectionURL = "https://api.scryfall.com/cards/collection"
	// scryfallCollectionMax is the most identifiers /cards/collection takes.
	scryfallCollectionMax = 75
	// scryfallDefaultCardsURL describes the default_cards bulk file: every
	// printing in English, about 80 MB gzipped, about 20k of them on Arena.
	scryfallDefaultCardsURL = "https://api.scryfall.com/bulk-data/default-cards"
	// cardPreviewBulkTimeout bounds downloading and storing the bulk file.
	cardPreviewBulkTimeout = 30 * time.Minute
	// cardPreviewBulkBatch is how many cards each write transaction stores.
	cardPreviewBulkBatch = 500
	// cardPreviewSyncInterval is how old the stored copy of every Arena card
	// can get before it is downloaded again. New cards in between are fetched
	// one batch at a time when a preview asks for them.
	cardPreviewSyncInterval = 7 * 24 * time.Hour
	// cardPreviewSyncRetry is the wait after a failed or interrupted download.
	cardPreviewSyncRetry = time.Hour
	// cardPreviewMissRetryInterval keeps a card Scryfall couldn't find from
	// being asked about on every hover.
	cardPreviewMissRetryInterval = time.Hour
	// cardPreviewRequestMax bounds one /api/cards/previews request.
	cardPreviewRequestMax = 1000
)

// cardPreviewRequest names a card by Arena ID, by name, or both. Settings
// samples have a name but no Arena ID.
type cardPreviewRequest struct {
	CardID int64  `json:"cardId"`
	Name   string `json:"name"`
}

type cardPreviewResponse struct {
	Name        string   `json:"name"`
	ImageURL    string   `json:"imageUrl"`
	ArtCropURL  string   `json:"artCropUrl,omitempty"`
	ScryfallURL string   `json:"scryfallUrl,omitempty"`
	ManaCost    string   `json:"manaCost,omitempty"`
	ManaValue   *float64 `json:"manaValue,omitempty"`
	TypeLine    string   `json:"typeLine,omitempty"`
	Rarity      string   `json:"rarity,omitempty"`
	Colors      []string `json:"colors,omitempty"`
}

// handleCardPreviews answers POST {"cards":[{cardId,name}]} with
// {"previews":[preview|null]} in the same order.
func (s *Server) handleCardPreviews(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body struct {
		Cards []cardPreviewRequest `json:"cards"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(body.Cards) > cardPreviewRequestMax {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("at most %d cards per request", cardPreviewRequestMax))
		return
	}

	resolved := s.resolveCardPreviews(r.Context(), body.Cards)
	out := make([]*cardPreviewResponse, len(resolved))
	for i, preview := range resolved {
		if preview == nil {
			continue
		}
		out[i] = &cardPreviewResponse{
			Name:        preview.Name,
			ImageURL:    preview.ImageURL,
			ArtCropURL:  preview.ArtCropURL,
			ScryfallURL: preview.ScryfallURL,
			ManaCost:    preview.ManaCost,
			ManaValue:   preview.ManaValue,
			TypeLine:    preview.TypeLine,
			Rarity:      preview.Rarity,
			Colors:      strings.Split(preview.Colors, ""),
		}
		if preview.Colors == "" {
			out[i].Colors = nil
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"previews": out})
}

// resolveCardPreviews returns a preview (or nil) per request. It reads the
// stored copy first, so cards already seen work offline, then asks Scryfall by
// Arena ID, then by name, and stores what it finds under the requested ID.
func (s *Server) resolveCardPreviews(ctx context.Context, requests []cardPreviewRequest) []*db.CardPreview {
	out := make([]*db.CardPreview, len(requests))
	ids := make([]int64, 0, len(requests))
	for _, req := range requests {
		if req.CardID > 0 {
			ids = append(ids, req.CardID)
		}
	}
	stored, err := s.store.LookupCardPreviews(ctx, ids)
	if err != nil {
		log.Printf("card preview lookup failed: %v", err)
		stored = map[int64]db.CardPreview{}
	}

	// Pending holds request indexes still unanswered after each source.
	pending := make([]int, 0)
	for i, req := range requests {
		if p, ok := stored[req.CardID]; ok && req.CardID > 0 {
			out[i] = &p
			continue
		}
		if req.CardID > 0 || db.NormalizeCardPreviewName(req.Name) != "" {
			pending = append(pending, i)
		}
	}
	if len(pending) == 0 {
		return out
	}

	found := make(map[int64]db.CardPreview)
	// answer fills request i and queues the preview to be stored under its ID.
	answer := func(i int, p db.CardPreview) {
		if requests[i].CardID > 0 {
			p.ArenaID = requests[i].CardID
			found[p.ArenaID] = p
		}
		out[i] = &p
	}
	unanswered := func() []int {
		next := pending[:0:0]
		for _, i := range pending {
			if out[i] == nil {
				next = append(next, i)
			}
		}
		return next
	}

	// Scryfall by Arena ID gives the exact printing.
	var askIDs []int64
	for _, i := range pending {
		if id := requests[i].CardID; id > 0 && !s.cardPreviewMissedRecently(fmt.Sprintf("id:%d", id)) {
			askIDs = append(askIDs, id)
		}
	}
	if askIDs = uniqueCardIDs(askIDs); len(askIDs) > 0 {
		byID, failed, err := s.fetchCardPreviewsByArenaID(ctx, askIDs)
		if err != nil {
			logScryfallError("scryfall card preview lookup failed: %v", err)
		}
		for _, i := range pending {
			if p, ok := byID[requests[i].CardID]; ok {
				answer(i, p)
			}
		}
		failedSet := make(map[int64]struct{}, len(failed))
		for _, id := range failed {
			failedSet[id] = struct{}{}
		}
		for _, id := range askIDs {
			if _, ok := byID[id]; !ok {
				if _, errored := failedSet[id]; !errored {
					s.recordCardPreviewMiss(fmt.Sprintf("id:%d", id))
				}
			}
		}
	}

	// Another stored printing with the same name, then Scryfall by name.
	if pending = unanswered(); len(pending) > 0 {
		names := make([]string, 0, len(pending))
		for _, i := range pending {
			names = append(names, requests[i].Name)
		}
		byName, err := s.store.LookupCardPreviewsByName(ctx, names)
		if err != nil {
			log.Printf("card preview name lookup failed: %v", err)
		}
		for _, i := range pending {
			if p, ok := byName[db.NormalizeCardPreviewName(requests[i].Name)]; ok {
				answer(i, p)
			}
		}
	}
	if pending = unanswered(); len(pending) > 0 {
		var askNames []string
		seen := make(map[string]struct{})
		for _, i := range pending {
			key := db.NormalizeCardPreviewName(requests[i].Name)
			if key == "" || s.cardPreviewMissedRecently("name:"+key) {
				continue
			}
			if _, ok := seen[key]; !ok {
				seen[key] = struct{}{}
				askNames = append(askNames, strings.TrimSpace(requests[i].Name))
			}
		}
		if len(askNames) > 0 {
			byName, failed, err := s.fetchCardPreviewsByName(ctx, askNames)
			if err != nil {
				logScryfallError("scryfall card preview name lookup failed: %v", err)
			}
			for _, i := range pending {
				if p, ok := byName[db.NormalizeCardPreviewName(requests[i].Name)]; ok {
					answer(i, p)
				}
			}
			failedSet := make(map[string]struct{}, len(failed))
			for _, name := range failed {
				failedSet[db.NormalizeCardPreviewName(name)] = struct{}{}
			}
			for key := range seen {
				if _, ok := byName[key]; ok {
					continue
				}
				if _, errored := failedSet[key]; !errored {
					s.recordCardPreviewMiss("name:" + key)
				}
			}
		}
	}

	if len(found) > 0 {
		previews := make([]db.CardPreview, 0, len(found))
		for _, p := range found {
			previews = append(previews, p)
		}
		if err := s.store.UpsertCardPreviews(ctx, previews); err != nil {
			log.Printf("card preview cache upsert failed: %v", err)
		}
	}
	return out
}

func (s *Server) cardPreviewMissedRecently(key string) bool {
	s.cardPreviewMissMu.Lock()
	defer s.cardPreviewMissMu.Unlock()
	missedAt, ok := s.cardPreviewMisses[key]
	return ok && time.Since(missedAt) < cardPreviewMissRetryInterval
}

func (s *Server) recordCardPreviewMiss(key string) {
	s.cardPreviewMissMu.Lock()
	defer s.cardPreviewMissMu.Unlock()
	if s.cardPreviewMisses == nil {
		s.cardPreviewMisses = make(map[string]time.Time)
	}
	s.cardPreviewMisses[key] = time.Now()
}

// fetchCardPreviewsByArenaID searches Scryfall in batches. failed lists the
// IDs whose batch errored, so a missing answer for them isn't a real miss.
func (s *Server) fetchCardPreviewsByArenaID(ctx context.Context, cardIDs []int64) (map[int64]db.CardPreview, []int64, error) {
	out := make(map[int64]db.CardPreview, len(cardIDs))
	var failed []int64
	var firstErr error
	for start := 0; start < len(cardIDs); start += scryfallSearchBatchMax {
		batch := cardIDs[start:min(start+scryfallSearchBatchMax, len(cardIDs))]
		terms := make([]string, len(batch))
		for i, id := range batch {
			terms[i] = fmt.Sprintf("arenaid:%d", id)
		}
		// unique=prints: two Arena IDs can be printings of the same card.
		searchURL := fmt.Sprintf("%s?q=%s&unique=prints", scryfallSearchURL, url.QueryEscape(strings.Join(terms, " or ")))
		err := s.scryfallSearchPages(ctx, searchURL, func(cards []scryfallPreviewCard) error {
			for _, card := range cards {
				if p, ok := card.preview(); ok && card.ArenaID > 0 {
					out[card.ArenaID] = p
				}
			}
			return nil
		})
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			failed = append(failed, batch...)
		}
	}
	return out, failed, firstErr
}

// fetchCardPreviewsByName asks /cards/collection, keyed by
// NormalizeCardPreviewName. Scryfall picks the printing.
func (s *Server) fetchCardPreviewsByName(ctx context.Context, names []string) (map[string]db.CardPreview, []string, error) {
	out := make(map[string]db.CardPreview, len(names))
	var failed []string
	var firstErr error
	for start := 0; start < len(names); start += scryfallCollectionMax {
		batch := names[start:min(start+scryfallCollectionMax, len(names))]
		cards, err := s.fetchScryfallCollection(ctx, batch)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			failed = append(failed, batch...)
			continue
		}
		for i, card := range cards {
			if card == nil {
				continue
			}
			if p, ok := card.preview(); ok {
				out[db.NormalizeCardPreviewName(batch[i])] = p
			}
		}
	}
	return out, failed, firstErr
}

// fetchScryfallCollection returns one card (or nil) per name, in order.
// Scryfall leaves unknown names out of data and lists them in not_found.
func (s *Server) fetchScryfallCollection(ctx context.Context, names []string) ([]*scryfallPreviewCard, error) {
	type identifier struct {
		Name string `json:"name"`
	}
	identifiers := make([]identifier, len(names))
	for i, name := range names {
		identifiers[i] = identifier{Name: name}
	}
	payload, err := json.Marshal(map[string]any{"identifiers": identifiers})
	if err != nil {
		return nil, fmt.Errorf("encode scryfall collection request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, scryfallCollectionURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build scryfall collection request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", version.UserAgent("local tracker"))

	res, err := s.doScryfallRequest(req)
	if err != nil {
		return nil, fmt.Errorf("request scryfall collection: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
		return nil, fmt.Errorf("scryfall collection status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}

	var decoded struct {
		Data     []scryfallPreviewCard `json:"data"`
		NotFound []identifier          `json:"not_found"`
	}
	if err := json.NewDecoder(res.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode scryfall collection: %w", err)
	}

	missing := make(map[string]int, len(decoded.NotFound))
	for _, id := range decoded.NotFound {
		missing[strings.ToLower(strings.TrimSpace(id.Name))]++
	}
	out := make([]*scryfallPreviewCard, len(names))
	next := 0
	for i, name := range names {
		key := strings.ToLower(strings.TrimSpace(name))
		if missing[key] > 0 {
			missing[key]--
			continue
		}
		if next < len(decoded.Data) {
			out[i] = &decoded.Data[next]
			next++
		}
	}
	return out, nil
}

// scryfallSearchPages runs a search and passes each page of cards to visit,
// following next_page. A 404 means no matches, not an error.
func (s *Server) scryfallSearchPages(ctx context.Context, searchURL string, visit func([]scryfallPreviewCard) error) error {
	next := searchURL
	for next != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, next, nil)
		if err != nil {
			return fmt.Errorf("build scryfall search request: %w", err)
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", version.UserAgent("local tracker"))

		res, err := s.doScryfallRequest(req)
		if err != nil {
			return fmt.Errorf("request scryfall search: %w", err)
		}
		if res.StatusCode == http.StatusNotFound {
			res.Body.Close()
			return nil
		}
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			body, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
			res.Body.Close()
			return fmt.Errorf("scryfall search status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
		}
		var page struct {
			Data     []scryfallPreviewCard `json:"data"`
			HasMore  bool                  `json:"has_more"`
			NextPage string                `json:"next_page"`
		}
		err = json.NewDecoder(res.Body).Decode(&page)
		res.Body.Close()
		if err != nil {
			return fmt.Errorf("decode scryfall search: %w", err)
		}
		if err := visit(page.Data); err != nil {
			return err
		}
		next = ""
		if page.HasMore {
			next = strings.TrimSpace(page.NextPage)
		}
	}
	return nil
}

// StartCardPreviewSync keeps a stored preview for every Arena card,
// downloading them all at launch when the copy is missing or older than
// cardPreviewSyncInterval, and checking again daily.
func (s *Server) StartCardPreviewSync(ctx context.Context) {
	go func() {
		for {
			wait := 24 * time.Hour
			syncedAt, err := s.store.CardPreviewsSyncedAt(ctx)
			if err != nil {
				log.Printf("card preview sync: %v", err)
				wait = cardPreviewSyncRetry
			} else if time.Since(syncedAt) >= cardPreviewSyncInterval {
				if err := s.syncAllCardPreviews(ctx); err != nil {
					if ctx.Err() != nil {
						return
					}
					logScryfallError("card preview sync failed: %v", err)
					wait = cardPreviewSyncRetry
				}
			}
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
}

// syncAllCardPreviews downloads Scryfall's default_cards bulk file and stores
// every card with an Arena ID, a batch at a time, so an interrupted download
// keeps what it got. Only a finished download counts as a sync.
//
// Scryfall requires the bulk files, not paged searches, for loading this many
// cards: the files come from data.scryfall.io, which has no rate limit, so
// this costs one API request.
func (s *Server) syncAllCardPreviews(ctx context.Context) error {
	started := time.Now()
	downloadURL, err := s.cardPreviewBulkURL(ctx)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, cardPreviewBulkTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return fmt.Errorf("build bulk card request: %w", err)
	}
	req.Header.Set("User-Agent", version.UserAgent("local tracker"))
	// s.httpClient's timeout fits API calls, not an ~80 MB download.
	res, err := (&http.Client{Transport: s.httpClient.Transport}).Do(req)
	if err != nil {
		return fmt.Errorf("download bulk cards: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
		return fmt.Errorf("bulk cards status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}

	cards, err := gunzipIfCompressed(res.Body)
	if err != nil {
		return fmt.Errorf("read bulk cards: %w", err)
	}
	stored := 0
	batch := make([]db.CardPreview, 0, cardPreviewBulkBatch)
	flush := func() error {
		if err := s.store.UpsertCardPreviews(ctx, batch); err != nil {
			return err
		}
		stored += len(batch)
		batch = batch[:0]
		return nil
	}
	// The file is JSON Lines; a json.Decoder reads one card per Decode.
	decoder := json.NewDecoder(cards)
	for {
		var card scryfallPreviewCard
		if err := decoder.Decode(&card); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return fmt.Errorf("decode bulk cards after %d stored: %w", stored, err)
		}
		if p, ok := card.preview(); ok && card.ArenaID > 0 {
			batch = append(batch, p)
			if len(batch) == cardPreviewBulkBatch {
				if err := flush(); err != nil {
					return err
				}
			}
		}
	}
	if err := flush(); err != nil {
		return err
	}
	if stored == 0 {
		return errors.New("bulk card file had no Arena cards")
	}
	if err := s.store.SetCardPreviewsSyncedAt(ctx, time.Now()); err != nil {
		return err
	}
	log.Printf("card preview sync: stored %d Arena cards in %s", stored, time.Since(started).Round(time.Second))
	return nil
}

// cardPreviewBulkURL asks Scryfall where today's default_cards file is.
func (s *Server) cardPreviewBulkURL(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, scryfallDefaultCardsURL, nil)
	if err != nil {
		return "", fmt.Errorf("build bulk data request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", version.UserAgent("local tracker"))
	res, err := s.doScryfallRequest(req)
	if err != nil {
		return "", fmt.Errorf("request bulk data: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
		return "", fmt.Errorf("bulk data status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	var bulk struct {
		JSONLDownloadURI string `json:"jsonl_download_uri"`
	}
	if err := json.NewDecoder(res.Body).Decode(&bulk); err != nil {
		return "", fmt.Errorf("decode bulk data: %w", err)
	}
	if uri := strings.TrimSpace(bulk.JSONLDownloadURI); uri != "" {
		return uri, nil
	}
	return "", errors.New("bulk data has no JSON Lines download")
}

// gunzipIfCompressed returns r decompressed when it starts with the gzip
// magic bytes. Scryfall serves .jsonl.gz as application/gzip with no
// Content-Encoding, so the transport doesn't decompress it.
func gunzipIfCompressed(r io.Reader) (io.Reader, error) {
	buffered := bufio.NewReaderSize(r, 64<<10)
	magic, err := buffered.Peek(2)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(magic) == 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		return gzip.NewReader(buffered)
	}
	return buffered, nil
}

type scryfallImageURIs struct {
	PNG    string `json:"png"`
	Large  string `json:"large"`
	Normal string `json:"normal"`
	Small  string `json:"small"`
	// Art keeps the printed art's aspect (a Saga is portrait); ArtCrop is
	// always 626×457, which the fixed-aspect art tiles assume.
	Art     string `json:"art"`
	ArtCrop string `json:"art_crop"`
}

func (u *scryfallImageURIs) card() string {
	if u == nil {
		return ""
	}
	return firstNonEmpty(u.Normal, u.Large, u.Small, u.PNG)
}

func (u *scryfallImageURIs) artCrop() string {
	if u == nil {
		return ""
	}
	return firstNonEmpty(u.ArtCrop, u.Art)
}

type scryfallPreviewFace struct {
	ImageURIs *scryfallImageURIs `json:"image_uris"`
	ManaCost  string             `json:"mana_cost"`
	TypeLine  string             `json:"type_line"`
	Colors    []string           `json:"colors"`
}

type scryfallPreviewCard struct {
	ArenaID     int64                 `json:"arena_id"`
	Name        string                `json:"name"`
	ScryfallURI string                `json:"scryfall_uri"`
	ImageURIs   *scryfallImageURIs    `json:"image_uris"`
	CardFaces   []scryfallPreviewFace `json:"card_faces"`
	ManaCost    *string               `json:"mana_cost"`
	CMC         *float64              `json:"cmc"`
	TypeLine    string                `json:"type_line"`
	Rarity      string                `json:"rarity"`
	Colors      []string              `json:"colors"`
}

// preview maps a Scryfall card to what previews show. Double-faced cards keep
// their images on the faces; the front face wins. ok is false without an
// image, since there is nothing to show.
func (c scryfallPreviewCard) preview() (db.CardPreview, bool) {
	image := c.ImageURIs.card()
	art := c.ImageURIs.artCrop()
	for _, face := range c.CardFaces {
		if image == "" {
			image = face.ImageURIs.card()
		}
		if art == "" {
			art = face.ImageURIs.artCrop()
		}
	}
	name := strings.TrimSpace(c.Name)
	if image == "" || name == "" {
		return db.CardPreview{}, false
	}

	manaCost := ""
	if c.ManaCost != nil {
		manaCost = strings.TrimSpace(*c.ManaCost)
	}
	if manaCost == "" {
		var costs []string
		for _, face := range c.CardFaces {
			if cost := strings.TrimSpace(face.ManaCost); cost != "" {
				costs = append(costs, cost)
			}
		}
		manaCost = strings.Join(costs, " // ")
	}
	typeLine := strings.TrimSpace(c.TypeLine)
	if typeLine == "" {
		var types []string
		for _, face := range c.CardFaces {
			if t := strings.TrimSpace(face.TypeLine); t != "" {
				types = append(types, t)
			}
		}
		typeLine = strings.Join(types, " // ")
	}

	colors := make(map[string]bool)
	for _, color := range c.Colors {
		colors[strings.ToUpper(strings.TrimSpace(color))] = true
	}
	for _, face := range c.CardFaces {
		for _, color := range face.Colors {
			colors[strings.ToUpper(strings.TrimSpace(color))] = true
		}
	}
	var wubrg strings.Builder
	for _, color := range []string{"W", "U", "B", "R", "G"} {
		if colors[color] {
			wubrg.WriteString(color)
		}
	}

	rarity := strings.ToLower(strings.TrimSpace(c.Rarity))
	switch rarity {
	case "common", "uncommon", "rare", "mythic":
	default:
		rarity = ""
	}

	return db.CardPreview{
		ArenaID:     c.ArenaID,
		Name:        name,
		ImageURL:    image,
		ArtCropURL:  art,
		ScryfallURL: stripQuery(c.ScryfallURI),
		ManaCost:    manaCost,
		ManaValue:   c.CMC,
		TypeLine:    typeLine,
		Rarity:      rarity,
		Colors:      wubrg.String(),
	}, true
}

// stripQuery drops Scryfall's utm_source=api tracking suffix.
func stripQuery(raw string) string {
	raw = strings.TrimSpace(raw)
	if before, _, ok := strings.Cut(raw, "?"); ok {
		return before
	}
	return raw
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
