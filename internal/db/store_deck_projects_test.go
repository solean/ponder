package db

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/solean/ponder/internal/model"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()

	ctx := context.Background()
	database, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})
	if err := Init(ctx, database); err != nil {
		t.Fatalf("init db: %v", err)
	}
	return NewStore(database)
}

func floatPtr(v float64) *float64 {
	return &v
}

func testCardDefinitions() []CardDefinitionUpsert {
	return []CardDefinitionUpsert{
		{
			CardDefinition: model.CardDefinition{
				ArenaID: 101, Name: "Lightning Strike", SetCode: "DMU", CollectorNumber: "137",
				Rarity: "common", ManaCost: "{1}{R}", ManaValue: floatPtr(2),
				Colors: []string{"R"}, ColorIdentity: []string{"R"},
				TypeLine: "Instant",
			},
			IsPrimary: true,
		},
		{
			CardDefinition: model.CardDefinition{
				ArenaID: 202, Name: "Lightning Strike", SetCode: "M25", CollectorNumber: "12",
				Rarity: "common", ManaCost: "{1}{R}", ManaValue: floatPtr(2),
				Colors: []string{"R"}, ColorIdentity: []string{"R"},
				TypeLine: "Instant",
			},
			IsPrimary: true,
		},
		{
			CardDefinition: model.CardDefinition{
				ArenaID: 301, Name: "Atraxa, Grand Unifier", SetCode: "ONE", CollectorNumber: "196",
				Rarity: "mythic", ManaCost: "{3}{G}{W}{U}{B}", ManaValue: floatPtr(7),
				Colors: []string{"W", "U", "B", "G"}, ColorIdentity: []string{"W", "U", "B", "G"},
				TypeLine: "Legendary Creature \u2014 Phyrexian Angel",
			},
			IsPrimary: true,
		},
		{
			CardDefinition: model.CardDefinition{
				ArenaID: 401, Name: "Forest", SetCode: "DMU", CollectorNumber: "277",
				Rarity: "land", ManaValue: floatPtr(0),
				Colors: []string{}, ColorIdentity: []string{"G"},
				TypeLine: "Basic Land \u2014 Forest",
			},
			IsPrimary: true,
		},
		{
			CardDefinition: model.CardDefinition{
				ArenaID: 501, Name: "Spell Pierce", SetCode: "NEO", CollectorNumber: "80",
				Rarity: "common", ManaCost: "{U}", ManaValue: floatPtr(1),
				Colors: []string{"U"}, ColorIdentity: []string{"U"},
				TypeLine: "Instant",
			},
			IsPrimary: true,
		},
	}
}

