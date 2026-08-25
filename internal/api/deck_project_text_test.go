package api

import (
	"reflect"
	"testing"

	"github.com/solean/ponder/internal/model"
)

func TestParseArenaDeckList(t *testing.T) {
	t.Parallel()

	text := `About
Name Izzet Phoenix

Deck
4 Lightning Strike (DMU) 137
3 A-Consider
2 Arclight Phoenix

Sideboard
2 Spell Pierce (NEO) 80
not a card line
`

	name, entries, malformed := parseArenaDeckList(text)
	if name != "Izzet Phoenix" {
		t.Fatalf("name = %q, want Izzet Phoenix", name)
	}
	want := []arenaDeckListEntry{
		{Section: "main", Quantity: 4, Name: "Lightning Strike", SetCode: "DMU", CollectorNumber: "137"},
		{Section: "main", Quantity: 3, Name: "A-Consider"},
		{Section: "main", Quantity: 2, Name: "Arclight Phoenix"},
		{Section: "sideboard", Quantity: 2, Name: "Spell Pierce", SetCode: "NEO", CollectorNumber: "80"},
	}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("entries = %+v, want %+v", entries, want)
	}
	if len(malformed) != 1 || malformed[0] != "not a card line" {
		t.Fatalf("malformed = %v", malformed)
	}
}

func TestParseArenaDeckListWithoutHeaders(t *testing.T) {
	t.Parallel()

	_, entries, malformed := parseArenaDeckList("4 Forest\n2 Llanowar Elves")
	if len(malformed) != 0 {
		t.Fatalf("malformed = %v", malformed)
	}
	if len(entries) != 2 || entries[0].Section != "main" || entries[1].Section != "main" {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestFormatArenaDeckList(t *testing.T) {
	t.Parallel()

	cards := []model.DeckProjectCard{
		{Section: "main", ArenaID: 101, Quantity: 4, Name: "Lightning Strike", SetCode: "DMU", CollectorNumber: "137"},
		{Section: "main", ArenaID: 102, Quantity: 20, Name: "Mountain"},
		{Section: "sideboard", ArenaID: 103, Quantity: 2, Name: "Spell Pierce", SetCode: "NEO", CollectorNumber: "80"},
		{Section: "main", ArenaID: 999, Quantity: 1},
	}

	text, unresolved := formatArenaDeckList(cards)
	want := "Deck\n4 Lightning Strike (DMU) 137\n20 Mountain\n\nSideboard\n2 Spell Pierce (NEO) 80\n"
	if text != want {
		t.Fatalf("text = %q, want %q", text, want)
	}
	if len(unresolved) != 1 || unresolved[0] != 999 {
		t.Fatalf("unresolved = %v", unresolved)
	}
}

func TestFormatArenaDeckListRoundTrip(t *testing.T) {
	t.Parallel()

	cards := []model.DeckProjectCard{
		{Section: "main", ArenaID: 101, Quantity: 4, Name: "Lightning Strike", SetCode: "DMU", CollectorNumber: "137"},
		{Section: "sideboard", ArenaID: 103, Quantity: 2, Name: "Spell Pierce", SetCode: "NEO", CollectorNumber: "80"},
	}
	text, _ := formatArenaDeckList(cards)

	_, entries, malformed := parseArenaDeckList(text)
	if len(malformed) != 0 {
		t.Fatalf("malformed = %v", malformed)
	}
	want := []arenaDeckListEntry{
		{Section: "main", Quantity: 4, Name: "Lightning Strike", SetCode: "DMU", CollectorNumber: "137"},
		{Section: "sideboard", Quantity: 2, Name: "Spell Pierce", SetCode: "NEO", CollectorNumber: "80"},
	}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("entries = %+v, want %+v", entries, want)
	}
}

func TestPickPrintingForImport(t *testing.T) {
	t.Parallel()

	printings := []model.CardDefinition{
		{ArenaID: 101, Name: "Lightning Strike", SetCode: "DMU", CollectorNumber: "137"},
		{ArenaID: 202, Name: "Lightning Strike", SetCode: "M25", CollectorNumber: "12"},
		{ArenaID: 303, Name: "Lightning Strike", SetCode: "Y26", CollectorNumber: "1", IsDigitalOnly: true},
	}

	if _, ok := pickPrintingForImport(nil, "", ""); ok {
		t.Fatalf("empty printings should not resolve")
	}

	chosen, ok := pickPrintingForImport(printings, "DMU", "137")
	if !ok || chosen.ArenaID != 101 {
		t.Fatalf("exact match = %+v ok=%v, want 101", chosen, ok)
	}

	chosen, ok = pickPrintingForImport(printings, "M25", "999")
	if !ok || chosen.ArenaID != 202 {
		t.Fatalf("set match = %+v ok=%v, want 202", chosen, ok)
	}

	// No set hint: newest non-digital printing wins over a newer digital one.
	chosen, ok = pickPrintingForImport(printings, "", "")
	if !ok || chosen.ArenaID != 202 {
		t.Fatalf("default printing = %+v ok=%v, want 202", chosen, ok)
	}
}

func TestConvertMTGARawManaCost(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"":             "",
		"o3oGoWoUoB":   "{3}{G}{W}{U}{B}",
		"oXoGoU":       "{X}{G}{U}",
		"o3o(U/P)":     "{3}{U/P}",
		"o(2/W)o(2/W)": "{2/W}{2/W}",
		"o1oR":         "{1}{R}",
	}
	for input, want := range cases {
		if got := convertMTGARawManaCost(input); got != want {
			t.Fatalf("convertMTGARawManaCost(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestComposeTypeLine(t *testing.T) {
	t.Parallel()

	if got := composeTypeLine("Legendary Creature", "Phyrexian Angel"); got != "Legendary Creature \u2014 Phyrexian Angel" {
		t.Fatalf("composeTypeLine = %q", got)
	}
	if got := composeTypeLine("Instant", ""); got != "Instant" {
		t.Fatalf("composeTypeLine = %q", got)
	}
	if got := composeTypeLine("", ""); got != "" {
		t.Fatalf("composeTypeLine = %q", got)
	}
}
