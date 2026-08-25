package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/solean/ponder/internal/db"
	"github.com/solean/ponder/internal/model"
)

// writeFixtureRawCardDB creates a minimal Arena Raw_CardDatabase with the
// tables and columns the catalog extraction reads.
func writeFixtureRawCardDB(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "Raw_CardDatabase_fixture.mtga")
	rawDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open fixture raw db: %v", err)
	}
	defer rawDB.Close()

	statements := []string{
		`CREATE TABLE Versions (Type TEXT NOT NULL PRIMARY KEY, Version TEXT NOT NULL)`,
		`INSERT INTO Versions (Type, Version) VALUES ('Data', 'test.1.0'), ('GRP', '1.1')`,
		`CREATE TABLE Localizations_enUS (LocId INT NOT NULL, Formatted INT NOT NULL DEFAULT 0, Loc TEXT)`,
		`CREATE TABLE Cards (
			GrpId INT NOT NULL PRIMARY KEY,
			TitleId INT NOT NULL,
			AltTitleId INT NOT NULL DEFAULT 0,
			InterchangeableTitleId INT NOT NULL DEFAULT 0,
			TypeTextId INT NOT NULL DEFAULT 0,
			SubtypeTextId INT NOT NULL DEFAULT 0,
			Rarity INT NOT NULL DEFAULT 0,
			ExpansionCode TEXT,
			CollectorNumber TEXT,
			OldSchoolManaText TEXT,
			Colors TEXT,
			ColorIdentity TEXT,
			Order_CMCWithXLast INT,
			IsToken BOOLEAN NOT NULL DEFAULT 0,
			IsPrimaryCard BOOLEAN NOT NULL DEFAULT 1,
			IsDigitalOnly BOOLEAN NOT NULL DEFAULT 0,
			IsRebalanced BOOLEAN NOT NULL DEFAULT 0
		)`,
		`INSERT INTO Localizations_enUS (LocId, Formatted, Loc) VALUES
			(1, 0, 'Lightning Strike'),
			(1, 1, '<nobr>Lightning Strike</nobr>'),
			(2, 0, 'Instant'),
			(3, 0, 'Forest'),
			(4, 0, 'Basic Land'),
			(5, 0, 'Forest'),
			(6, 0, 'Hydroid Krasis'),
			(7, 0, 'Legendary Creature'),
			(8, 0, 'Jellyfish Hydra Beast'),
			(9, 0, 'Alt Art Strike')`,
		// Two Lightning Strike printings, a basic land, an X spell, plus a
		// token and a non-primary printing that extraction must skip.
		`INSERT INTO Cards (
			GrpId, TitleId, TypeTextId, SubtypeTextId, Rarity, ExpansionCode, CollectorNumber,
			OldSchoolManaText, Colors, ColorIdentity, Order_CMCWithXLast,
			IsToken, IsPrimaryCard, IsDigitalOnly, IsRebalanced
		) VALUES
			(101, 1, 2, 0, 2, 'DMU', '137', 'o1oR', '4', '4', 2, 0, 1, 0, 0),
			(202, 1, 2, 0, 2, 'M25', '12', 'o1oR', '4', '4', 2, 0, 1, 0, 0),
			(401, 3, 4, 5, 1, 'DMU', '277', '', '', '5', 0, 0, 1, 0, 0),
			(601, 6, 7, 8, 4, 'RNA', '183', 'oXoGoU', '2,5', '2,5', 1002, 0, 1, 0, 0),
			(701, 1, 2, 0, 2, 'DMU', 'T1', 'o1oR', '4', '4', 2, 1, 1, 0, 0),
			(801, 9, 2, 0, 2, 'DMU', '137a', 'o1oR', '4', '4', 2, 0, 0, 0, 0)`,
	}
	for _, statement := range statements {
		if _, err := rawDB.Exec(statement); err != nil {
			t.Fatalf("fixture exec: %v\n%s", err, statement)
		}
	}
	return path
}

func newDeckBuilderTestServer(t *testing.T) *Server {
	t.Helper()

	t.Setenv(mtgaRawCardDBEnvVar, writeFixtureRawCardDB(t))

	ctx := context.Background()
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})
	if err := db.Init(ctx, database); err != nil {
		t.Fatalf("init db: %v", err)
	}
	return NewServer(db.NewStore(database), "", nil)
}

func doJSON(t *testing.T, server *Server, method, path string, body string, out any) *httptest.ResponseRecorder {
	t.Helper()

	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if out != nil && rec.Code < 300 {
		if err := json.NewDecoder(rec.Body).Decode(out); err != nil {
			t.Fatalf("decode %s %s response: %v", method, path, err)
		}
	}
	return rec
}