func TestReplaceAndSearchCardDefinitions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newTestStore(t)

	if err := store.ReplaceCardDefinitions(ctx, "", nil); err == nil {
		t.Fatalf("ReplaceCardDefinitions with empty set should fail")
	}

	if err := store.ReplaceCardDefinitions(ctx, "2026.62.0.241", testCardDefinitions()); err != nil {
		t.Fatalf("ReplaceCardDefinitions: %v", err)
	}

	version, err := store.CardDefinitionsVersion(ctx)
	if err != nil {
		t.Fatalf("CardDefinitionsVersion: %v", err)
	}
	if version != "2026.62.0.241" {
		t.Fatalf("version = %q, want 2026.62.0.241", version)
	}

	count, err := store.CountCardDefinitions(ctx)
	if err != nil {
		t.Fatalf("CountCardDefinitions: %v", err)
	}
	if count != 5 {
		t.Fatalf("count = %d, want 5", count)
	}

	// Duplicate printings collapse to one result: the highest Arena ID.
	cards, total, err := store.SearchCardDefinitions(ctx, CardSearchParams{Query: "lightning"})
	if err != nil {
		t.Fatalf("search by name: %v", err)
	}
	if total != 1 || len(cards) != 1 {
		t.Fatalf("lightning search: total=%d len=%d, want 1/1", total, len(cards))
	}
	if cards[0].ArenaID != 202 {
		t.Fatalf("lightning printing = %d, want 202 (newest)", cards[0].ArenaID)
	}

	// Substring match.
	cards, _, err = store.SearchCardDefinitions(ctx, CardSearchParams{Query: "grand uni"})
	if err != nil {
		t.Fatalf("substring search: %v", err)
	}
	if len(cards) != 1 || cards[0].Name != "Atraxa, Grand Unifier" {
		t.Fatalf("substring search returned %+v", cards)
	}

	// Color subset: selecting U excludes multicolor Atraxa and mono-red, but
	// keeps colorless-cost basics and mono-blue.
	cards, _, err = store.SearchCardDefinitions(ctx, CardSearchParams{Colors: []string{"U"}})
	if err != nil {
		t.Fatalf("color search: %v", err)
	}
	gotNames := map[string]bool{}
	for _, card := range cards {
		gotNames[card.Name] = true
	}
	if !gotNames["Spell Pierce"] || !gotNames["Forest"] || gotNames["Lightning Strike"] || gotNames["Atraxa, Grand Unifier"] {
		t.Fatalf("color subset search returned %v", gotNames)
	}

	// Type, rarity, set, and mana-value filters.
	cards, _, err = store.SearchCardDefinitions(ctx, CardSearchParams{TypeText: "basic land"})
	if err != nil {
		t.Fatalf("type search: %v", err)
	}
	if len(cards) != 1 || cards[0].Name != "Forest" {
		t.Fatalf("type search returned %+v", cards)
	}

	cards, _, err = store.SearchCardDefinitions(ctx, CardSearchParams{Rarity: "mythic"})
	if err != nil {
		t.Fatalf("rarity search: %v", err)
	}
	if len(cards) != 1 || cards[0].Name != "Atraxa, Grand Unifier" {
		t.Fatalf("rarity search returned %+v", cards)
	}

	cards, _, err = store.SearchCardDefinitions(ctx, CardSearchParams{SetCode: "neo"})
	if err != nil {
		t.Fatalf("set search: %v", err)
	}
	if len(cards) != 1 || cards[0].Name != "Spell Pierce" {
		t.Fatalf("set search returned %+v", cards)
	}

	cards, _, err = store.SearchCardDefinitions(ctx, CardSearchParams{ManaValueMin: floatPtr(2), ManaValueMax: floatPtr(6)})
	if err != nil {
		t.Fatalf("mana value search: %v", err)
	}
	if len(cards) != 1 || cards[0].Name != "Lightning Strike" {
		t.Fatalf("mana value search returned %+v", cards)
	}

	// Explicit mana-value sort.
	cards, _, err = store.SearchCardDefinitions(ctx, CardSearchParams{Sort: "manaValue"})
	if err != nil {
		t.Fatalf("sorted search: %v", err)
	}
	if len(cards) != 4 || cards[0].Name != "Forest" || cards[3].Name != "Atraxa, Grand Unifier" {
		t.Fatalf("mana value sort returned %+v", cards)
	}

	if _, _, err := store.SearchCardDefinitions(ctx, CardSearchParams{Sort: "bogus"}); err == nil {
		t.Fatalf("invalid sort should fail")
	}

	// Name lookup returns all printings keyed by normalized name.
	byName, err := store.ListCardDefinitionsByNames(ctx, []string{"LIGHTNING STRIKE", "unknown card"})
	if err != nil {
		t.Fatalf("ListCardDefinitionsByNames: %v", err)
	}
	if len(byName["lightning strike"]) != 2 {
		t.Fatalf("lightning strike printings = %d, want 2", len(byName["lightning strike"]))
	}
	if len(byName["unknown card"]) != 0 {
		t.Fatalf("unknown card should have no printings")
	}
}

