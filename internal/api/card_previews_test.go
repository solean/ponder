package api

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/solean/ponder/internal/db"
)

type scryfallStub func(req *http.Request) (int, string, error)

// newCardPreviewTestServer returns a server whose Scryfall traffic goes to
// respond, with the request pacing removed so tests don't wait between calls.
func newCardPreviewTestServer(t *testing.T, respond scryfallStub) (*Server, *db.Store, *[]*http.Request) {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "ponder.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.Init(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	store := db.NewStore(database)
	server := NewServer(store, "", nil)

	var requests []*http.Request
	server.httpClient = &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		requests = append(requests, req)
		// doScryfallRequest holds its mutex on this goroutine.
		server.scryfallNextRequest = time.Time{}
		status, body, err := respond(req)
		if err != nil {
			return nil, err
		}
		return &http.Response{
			StatusCode: status,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})}
	return server, store, &requests
}

func offline(*http.Request) (int, string, error) {
	return 0, "", errors.New("network is unreachable")
}

const boltJSON = `{
	"arena_id": 101, "name": "Lightning Bolt",
	"scryfall_uri": "https://scryfall.com/card/sta/42/lightning-bolt?utm_source=api",
	"image_uris": {"normal": "https://img/bolt-normal.jpg?1", "art_crop": "https://img/bolt-art.jpg?1"},
	"mana_cost": "{R}", "cmc": 1, "type_line": "Instant", "rarity": "uncommon", "colors": ["R"]
}`

func TestScryfallPreviewMapsDoubleFacedCards(t *testing.T) {
	var card scryfallPreviewCard
	if err := json.Unmarshal([]byte(`{
		"arena_id": 7, "name": "Delver of Secrets // Insectile Aberration",
		"cmc": 1, "rarity": "special",
		"card_faces": [
			{"mana_cost": "{U}", "type_line": "Creature — Human Wizard", "colors": ["U"],
			 "image_uris": {"large": "https://img/front-large.jpg", "art": "https://img/front-art.jpg"}},
			{"mana_cost": "", "type_line": "Creature — Human Insect", "colors": ["W", "U"],
			 "image_uris": {"normal": "https://img/back.jpg", "art_crop": "https://img/back-art.jpg"}}
		]
	}`), &card); err != nil {
		t.Fatal(err)
	}
	got, ok := card.preview()
	if !ok {
		t.Fatal("preview() = not ok")
	}
	if got.ImageURL != "https://img/front-large.jpg" || got.ArtCropURL != "https://img/front-art.jpg" {
		t.Fatalf("images = %q, %q; want the front face's", got.ImageURL, got.ArtCropURL)
	}
	if got.ManaCost != "{U}" || got.TypeLine != "Creature — Human Wizard // Creature — Human Insect" {
		t.Fatalf("cost/type = %q, %q", got.ManaCost, got.TypeLine)
	}
	if got.Colors != "WU" || got.Rarity != "" || got.ManaValue == nil || *got.ManaValue != 1 {
		t.Fatalf("colors/rarity/mv = %q, %q, %v", got.Colors, got.Rarity, got.ManaValue)
	}

	if _, ok := (scryfallPreviewCard{Name: "No Art"}).preview(); ok {
		t.Fatal("a card without an image should have no preview")
	}
}

func TestCardPreviewsAreStoredAndServedOffline(t *testing.T) {
	ctx := context.Background()
	online := true
	server, _, requests := newCardPreviewTestServer(t, func(req *http.Request) (int, string, error) {
		if !online {
			return offline(req)
		}
		if !strings.Contains(req.URL.Query().Get("q"), "arenaid:101") {
			t.Errorf("unexpected Scryfall query %q", req.URL.RawQuery)
		}
		return http.StatusOK, `{"data":[` + boltJSON + `],"has_more":false}`, nil
	})

	got := server.resolveCardPreviews(ctx, []cardPreviewRequest{{CardID: 101, Name: "Lightning Bolt"}})
	if got[0] == nil || got[0].ImageURL != "https://img/bolt-normal.jpg?1" ||
		got[0].ScryfallURL != "https://scryfall.com/card/sta/42/lightning-bolt" {
		t.Fatalf("first lookup = %+v", got[0])
	}

	// A restart with no network still finds the stored copy.
	online = false
	restarted := NewServer(server.store, "", nil)
	restarted.httpClient = server.httpClient
	before := len(*requests)
	got = restarted.resolveCardPreviews(ctx, []cardPreviewRequest{{CardID: 101}})
	if got[0] == nil || got[0].Name != "Lightning Bolt" || got[0].Rarity != "uncommon" {
		t.Fatalf("offline lookup = %+v", got[0])
	}
	if len(*requests) != before {
		t.Fatalf("stored preview still asked Scryfall")
	}

	// Another printing of a stored card is answered by name, offline too.
	got = restarted.resolveCardPreviews(ctx, []cardPreviewRequest{{CardID: 202, Name: "lightning bolt"}})
	if got[0] == nil || got[0].Name != "Lightning Bolt" {
		t.Fatalf("offline name fallback = %+v", got[0])
	}
}

