package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/solean/ponder/internal/db"
	"github.com/solean/ponder/internal/model"
)

func TestSeventeenLandsFormat(t *testing.T) {
	cases := []struct {
		event     string
		expansion string
		eventType string
	}{
		{"PremierDraft_FRA_20260929", "FRA", "PremierDraft"},
		{"QuickDraft_HOB_20260820", "HOB", "QuickDraft"},
		{"TradDraft_FRA_20260929", "FRA", "TradDraft"},
		{"FIN_Quick_Draft", "FIN", "QuickDraft"},
		{"PickTwoDraft_FRA_20261001", "FRA", "PickTwoDraft"},
		// Formats without comparable draft ratings must not borrow another format's.
		{"BotDraft_FRA_20260929", "", ""},
		{"PremierSealed_FRA_20260929", "", ""},
		{"CubeDraft_20260929", "", ""},
		{"Ladder", "", ""},
	}
	for _, tc := range cases {
		expansion, eventType, ok := seventeenLandsFormat(tc.event)
		if ok != (tc.eventType != "") || expansion != tc.expansion || eventType != tc.eventType {
			t.Errorf("%s: got (%q, %q, %v), want (%q, %q)", tc.event, expansion, eventType, ok, tc.expansion, tc.eventType)
		}
	}
}

func TestCardStatsCachesAndFallsBack(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(filepath.Join(t.TempDir(), "card-stats.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.Init(ctx, database); err != nil {
		t.Fatal(err)
	}
	store := db.NewStore(database)
	server := NewServer(store, "", nil)

	requests := 0
	upstreamStatus := http.StatusOK
	var requestedURL string
	server.httpClient = &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		requestedURL = req.URL.String()
		body := `{"copyright":"x","data":[
			{"mtga_id":101,"seen_count":10,"avg_seen":3.5,"pick_count":4,"avg_pick":2.25,"game_count":300,"win_rate":0.55,"ever_drawn_game_count":100,"ever_drawn_win_rate":0.60,"drawn_improvement_win_rate":0.04},
			{"mtga_id":102,"seen_count":10,"avg_seen":null,"pick_count":1,"avg_pick":null,"game_count":5,"win_rate":null,"ever_drawn_game_count":300,"ever_drawn_win_rate":0.50,"drawn_improvement_win_rate":null},
			{"mtga_id":103,"seen_count":0,"avg_seen":null,"pick_count":0,"avg_pick":null,"game_count":0,"win_rate":null,"ever_drawn_game_count":0,"ever_drawn_win_rate":null,"drawn_improvement_win_rate":null}
		]}`
		if upstreamStatus != http.StatusOK {
			body = "unavailable"
		}
		return &http.Response{StatusCode: upstreamStatus, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: req}, nil
	})}
	handler := server.Handler()

	get := func(event string) (int, model.CardStatsResponse) {
		t.Helper()
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, localRequest(http.MethodGet, "/api/card-stats?event="+event, nil))
		var body model.CardStatsResponse
		if rec.Code == http.StatusOK {
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode: %v: %s", err, rec.Body.String())
			}
		}
		return rec.Code, body
	}

	code, body := get("PremierDraft_FRA_20260929")
	if code != http.StatusOK || body.Status != model.CardStatsStatusOK || body.Stale {
		t.Fatalf("first load: %d %+v", code, body)
	}
	if !strings.Contains(requestedURL, "expansion=FRA") || !strings.Contains(requestedURL, "event_type=PremierDraft") {
		t.Fatalf("requested %s", requestedURL)
	}
	fraCards := body.Cards
	if len(fraCards) != 3 || fraCards[0].ArenaID != 101 || fraCards[0].ATA == nil || *fraCards[0].ATA != 2.25 {
		t.Fatalf("cards: %+v", fraCards)
	}
	// Withheld small-sample rates stay null rather than becoming 0%.
	if fraCards[1].ATA != nil || fraCards[1].GPWinRate != nil {
		t.Fatalf("null rates not preserved: %+v", fraCards[1])
	}
	// (0.60*100 + 0.50*300) / 400; the unrated card is excluded.
	if body.MeanGIHWinRate == nil || *body.MeanGIHWinRate < 0.5249 || *body.MeanGIHWinRate > 0.5251 {
		t.Fatalf("mean GIH WR: %v", body.MeanGIHWinRate)
	}

	if _, _ = get("PremierDraft_FRA_20260929"); requests != 1 {
		t.Fatalf("fresh snapshot refetched: %d requests", requests)
	}
	if code, body = get("BotDraft_FRA_20260929"); code != http.StatusOK || body.Status != model.CardStatsStatusUnsupported || requests != 1 {
		t.Fatalf("unsupported: %d %+v after %d requests", code, body, requests)
	}

	// An expired snapshot from an earlier run is refreshed; if 17Lands fails,
	// the old copy is served as stale and 17Lands is not retried immediately.
	upstreamStatus = http.StatusServiceUnavailable
	expired := time.Now().Add(-cardStatsTTL - time.Minute)
	if err := store.UpsertCardStatSnapshot(ctx, "FRA", "TradDraft", fraCards, expired); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		code, body = get("TradDraft_FRA_20260929")
		if code != http.StatusOK || !body.Stale || len(body.Cards) != 3 || requests != 2 {
			t.Fatalf("stale fallback: %d %+v after %d requests", code, body, requests)
		}
	}

	// Nothing cached and 17Lands down: an error, not an empty "ok".
	if code, _ = get("QuickDraft_HOB_20260820"); code != http.StatusBadGateway || requests != 3 {
		t.Fatalf("uncached failure: %d after %d requests", code, requests)
	}
}
