package db

import (
	"context"
	"testing"
)

func TestNormalizeCardPreviewName(t *testing.T) {
	for in, want := range map[string]string{
		"  Lightning Bolt ":                         "lightning bolt",
		"Delver of Secrets // Insectile Aberration": "delver of secrets",
		"": "",
	} {
		if got := NormalizeCardPreviewName(in); got != want {
			t.Errorf("NormalizeCardPreviewName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCardPreviewsUpsertAndLookupByName(t *testing.T) {
	ctx := context.Background()
	database := openTempSQLiteDB(t)
	if err := Init(ctx, database); err != nil {
		t.Fatal(err)
	}
	store := NewStore(database)
	if err := store.UpsertCardPreviews(ctx, []CardPreview{
		{ArenaID: 10, Name: "Fable of the Mirror-Breaker // Reflection of Kiki-Jiki", ImageURL: "old"},
		{ArenaID: 20, Name: "Fable of the Mirror-Breaker // Reflection of Kiki-Jiki", ImageURL: "new"},
		{ArenaID: 30, Name: "No Image"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertCardPreviews(ctx, []CardPreview{{ArenaID: 10, Name: "Fable of the Mirror-Breaker", ImageURL: "updated"}}); err != nil {
		t.Fatal(err)
	}

	byID, err := store.LookupCardPreviews(ctx, []int64{10, 30})
	if err != nil {
		t.Fatal(err)
	}
	if len(byID) != 1 || byID[10].ImageURL != "updated" {
		t.Fatalf("by ID = %+v", byID)
	}
	byName, err := store.LookupCardPreviewsByName(ctx, []string{"fable of the mirror-breaker", "Missing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(byName) != 1 || byName["fable of the mirror-breaker"].ArenaID != 20 {
		t.Fatalf("by name = %+v, want the highest Arena ID", byName)
	}

	if syncedAt, err := store.CardPreviewsSyncedAt(ctx); err != nil || !syncedAt.IsZero() {
		t.Fatalf("CardPreviewsSyncedAt before any sync = %v, %v", syncedAt, err)
	}
}
