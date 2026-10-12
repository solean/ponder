package api

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/solean/ponder/internal/db"
)

// New sets reach Scryfall before their Arena IDs do, so type lines must come
// from the MTGA client's card database and land in the cache.
func TestResolveCardTypeLinesFromMTGARaw(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	rawPath := filepath.Join(dir, "Raw_CardDatabase_test.mtga")
	raw, err := sql.Open("sqlite", rawPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE TABLE Cards (GrpId INTEGER PRIMARY KEY, TypeTextId INTEGER, SubtypeTextId INTEGER)`,
		`CREATE TABLE Localizations_enUS (LocId INT NOT NULL, Formatted INT NOT NULL, Loc TEXT, PRIMARY KEY (LocId, Formatted))`,
		`INSERT INTO Cards VALUES (106467, 1, 2), (106422, 3, 0), (91310, 4, 5)`,
		`INSERT INTO Localizations_enUS VALUES
			(1, 1, 'Legendary Artifact Creature'),
			(2, 0, 'Dragon Construct'), (2, 1, '<nobr>Dragon Construct</nobr>'),
			(3, 1, 'Land'),
			(4, 1, 'Basic Land'), (5, 1, 'Forest')`,
	} {
		if _, err := raw.ExecContext(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	t.Setenv(mtgaRawCardDBEnvVar, rawPath)

	database, err := db.Open(filepath.Join(dir, "ponder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.Init(ctx, database); err != nil {
		t.Fatal(err)
	}
	store := db.NewStore(database)
	server := NewServer(store, "", nil)

	want := map[int64]string{
		106467: "Legendary Artifact Creature — Dragon Construct",
		106422: "Land",
		91310:  "Basic Land — Forest",
	}
	ids := []int64{106467, 106422, 91310}
	if got := server.resolveCardTypeLines(ctx, ids); !reflect.DeepEqual(got, want) {
		t.Fatalf("resolved = %v, want %v", got, want)
	}
	cached, err := store.LookupCardTypeLines(ctx, ids)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cached, want) {
		t.Fatalf("cached = %v, want %v", cached, want)
	}
}
