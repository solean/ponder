import { describe, expect, test } from "bun:test";

import {
  DEFAULT_DECK_SORT,
  EMPTY_DECK_FILTERS,
  buildDeckRow,
  deckListParams,
  filterDeckRows,
  parseDeckListParams,
  sortDeckRows,
  type DeckListFilters,
} from "../src/lib/deckList";
import type { DeckSummary } from "../src/lib/types";

const NOW = Date.parse("2026-10-09T12:00:00Z");

function deck(overrides: Partial<DeckSummary>): DeckSummary {
  return {
    deckId: 1,
    deckName: "Deck",
    format: "Standard",
    eventName: "",
    matches: 0,
    wins: 0,
    losses: 0,
    winRate: 0,
    colors: [],
    colorsKnown: true,
    results: [],
    ...overrides,
  };
}

const izzet = deck({
  deckId: 1,
  deckName: "Izzet Prowess",
  colors: ["U", "R"],
  results: [
    { eventName: "Traditional_Ladder", playedAt: "2026-10-08T10:00:00Z", result: "win" },
    { eventName: "Ladder", playedAt: "2026-10-01T10:00:00Z", result: "loss" },
    { eventName: "Traditional_Ladder", playedAt: "2026-06-01T10:00:00Z", result: "win" },
  ],
});
const golgari = deck({
  deckId: 2,
  deckName: "Pioneer Golgari",
  format: "TraditionalExplorer",
  colors: ["B", "G"],
  results: [{ eventName: "Ladder", playedAt: "2026-09-20T10:00:00Z", result: "win" }],
});
const brew = deck({ deckId: 3, deckName: "Brew", colors: ["U"] });
const unknownColors = deck({ deckId: 4, deckName: "Mystery", colorsKnown: false, colors: null });

function rows(filters: DeckListFilters = EMPTY_DECK_FILTERS) {
  return [izzet, golgari, brew, unknownColors].map((d) => buildDeckRow(d, filters, NOW));
}

describe("deck list URL state", () => {
  test("defaults serialize to an empty query", () => {
    expect(deckListParams(EMPTY_DECK_FILTERS, DEFAULT_DECK_SORT).toString()).toBe("");
    expect(parseDeckListParams(new URLSearchParams())).toEqual({
      filters: EMPTY_DECK_FILTERS,
      sort: DEFAULT_DECK_SORT,
    });
  });

  test("round-trips filters and sort, normalizing color order", () => {
    const filters: DeckListFilters = {
      q: "izzet",
      format: "Standard",
      event: "Traditional Ladder",
      colors: ["R", "U"],
      period: "30d",
      hideUnplayed: true,
    };
    const params = deckListParams(filters, { key: "winRate", desc: false });
    expect(params.get("colors")).toBe("UR");
    expect(params.get("dir")).toBe("asc");

    const parsed = parseDeckListParams(params);
    expect(parsed.filters).toEqual({ ...filters, colors: ["U", "R"] });
    expect(parsed.sort).toEqual({ key: "winRate", desc: false });
  });

  test("ignores invalid values", () => {
    const parsed = parseDeckListParams(new URLSearchParams("sort=bogus&period=1y&colors=uxq"));
    expect(parsed.sort).toEqual(DEFAULT_DECK_SORT);
    expect(parsed.filters.period).toBe("");
    expect(parsed.filters.colors).toEqual(["U"]);
  });

  test("text columns default ascending without a dir param", () => {
    expect(parseDeckListParams(new URLSearchParams("sort=name")).sort).toEqual({ key: "name", desc: false });
    expect(deckListParams(EMPTY_DECK_FILTERS, { key: "name", desc: false }).toString()).toBe("sort=name");
  });
});

describe("deck row scoping", () => {
  test("unscoped rows count every match", () => {
    const row = buildDeckRow(izzet, EMPTY_DECK_FILTERS, NOW);
    expect([row.matches, row.wins, row.losses]).toEqual([3, 2, 1]);
    expect(row.lastPlayedAt).toBe("2026-10-08T10:00:00Z");
    expect(row.events).toEqual([
      { category: "Traditional Ladder", matches: 2 },
      { category: "Ranked Ladder", matches: 1 },
    ]);
  });

  test("event and period filters scope the record", () => {
    const row = buildDeckRow(izzet, { ...EMPTY_DECK_FILTERS, event: "Traditional Ladder", period: "30d" }, NOW);
    expect([row.matches, row.wins, row.losses]).toEqual([1, 1, 0]);
    expect(row.winRate).toBe(1);
    // Events stay descriptive of the whole deck.
    expect(row.events).toHaveLength(2);
  });

  test("win rate ignores matches without a result", () => {
    const row = buildDeckRow(
      deck({ results: [
        { eventName: "Ladder", playedAt: "2026-10-01T00:00:00Z", result: "win" },
        { eventName: "Ladder", playedAt: "2026-09-01T00:00:00Z", result: "" },
      ] }),
      EMPTY_DECK_FILTERS,
      NOW,
    );
    expect(row.matches).toBe(2);
    expect(row.winRate).toBe(1);
  });
});

describe("deck row filtering", () => {
  const names = (filters: DeckListFilters) =>
    filterDeckRows(rows(filters), filters).map((row) => row.deck.deckName);

  test("search, format label, and colors", () => {
    expect(names({ ...EMPTY_DECK_FILTERS, q: "golg" })).toEqual(["Pioneer Golgari"]);
    expect(names({ ...EMPTY_DECK_FILTERS, format: "Pioneer" })).toEqual(["Pioneer Golgari"]);
    expect(names({ ...EMPTY_DECK_FILTERS, colors: ["U"] })).toEqual(["Izzet Prowess", "Brew"]);
  });

  test("scoped views and hide-unplayed drop decks without matches", () => {
    expect(names({ ...EMPTY_DECK_FILTERS, hideUnplayed: true })).toEqual(["Izzet Prowess", "Pioneer Golgari"]);
    expect(names({ ...EMPTY_DECK_FILTERS, period: "7d" })).toEqual(["Izzet Prowess"]);
    expect(names({ ...EMPTY_DECK_FILTERS, event: "Ranked Ladder" })).toEqual(["Izzet Prowess", "Pioneer Golgari"]);
  });
});

describe("deck row sorting", () => {
  const sorted = (key: Parameters<typeof sortDeckRows>[1]["key"], desc: boolean) =>
    sortDeckRows(rows(), { key, desc }).map((row) => row.deck.deckName);

  test("last played puts unplayed decks last in both directions", () => {
    expect(sorted("lastPlayed", true)).toEqual(["Izzet Prowess", "Pioneer Golgari", "Brew", "Mystery"]);
    expect(sorted("lastPlayed", false)).toEqual(["Pioneer Golgari", "Izzet Prowess", "Brew", "Mystery"]);
  });

  test("name sorts case-insensitively", () => {
    expect(sorted("name", false)).toEqual(["Brew", "Izzet Prowess", "Mystery", "Pioneer Golgari"]);
  });

  test("colors sort by count then WUBRG, unknown last", () => {
    expect(sorted("colors", false)).toEqual(["Brew", "Izzet Prowess", "Pioneer Golgari", "Mystery"]);
  });

  test("win rate keeps decks without a rate last", () => {
    expect(sorted("winRate", true).slice(0, 2)).toEqual(["Pioneer Golgari", "Izzet Prowess"]);
    expect(sorted("winRate", false).slice(0, 2)).toEqual(["Izzet Prowess", "Pioneer Golgari"]);
  });
});