func TestCardPreviewNameFallbackUsesCollectionOrder(t *testing.T) {
	ctx := context.Background()
	server, store, requests := newCardPreviewTestServer(t, func(req *http.Request) (int, string, error) {
		if req.Method == http.MethodGet {
			return http.StatusNotFound, `{"object":"error"}`, nil
		}
		body, _ := io.ReadAll(req.Body)
		if !strings.Contains(string(body), `"Nope"`) {
			t.Errorf("collection body = %s", body)
		}
		return http.StatusOK, `{"not_found":[{"name":"Nope"}],"data":[` +
			strings.Replace(boltJSON, `"arena_id": 101`, `"arena_id": 0`, 1) + `]}`, nil
	})

	got := server.resolveCardPreviews(ctx, []cardPreviewRequest{
		{CardID: 301, Name: "Nope"},
		{CardID: 302, Name: "Lightning Bolt"},
		{Name: "Lightning Bolt"}, // a settings sample: name only
	})
	if got[0] != nil {
		t.Fatalf("unknown card = %+v, want nil", got[0])
	}
	if got[1] == nil || got[1].Name != "Lightning Bolt" || got[2] == nil {
		t.Fatalf("name lookups = %+v, %+v", got[1], got[2])
	}
	stored, err := store.LookupCardPreviews(ctx, []int64{301, 302})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := stored[302]; !ok || len(stored) != 1 {
		t.Fatalf("stored = %v, want only 302", stored)
	}

	// Misses aren't asked about again right away.
	before := len(*requests)
	server.resolveCardPreviews(ctx, []cardPreviewRequest{{CardID: 301, Name: "Nope"}})
	if len(*requests) != before {
		t.Fatalf("recent miss asked Scryfall again (%d requests)", len(*requests)-before)
	}
}

func TestFailedLookupIsNotRememberedAsMiss(t *testing.T) {
	ctx := context.Background()
	online := false
	server, _, _ := newCardPreviewTestServer(t, func(req *http.Request) (int, string, error) {
		if !online {
			return offline(req)
		}
		return http.StatusOK, `{"data":[` + boltJSON + `],"has_more":false}`, nil
	})
	if got := server.resolveCardPreviews(ctx, []cardPreviewRequest{{CardID: 101}}); got[0] != nil {
		t.Fatalf("offline lookup = %+v", got[0])
	}
	online = true
	if got := server.resolveCardPreviews(ctx, []cardPreviewRequest{{CardID: 101}}); got[0] == nil {
		t.Fatal("card wasn't looked up again once the network was back")
	}
}

func TestSyncAllCardPreviewsReadsBulkFile(t *testing.T) {
	ctx := context.Background()
	compact := func(card string) string {
		var buf bytes.Buffer
		if err := json.Compact(&buf, []byte(card)); err != nil {
			t.Fatal(err)
		}
		return buf.String()
	}
	jsonl := strings.Join([]string{
		compact(boltJSON),
		compact(strings.Replace(boltJSON, `"arena_id": 101`, `"arena_id": 102`, 1)),
		`{"name":"Not On Arena","image_uris":{"normal":"https://img/paper.jpg"}}`,
	}, "\n") + "\n"
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	if _, err := zw.Write([]byte(jsonl)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	file := gz.String()

	truncate := true
	var apiRequests []string
	server, store, _ := newCardPreviewTestServer(t, func(req *http.Request) (int, string, error) {
		switch req.URL.Host {
		case "api.scryfall.com":
			apiRequests = append(apiRequests, req.URL.Path)
			return http.StatusOK, `{"object":"bulk_data","type":"default_cards",
				"jsonl_download_uri":"https://data.scryfall.io/default-cards/default-cards-1.jsonl.gz"}`, nil
		case "data.scryfall.io":
			if truncate {
				return http.StatusOK, file[:len(file)/2], nil
			}
			return http.StatusOK, file, nil
		}
		t.Errorf("unexpected request %s", req.URL)
		return http.StatusNotFound, "", nil
	})

	if err := server.syncAllCardPreviews(ctx); err == nil {
		t.Fatal("sync of a truncated file reported success")
	}
	if syncedAt, _ := store.CardPreviewsSyncedAt(ctx); !syncedAt.IsZero() {
		t.Fatalf("truncated sync recorded synced_at %v", syncedAt)
	}

	truncate = false
	apiRequests = nil
	if err := server.syncAllCardPreviews(ctx); err != nil {
		t.Fatal(err)
	}
	if len(apiRequests) != 1 || apiRequests[0] != "/bulk-data/default-cards" {
		t.Fatalf("API requests = %v, want only the bulk data lookup", apiRequests)
	}
	stored, err := store.LookupCardPreviews(ctx, []int64{101, 102})
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 2 || stored[102].ImageURL != "https://img/bolt-normal.jpg?1" {
		t.Fatalf("stored = %+v, want 101 and 102", stored)
	}
	if syncedAt, _ := store.CardPreviewsSyncedAt(ctx); time.Since(syncedAt) > time.Minute {
		t.Fatalf("synced_at = %v", syncedAt)
	}
}

func TestHandleCardPreviews(t *testing.T) {
	server, store, _ := newCardPreviewTestServer(t, offline)
	mv := 1.0
	if err := store.UpsertCardPreviews(context.Background(), []db.CardPreview{{
		ArenaID: 101, Name: "Lightning Bolt", ImageURL: "https://img/bolt.jpg", ManaValue: &mv, Colors: "BR",
	}}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/cards/previews",
		strings.NewReader(`{"cards":[{"cardId":999},{"cardId":101,"name":"Lightning Bolt"}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var body struct {
		Previews []*cardPreviewResponse `json:"previews"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Previews) != 2 || body.Previews[0] != nil || body.Previews[1] == nil {
		t.Fatalf("previews = %+v", body.Previews)
	}
	if got := body.Previews[1]; got.ImageURL != "https://img/bolt.jpg" || strings.Join(got.Colors, "") != "BR" {
		t.Fatalf("preview = %+v", got)
	}
}
