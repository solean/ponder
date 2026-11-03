package api

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/solean/ponder/internal/model"
)

func TestFixedManaProduction(t *testing.T) {
	for _, tc := range []struct {
		text   string
		total  int64
		colors map[string]int64
	}{
		{"{oT}: Add {oU}.", 1, map[string]int64{"U": 1}},
		{"{oT}: Add {oW} or {oB}.", 1, map[string]int64{"W": 1, "B": 1}},
		{"{oT}: Add {oWoW} or {oBoB}.", 2, map[string]int64{"W": 2, "B": 2}},
		{"{oT}: Add {oCoC}.", 2, map[string]int64{"C": 2}},
		{"{oT}: Add {oWoU}.", 2, map[string]int64{"W": 1, "U": 1}},
		{"{oT}, Sacrifice CARDNAME: Add one mana of any color.", 1, map[string]int64{"W": 1, "U": 1, "B": 1, "R": 1, "G": 1}},
		{"{oT}: Add {oW}, {oU}, or {oB}.", 1, map[string]int64{"W": 1, "U": 1, "B": 1}},
	} {
		t.Run(tc.text, func(t *testing.T) {
			total, colors, ok := fixedManaProduction(tc.text)
			if !ok || total != tc.total || !reflect.DeepEqual(colors, tc.colors) {
				t.Fatalf("got %d %v %v", total, colors, ok)
			}
		})
	}
	for _, text := range []string{
		"{o1}, {oT}: Add one mana of any color.",
		"{oT}: Add {oG}. Activate only if you control a creature.",
		"{oT}: Add {oC}. Spend this mana only to cast artifacts.",
		"{oT}: Add X mana of any one color.",
		"{oT}: Add {oC} for each creature you control.",
		"{oT}: Add {oC}. If you control a Gate, add {oCoC} instead.",
	} {
		if _, _, ok := fixedManaProduction(text); ok {
			t.Errorf("unsupported ability counted: %s", text)
		}
	}
}

func TestProjectLiveManaCountsEachPermanentOnce(t *testing.T) {
	object := func(types, abilities string) model.MatchReplayFrameObjectRow {
		return model.MatchReplayFrameObjectRow{DetailsJSON: `{"cardTypes":[` + types + `],"uniqueAbilities":[` + abilities + `]}`}
	}
	dual := object(`"CardType_Land"`, `{"grpId":1},{"grpId":2}`)
	tapped := dual
	tapped.IsTapped = true
	rock := object(`"CardType_Artifact"`, `{"grpId":3}`)
	sick := object(`"CardType_Creature"`, `{"grpId":4}`)
	sick.HasSummoningSickness = true
	land := object(`"CardType_Land"`, `{"grpId":4}`)
	land.HasSummoningSickness = true // irrelevant for non-creature lands
	conditional := object(`"CardType_Land"`, `{"grpId":5}`)
	unknown := object(`"CardType_Artifact"`, `{"grpId":999}`)
	abilities := map[int64]liveManaAbility{
		1: {"{oT}: Add {oW} or {oB}.", true}, 2: {"{oT}: Add {oW}.", true},
		3: {"{oT}: Add {oCoC}.", true}, 4: {"{oT}: Add {oG}.", true},
		5: {"{oT}: Add {oR}. Activate only if you control a Dragon.", true},
	}
	got := projectLiveMana([]model.MatchReplayFrameObjectRow{dual, tapped, rock, sick, land, conditional, unknown},
		[]model.ManaPoolEntry{{Color: "ManaColor_Blue", Count: 2}}, true, abilities)
	if got.Lands != 4 || got.Available != 6 || got.Floating != 2 || got.Unresolved != 2 || !got.PoolKnown {
		t.Fatalf("summary = %+v", got)
	}
	if !reflect.DeepEqual(got.ByColor, map[string]int64{"W": 1, "B": 1, "C": 2, "G": 1, "U": 2}) {
		t.Fatalf("colors = %v", got.ByColor)
	}
	empty := projectLiveMana(nil, nil, false, nil)
	if empty.Available != 0 || empty.PoolKnown || !empty.LandsKnown {
		t.Fatalf("unknown pool = %+v", empty)
	}
}

func TestLiveManaAbilityCatalogUsesLowestFormattedLocalization(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cards.db")
	catalog, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer catalog.Close()
	_, err = catalog.Exec(`CREATE TABLE Abilities (Id INTEGER, Category INTEGER, SubCategory INTEGER, TextId INTEGER);
 CREATE TABLE Localizations_enUS (LocId INTEGER, Formatted INTEGER, Loc TEXT);
 INSERT INTO Abilities VALUES (1002,1,1,20),(2000,2,1,30);
 INSERT INTO Localizations_enUS VALUES (20,1,'{oT}: Add {oU}.'),(20,2,'formatted'),(30,0,'Flying');`)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(mtgaRawCardDBEnvVar, path)
	got := liveManaAbilities(context.Background(), []model.MatchReplayFrameObjectRow{{DetailsJSON: `{"uniqueAbilities":[{"grpId":1002},{"grpId":2000}]}`}})
	if got[1002].Text != "{oT}: Add {oU}." || !got[1002].Mana || got[2000].Mana {
		t.Fatalf("abilities = %v", got)
	}
}

func TestProjectLiveManaDoesNotClaimMissingTypesAreZeroLands(t *testing.T) {
	got := projectLiveMana([]model.MatchReplayFrameObjectRow{{DetailsJSON: `{}`}}, nil, false, nil)
	if got.LandsKnown || got.Unresolved != 1 {
		t.Fatalf("missing types = %+v", got)
	}
}
