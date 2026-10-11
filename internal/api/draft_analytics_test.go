package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/solean/ponder/internal/db"
	"github.com/solean/ponder/internal/model"
)

func TestDraftAnalyticsEndpoints(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database, err := db.Open(filepath.Join(t.TempDir(), "draft-analytics.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.Init(ctx, database); err != nil {
		t.Fatal(err)
	}
	store := db.NewStore(database)
	tx, err := store.BeginTx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i, event := range []string{"QuickDraft_TMT_20260313", "FIN_Quick_Draft", "Draft", "Sealed_TMT_20260313", "Ladder", "PremierDraft_MKM_20260101"} {
		match, err := store.UpsertMatchStart(ctx, tx, fmt.Sprintf("draft-api-%d", i), event, 1, "2026-07-01T00:00:00Z")
		if err != nil {
			t.Fatal(err)
		}
		if i == 5 { // Available sets include a match even before games arrive.
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO games (match_id, game_number, result, derived_at) VALUES (?, 1, 'win', '')`, match); err != nil {
			t.Fatal(err)
		}
	}
	// Every game sees a basic land and a spell; only the spell is a card to rate.
	if _, err := tx.ExecContext(ctx, `INSERT INTO game_card_stats (game_id, match_id, card_id, opening_kept_copies, played_copies)
		SELECT g.id, g.match_id, card.card_id, 1, 1 FROM games g, (SELECT 101 AS card_id UNION ALL SELECT 102) card`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertCardNames(ctx, map[int64]string{101: "Snow-Covered Island", 102: "Shock"}); err != nil {
		t.Fatal(err)
	}
	handler := NewServer(store, "", nil).Handler()
	for _, tc := range []struct {
		query string
		games int64
	}{
		{"", 3}, {"?set=TMT", 1}, {"?set=FIN", 1}, {"?set=", 1}, {"?set=XYZ", 0}, {"?set=MKM", 0},
	} {
		t.Run(tc.query, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, localRequest(http.MethodGet, "/api/drafts/analytics"+tc.query, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
			var out model.DraftAnalyticsResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			if out.Analytics.GameRecord.Games != tc.games || out.Analytics.DeckID != 0 || !reflect.DeepEqual(out.Sets, []string{"", "FIN", "MKM", "TMT"}) {
				t.Fatalf("response = %+v", out)
			}
			wantCards := []string{}
			if tc.games > 0 {
				wantCards = []string{"Shock"}
			}
			gotCards := []string{}
			for _, card := range out.Analytics.Cards {
				gotCards = append(gotCards, card.CardName)
			}
			if !reflect.DeepEqual(gotCards, wantCards) {
				t.Fatalf("cards = %v, want %v (basic lands excluded)", gotCards, wantCards)
			}
			rec = httptest.NewRecorder()
			handler.ServeHTTP(rec, localRequest(http.MethodGet, "/api/drafts/analytics/games"+tc.query, nil))
			var refs []model.DeckAnalyticsGameRef
			if rec.Code != http.StatusOK {
				t.Fatalf("games status %d: %s", rec.Code, rec.Body.String())
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &refs); err != nil {
				t.Fatal(err)
			}
			if refs == nil || int64(len(refs)) != tc.games {
				t.Fatalf("refs = %+v, want %d games", refs, tc.games)
			}
			for _, ref := range refs {
				if set := draftAnalyticsSet(localRequest(http.MethodGet, "/"+tc.query, nil)); set != nil && model.LimitedSetCode(ref.EventName) != *set {
					t.Fatalf("wrong-set ref = %+v", ref)
				}
			}
		})
	}
	for _, query := range []string{"?facet=played", "?card=101&facet=invalid", "?game=invalid", "?playDraw=invalid", "?landDrops=invalid"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, localRequest(http.MethodGet, "/api/drafts/analytics/games"+query, nil))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status %d, body %s", query, rec.Code, rec.Body.String())
		}
	}
	for _, path := range []string{"/api/drafts/analytics", "/api/drafts/analytics/games"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, localRequest(http.MethodPost, path, nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("POST %s: status %d", path, rec.Code)
		}
	}
}