func TestCardSearchEndpointExtractsCatalog(t *testing.T) {
	server := newDeckBuilderTestServer(t)

	var result model.CardSearchResult
	rec := doJSON(t, server, http.MethodGet, "/api/cards?q=lightning", "", &result)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String())
	}
	if result.Total != 1 || len(result.Cards) != 1 {
		t.Fatalf("result = %+v", result)
	}
	card := result.Cards[0]
	if card.ArenaID != 202 || card.ManaCost != "{1}{R}" || card.TypeLine != "Instant" || card.Rarity != "common" {
		t.Fatalf("card = %+v", card)
	}

	// Token (701) and non-primary (801) rows must not be extracted; the X
	// spell keeps its real mana value.
	var hydroid model.CardSearchResult
	doJSON(t, server, http.MethodGet, "/api/cards?q=hydroid", "", &hydroid)
	if len(hydroid.Cards) != 1 {
		t.Fatalf("hydroid result = %+v", hydroid)
	}
	if hydroid.Cards[0].ManaValue == nil || *hydroid.Cards[0].ManaValue != 2 {
		t.Fatalf("hydroid mana value = %v, want 2", hydroid.Cards[0].ManaValue)
	}
	if hydroid.Cards[0].TypeLine != "Legendary Creature \u2014 Jellyfish Hydra Beast" {
		t.Fatalf("hydroid type line = %q", hydroid.Cards[0].TypeLine)
	}

	var altArt model.CardSearchResult
	doJSON(t, server, http.MethodGet, "/api/cards?q=alt+art", "", &altArt)
	if altArt.Total != 0 {
		t.Fatalf("non-primary card leaked into catalog: %+v", altArt)
	}
}

func TestDeckProjectEndpointsCRUDAndImportExport(t *testing.T) {
	server := newDeckBuilderTestServer(t)

	// Create.
	var created model.DeckProject
	rec := doJSON(t, server, http.MethodPost, "/api/deck-projects",
		`{"name":"Test Deck","format":"standard","cards":[{"section":"main","arenaId":202,"quantity":4}]}`, &created)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d; body: %s", rec.Code, rec.Body.String())
	}
	if created.ID <= 0 || len(created.Cards) != 1 {
		t.Fatalf("created = %+v", created)
	}
	// Cards hydrate even though the catalog sync has not run yet for this
	// project; trigger a search so the catalog exists for later steps.
	doJSON(t, server, http.MethodGet, "/api/cards?q=x", "", nil)

	// Update via PUT.
	var saved model.DeckProject
	rec = doJSON(t, server, http.MethodPut, fmt.Sprintf("/api/deck-projects/%d", created.ID),
		`{"name":"Test Deck v2","format":"standard","cards":[{"section":"main","arenaId":202,"quantity":4},{"section":"sideboard","arenaId":601,"quantity":2}]}`, &saved)
	if rec.Code != http.StatusOK {
		t.Fatalf("save status = %d; body: %s", rec.Code, rec.Body.String())
	}
	if saved.Name != "Test Deck v2" || len(saved.Cards) != 2 {
		t.Fatalf("saved = %+v", saved)
	}

	// List.
	var summaries []model.DeckProjectSummary
	rec = doJSON(t, server, http.MethodGet, "/api/deck-projects", "", &summaries)
	if rec.Code != http.StatusOK || len(summaries) != 1 {
		t.Fatalf("list status = %d, summaries = %+v", rec.Code, summaries)
	}
	if summaries[0].MainCount != 4 || summaries[0].SideboardCount != 2 {
		t.Fatalf("summary counts = %+v", summaries[0])
	}

	// Export.
	var export model.DeckProjectExport
	rec = doJSON(t, server, http.MethodGet, fmt.Sprintf("/api/deck-projects/%d/export", created.ID), "", &export)
	if rec.Code != http.StatusOK {
		t.Fatalf("export status = %d; body: %s", rec.Code, rec.Body.String())
	}
	wantExport := "Deck\n4 Lightning Strike (M25) 12\n\nSideboard\n2 Hydroid Krasis (RNA) 183\n"
	if export.Text != wantExport {
		t.Fatalf("export = %q, want %q", export.Text, wantExport)
	}
	if len(export.Unresolved) != 0 {
		t.Fatalf("export unresolved = %v", export.Unresolved)
	}

	// Import: exact printing via set/collector, name-only resolution, and an
	// unresolvable card.
	var imported model.DeckProjectImportResult
	rec = doJSON(t, server, http.MethodPost, "/api/deck-projects/import",
		`{"text":"About\nName Imported Burn\n\nDeck\n4 Lightning Strike (DMU) 137\n8 Forest\n2 Black Lotus\n\nSideboard\n1 Hydroid Krasis\n"}`, &imported)
	if rec.Code != http.StatusCreated {
		t.Fatalf("import status = %d; body: %s", rec.Code, rec.Body.String())
	}
	if imported.Project.Name != "Imported Burn" {
		t.Fatalf("imported name = %q", imported.Project.Name)
	}
	if len(imported.Unresolved) != 1 || imported.Unresolved[0] != "2 Black Lotus" {
		t.Fatalf("unresolved = %v", imported.Unresolved)
	}
	cardsBySection := map[string]map[int64]int64{}
	for _, card := range imported.Project.Cards {
		if cardsBySection[card.Section] == nil {
			cardsBySection[card.Section] = map[int64]int64{}
		}
		cardsBySection[card.Section][card.ArenaID] = card.Quantity
	}
	if cardsBySection["main"][101] != 4 {
		t.Fatalf("import should keep the requested DMU printing: %+v", cardsBySection)
	}
	if cardsBySection["main"][401] != 8 {
		t.Fatalf("forest missing: %+v", cardsBySection)
	}
	if cardsBySection["sideboard"][601] != 1 {
		t.Fatalf("sideboard krasis missing: %+v", cardsBySection)
	}

	// Delete.
	rec = doJSON(t, server, http.MethodDelete, fmt.Sprintf("/api/deck-projects/%d", created.ID), "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d; body: %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, server, http.MethodGet, fmt.Sprintf("/api/deck-projects/%d", created.ID), "", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get deleted status = %d", rec.Code)
	}
}
