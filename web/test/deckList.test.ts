import { describe, expect, test } from "bun:test";

import {
  DEFAULT_DECK_SORT,
  EMPTY_DECK_FILTERS,
  buildDeckRow,
  deckListParams,
  filterDeckRows,
  parseDeckListParams,
  sortDeckRows,
  summarizeDeckRows,
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

describe("game-level stats", () => {
  const bo3 = deck({
    deckId: 10,
    results: [
      {
        eventName: "Traditional_Ladder",
        playedAt: "2026-10-08T00:00:00Z",
        result: "win",
        gameWins: 2,
        gameLosses: 1,
        playWins: 1,
        playLosses: 0,
        drawWins: 1,
        drawLosses: 1,
      },
      {
        eventName: "Ladder",
        playedAt: "2026-09-01T00:00:00Z",
        result: "loss",
        gameWins: 0,
        gameLosses: 1,
        playWins: 0,
        playLosses: 1,
        drawWins: 0,
        drawLosses: 0,
      },
    ],
  });

  test("sums games and play/draw across scoped matches", () => {
    const row = buildDeckRow(bo3, EMPTY_DECK_FILTERS, NOW);
    expect(row.games).toEqual({ wins: 2, losses: 2, rate: 0.5 });
    expect(row.play).toEqual({ wins: 1, losses: 1, rate: 0.5 });
    expect(row.draw).toEqual({ wins: 1, losses: 1, rate: 0.5 });

    const scoped = buildDeckRow(bo3, { ...EMPTY_DECK_FILTERS, event: "Traditional Ladder" }, NOW);
    expect(scoped.games).toEqual({ wins: 2, losses: 1, rate: 2 / 3 });
    expect(scoped.play.rate).toBe(1);
  });

  test("missing game data yields no rate", () => {
    const row = buildDeckRow(izzet, EMPTY_DECK_FILTERS, NOW);
    expect(row.games.rate).toBeNull();
    expect(row.play.rate).toBeNull();
  });
});

describe("form", () => {
  test("keeps the most recent results, oldest first", () => {
    const results = Array.from({ length: 12 }, (_, index) => ({
      eventName: "Ladder",
      playedAt: new Date(NOW - index * 60_000).toISOString(),
      result: index === 0 ? "loss" : index === 11 ? "loss" : "win",
    }));
    const row = buildDeckRow(deck({ results }), EMPTY_DECK_FILTERS, NOW);
    expect(row.form).toHaveLength(10);
    expect(row.form.at(-1)).toBe("loss");
    expect(row.form.slice(0, -1).every((result) => result === "win")).toBe(true);
  });
});

describe("version status", () => {
  const versioned = (lastVersionId: number) =>
    deck({
      versionCount: 3,
      latestVersion: { id: 30, versionNumber: 3, effectiveAt: "2026-10-05T00:00:00Z" },
      results: [
        { eventName: "Ladder", playedAt: "2026-10-04T00:00:00Z", result: "win", deckVersionId: lastVersionId },
        { eventName: "Ladder", playedAt: "2026-10-01T00:00:00Z", result: "win", deckVersionId: 20 },
      ],
    });

  test("flags a list edited after the last match", () => {
    const status = buildDeckRow(versioned(20), EMPTY_DECK_FILTERS, NOW).version;
    expect(status).toMatchObject({ versionNumber: 3, versionCount: 3, matchesOnLatest: 0, editedSinceLastPlayed: true });
  });

  test("not edited when the last match used the latest list", () => {
    const status = buildDeckRow(versioned(30), EMPTY_DECK_FILTERS, NOW).version;
    expect(status).toMatchObject({ matchesOnLatest: 1, editedSinceLastPlayed: false });
  });

  test("no version info without a latest version", () => {
    expect(buildDeckRow(brew, EMPTY_DECK_FILTERS, NOW).version).toBeNull();
  });
});

describe("summary", () => {
  test("totals the listed rows and picks most played and best deck", () => {
    const winner = deck({
      deckId: 20,
      deckName: "Winner",
      results: Array.from({ length: 10 }, (_, index) => ({
        eventName: "Ladder",
        playedAt: new Date(NOW - index * 60_000).toISOString(),
        result: index < 8 ? "win" : "loss",
      })),
    });
    const grinder = deck({
      deckId: 21,
      deckName: "Grinder",
      results: Array.from({ length: 14 }, (_, index) => ({
        eventName: "Ladder",
        playedAt: new Date(NOW - index * 60_000).toISOString(),
        result: index % 2 === 0 ? "win" : "loss",
      })),
    });
    const summary = summarizeDeckRows(
      [winner, grinder, izzet, brew].map((d) => buildDeckRow(d, EMPTY_DECK_FILTERS, NOW)),
    );
    expect(summary.decks).toBe(4);
    expect(summary.matches).toBe(27);
    expect(summary.record).toMatchObject({ wins: 17, losses: 10 });
    expect(summary.mostPlayed?.deck.deckName).toBe("Grinder");
    // Izzet (2–1) is under the sample floor, so it can't be best.
    expect(summary.best?.deck.deckName).toBe("Winner");
  });

  test("no best deck below the sample floor", () => {
    const summary = summarizeDeckRows([buildDeckRow(izzet, EMPTY_DECK_FILTERS, NOW)]);
    expect(summary.best).toBeNull();
    expect(summary.mostPlayed?.deck.deckName).toBe("Izzet Prowess");
  });
});
