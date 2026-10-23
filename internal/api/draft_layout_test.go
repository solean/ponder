package api

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/solean/ponder/internal/model"
)

func TestDraftDisplayPack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Raw_CardDatabase_test.mtga")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.Exec(`CREATE TABLE Cards (GrpId INTEGER PRIMARY KEY,Order_MythicToCommon INTEGER,Order_ColorOrder INTEGER,Order_CMCWithXLast INTEGER,Order_Title TEXT)`); err != nil {
		t.Fatal(err)
	}
	// Sort values copied from the local Arena database for the supplied P1P2
	// screenshot. Input follows Draft.Notify, expected follows visual positions.
	fixtures := []struct {
		id                  int64
		name                string
		rarity, color, mana int
		title               string
	}{
		{106433, "Room of Refuge", 3, 31, 0, "roomofrefuge"},
		{106376, "Konstrari Improviser", 3, 11, 2, "konstrariimproviser"},
		{106397, "Theorix Metamage", 3, 7, 3, "theorixmetamage"},
		{106237, "Hexhaven Battalion", 3, 0, 6, "hexhavenbattalion"},
		{106227, "Blossom-Blessed Angel", 3, 0, 4, "blossomblessedange"},
		{106407, "Whiplash Wordsmith", 3, 9, 4, "whiplashwordsmith"},
		{106347, "Restore with Empathy", 2, 4, 3, "restorewithempathy"},
		{106243, "Refute Destiny", 2, 0, 2, "refutedestiny"},
		{106425, "Haunted Ridge", 1, 9, 0, "hauntedridge"},
		{106494, "Marwyn, the Clearcutter", 2, 3, 1, "marwyntheclearcut"},
		{106464, "Ruric Thar, Biomagus", 2, 1, 6, "rurictharbiomagus"},
		{106510, "Ruric Thar, Magecrusher", 2, 4, 7, "rurictharmagecrus"},
		{106428, "Konstrari Annex", 4, 11, 0, "konstrariannex"},
	}

	// The second screenshot distinguishes title order from mana value: Pegasus
	// (3 mana) precedes Surgical Precision (2), and Awaken (5) precedes Hallway (3).
	second := []struct {
		id                  int64
		name                string
		rarity, color, mana int
		title               string
	}{
		{106384, "Proctor of Potential", 1, 5, 2, "proctorofpotential"},
		{106450, "Thalia, the Survivor", 2, 0, 4, "thaliathesurvivor"},
		{106297, "Terminal Criticism", 2, 2, 2, "terminalcriticism"},
		{106501, "Winter, Team Player", 2, 3, 5, "winterteamplayer"},
		{106403, "Vigorbloom Vanguard", 2, 13, 2, "vigorbloomvanguard"},
		{106246, "Shatterwing Pegasus", 3, 0, 3, "shatterwingpegasus"},
		{106247, "Surgical Precision", 3, 0, 2, "surgicalprecision"},
		{106268, "Sphinx's Approach", 3, 1, 3, "sphinxsapproach"},
		{106299, "Void Extrapolator", 3, 2, 2, "voidextrapolator"},
		{106304, "Awaken the Inferno", 3, 3, 5, "awakentheinferno"},
		{106315, "Hallway Heckler", 3, 3, 3, "hallwayheckler"},
		{106338, "Greenhouse Propagator", 3, 4, 3, "greenhousepropagato"},
		{106534, "Forest", 4, 4, 0, "forest"},
	}
	secondPack := make([]model.DraftPickCard, 0, len(second))
	secondWant := make([]string, 0, len(second))
	for i := len(second) - 1; i >= 0; i-- {
		f := second[i]
		if _, err := raw.Exec(`INSERT INTO Cards VALUES (?,?,?,?,?)`, f.id, f.rarity, f.color, f.mana, f.title); err != nil {
			t.Fatal(err)
		}
		secondPack = append(secondPack, model.DraftPickCard{CardID: f.id, CardName: f.name})
	}
	for _, f := range second {
		secondWant = append(secondWant, f.name)
	}
	t.Setenv(mtgaRawCardDBEnvVar, path)
	secondGot := draftDisplayPack(context.Background(), secondPack)
	secondNames := make([]string, len(secondGot))
	for i, card := range secondGot {
		secondNames[i] = card.CardName
	}
	if !reflect.DeepEqual(secondNames, secondWant) {
		t.Fatalf("second screenshot order = %v, want %v", secondNames, secondWant)
	}
	pack := make([]model.DraftPickCard, 0, len(fixtures))
	for _, f := range fixtures {
		if _, err := raw.Exec(`INSERT INTO Cards VALUES (?,?,?,?,?)`, f.id, f.rarity, f.color, f.mana, f.title); err != nil {
			t.Fatal(err)
		}
		pack = append(pack, model.DraftPickCard{CardID: f.id, CardName: f.name})
	}
	t.Setenv(mtgaRawCardDBEnvVar, path)
	original := append([]model.DraftPickCard(nil), pack...)
	got := draftDisplayPack(context.Background(), pack)
	want := []string{"Haunted Ridge", "Refute Destiny", "Ruric Thar, Biomagus", "Marwyn, the Clearcutter", "Restore with Empathy", "Ruric Thar, Magecrusher", "Blossom-Blessed Angel", "Hexhaven Battalion", "Theorix Metamage", "Whiplash Wordsmith", "Konstrari Improviser", "Room of Refuge", "Konstrari Annex"}
	names := make([]string, len(got))
	for i, card := range got {
		names[i] = card.CardName
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("visual order = %v, want %v", names, want)
	}
	if !reflect.DeepEqual(pack, original) {
		t.Fatal("display ordering mutated original pack")
	}

	duplicate := []model.DraftPickCard{{CardID: 106433, CardName: "room"}, {CardID: 106425, CardName: "first copy"}, {CardID: 106425, CardName: "second copy"}}
	ordered := draftDisplayPack(context.Background(), duplicate)
	if len(ordered) != 3 || ordered[0].CardName != "first copy" || ordered[1].CardName != "second copy" {
		t.Fatalf("duplicate occurrences lost/reordered: %#v", ordered)
	}
	if got := draftDisplayPack(context.Background(), append(pack, model.DraftPickCard{CardID: 999})); got != nil {
		t.Fatalf("missing metadata produced guessed order: %#v", got)
	}
	for _, column := range []string{"Order_MythicToCommon", "Order_ColorOrder", "Order_Title"} {
		t.Run("null "+column, func(t *testing.T) {
			tx, err := raw.Begin()
			if err != nil {
				t.Fatal(err)
			}
			// Commit so the independent read-only connection observes incomplete data.
			var previous any
			if err := tx.QueryRow(`SELECT ` + column + ` FROM Cards WHERE GrpId=106433`).Scan(&previous); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(`UPDATE Cards SET ` + column + `=NULL WHERE GrpId=106433`); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			if got := draftDisplayPack(context.Background(), pack); got != nil {
				t.Fatalf("null metadata produced order: %#v", got)
			}
			if _, err := raw.Exec(`UPDATE Cards SET `+column+`=? WHERE GrpId=106433`, previous); err != nil {
				t.Fatal(err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := draftDisplayPack(ctx, pack); got != nil {
		t.Fatal("cancelled query returned display pack")
	}
	if _, err := raw.Exec(`DROP TABLE Cards`); err != nil {
		t.Fatal(err)
	}
	if got := draftDisplayPack(context.Background(), pack); got != nil {
		t.Fatal("unavailable schema produced display pack")
	}
}