func TestDeckProjectCRUD(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newTestStore(t)

	if err := store.ReplaceCardDefinitions(ctx, "v1", testCardDefinitions()); err != nil {
		t.Fatalf("ReplaceCardDefinitions: %v", err)
	}

	created, err := store.CreateDeckProject(ctx, "  Mono Red  ", "standard", []DeckProjectCardInput{
		{Section: "main", ArenaID: 202, Quantity: 4},
		{Section: "main", ArenaID: 401, Quantity: 20},
		{Section: "sideboard", ArenaID: 501, Quantity: 2},
	})
	if err != nil {
		t.Fatalf("CreateDeckProject: %v", err)
	}
	if created.ID <= 0 || created.Name != "Mono Red" || created.Format != "standard" {
		t.Fatalf("created project = %+v", created)
	}
	if len(created.Cards) != 3 {
		t.Fatalf("created cards = %d, want 3", len(created.Cards))
	}
	for _, card := range created.Cards {
		if card.Missing {
			t.Fatalf("card %d unexpectedly missing", card.ArenaID)
		}
		if card.Name == "" {
			t.Fatalf("card %d has no hydrated name", card.ArenaID)
		}
	}

	// Duplicate rows for the same printing+section merge quantities.
	merged, err := store.CreateDeckProject(ctx, "Merge test", "", []DeckProjectCardInput{
		{Section: "main", ArenaID: 202, Quantity: 2},
		{Section: "main", ArenaID: 202, Quantity: 2},
	})
	if err != nil {
		t.Fatalf("CreateDeckProject merge: %v", err)
	}
	if len(merged.Cards) != 1 || merged.Cards[0].Quantity != 4 {
		t.Fatalf("merged cards = %+v", merged.Cards)
	}

	// Invalid rows are rejected.
	if _, err := store.CreateDeckProject(ctx, "bad", "", []DeckProjectCardInput{{Section: "commander", ArenaID: 202, Quantity: 1}}); err == nil {
		t.Fatalf("invalid section should fail")
	}
	if _, err := store.CreateDeckProject(ctx, "bad", "", []DeckProjectCardInput{{Section: "main", ArenaID: 202, Quantity: 0}}); err == nil {
		t.Fatalf("zero quantity should fail")
	}

	// A card missing from the catalog hydrates from the name cache and is
	// flagged for repair.
	if err := store.UpsertCardNames(ctx, map[int64]string{999: "Ghost Card"}); err != nil {
		t.Fatalf("UpsertCardNames: %v", err)
	}
	saved, err := store.SaveDeckProject(ctx, created.ID, "Mono Red v2", "explorer", []DeckProjectCardInput{
		{Section: "main", ArenaID: 202, Quantity: 3},
		{Section: "main", ArenaID: 999, Quantity: 1},
	})
	if err != nil {
		t.Fatalf("SaveDeckProject: %v", err)
	}
	if saved.Name != "Mono Red v2" || saved.Format != "explorer" {
		t.Fatalf("saved project = %+v", saved)
	}
	if len(saved.Cards) != 2 {
		t.Fatalf("saved cards = %d, want 2", len(saved.Cards))
	}
	var ghost *model.DeckProjectCard
	for i := range saved.Cards {
		if saved.Cards[i].ArenaID == 999 {
			ghost = &saved.Cards[i]
		}
	}
	if ghost == nil || !ghost.Missing || ghost.Name != "Ghost Card" {
		t.Fatalf("ghost card = %+v", ghost)
	}

	summaries, err := store.ListDeckProjects(ctx)
	if err != nil {
		t.Fatalf("ListDeckProjects: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("summaries = %d, want 2", len(summaries))
	}
	var monoRed *model.DeckProjectSummary
	for i := range summaries {
		if summaries[i].ID == created.ID {
			monoRed = &summaries[i]
		}
	}
	if monoRed == nil || monoRed.MainCount != 4 || monoRed.SideboardCount != 0 {
		t.Fatalf("mono red summary = %+v", monoRed)
	}

	if err := store.DeleteDeckProject(ctx, created.ID); err != nil {
		t.Fatalf("DeleteDeckProject: %v", err)
	}
	if _, err := store.GetDeckProject(ctx, created.ID); !errors.Is(err, ErrDeckProjectNotFound) {
		t.Fatalf("get deleted project err = %v, want ErrDeckProjectNotFound", err)
	}
	if err := store.DeleteDeckProject(ctx, created.ID); !errors.Is(err, ErrDeckProjectNotFound) {
		t.Fatalf("double delete err = %v, want ErrDeckProjectNotFound", err)
	}
	if _, err := store.SaveDeckProject(ctx, created.ID, "x", "", nil); !errors.Is(err, ErrDeckProjectNotFound) {
		t.Fatalf("save deleted project err = %v, want ErrDeckProjectNotFound", err)
	}
}
