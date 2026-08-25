import { describe, expect, test } from "bun:test";

import {
  addCard,
  adjustQuantity,
  colorCounts,
  deckWarnings,
  isBasicLand,
  isLand,
  manaCurve,
  moveCard,
  parseManaCostSymbols,
  removeCard,
  sectionTotal,
  sortProjectCards,
} from "../src/lib/deckBuilder";
import type { CardDefinition, DeckProjectCard } from "../src/lib/types";

function card(overrides: Partial<DeckProjectCard>): DeckProjectCard {
  return {
    section: "main",
    arenaId: 1,
    quantity: 1,
    name: "Test Card",
    setCode: "TST",
    collectorNumber: "1",
    rarity: "common",
    manaCost: "{1}{R}",
    manaValue: 2,
    colors: ["R"],
    typeLine: "Instant",
    missing: false,
    ...overrides,
  };
}

function definition(overrides: Partial<CardDefinition>): CardDefinition {
  return {
    arenaId: 1,
    name: "Test Card",
    setCode: "TST",
    collectorNumber: "1",
    rarity: "common",
    manaCost: "{1}{R}",
    manaValue: 2,
    colors: ["R"],
    colorIdentity: ["R"],
    typeLine: "Instant",
    isDigitalOnly: false,
    isRebalanced: false,
    ...overrides,
  };
}

describe("parseManaCostSymbols", () => {
  test("splits curly-brace symbols", () => {
    expect(parseManaCostSymbols("{3}{G}{W}{U}{B}")).toEqual(["3", "G", "W", "U", "B"]);
    expect(parseManaCostSymbols("{X}{U/P}")).toEqual(["X", "U/P"]);
    expect(parseManaCostSymbols("")).toEqual([]);
  });
});

describe("land detection", () => {
  test("detects lands by type line", () => {
    expect(isLand(card({ typeLine: "Basic Land — Forest" }))).toBe(true);
    expect(isLand(card({ typeLine: "Legendary Creature — Angel" }))).toBe(false);
  });

  test("detects basic lands with name fallback", () => {
    expect(isBasicLand(card({ typeLine: "Basic Land — Forest", name: "Forest" }))).toBe(true);
    expect(isBasicLand(card({ typeLine: "", name: "Mountain" }))).toBe(true);
    expect(isBasicLand(card({ typeLine: "Land", name: "Fabled Passage" }))).toBe(false);
  });
});

describe("curve and colors", () => {
  test("buckets main-deck nonland cards, 7+ capped", () => {
    const cards = [
      card({ arenaId: 1, quantity: 4, manaValue: 2 }),
      card({ arenaId: 2, quantity: 2, manaValue: 9 }),
      card({ arenaId: 3, quantity: 20, manaValue: 0, typeLine: "Basic Land — Forest" }),
      card({ arenaId: 4, quantity: 3, manaValue: 2, section: "sideboard" }),
    ];
    expect(manaCurve(cards)).toEqual([0, 0, 4, 0, 0, 0, 0, 2]);
  });

  test("counts main-deck colors per card copy", () => {
    const cards = [
      card({ arenaId: 1, quantity: 4, colors: ["W", "U"] }),
      card({ arenaId: 2, quantity: 2, colors: ["U"] }),
      card({ arenaId: 3, quantity: 1, colors: ["B"], section: "sideboard" }),
    ];
    expect(colorCounts(cards)).toEqual({ W: 4, U: 6 });
  });
});

describe("deckWarnings", () => {
  test("flags empty name, small main deck, and oversized sideboard", () => {
    const cards = [
      card({ arenaId: 1, quantity: 30 }),
      card({ arenaId: 2, quantity: 16, section: "sideboard", name: "Side Card" }),
    ];
    const warnings = deckWarnings("", cards);
    expect(warnings.some((w) => w.includes("no name"))).toBe(true);
    expect(warnings.some((w) => w.includes("Main deck has 30"))).toBe(true);
    expect(warnings.some((w) => w.includes("Sideboard has 16"))).toBe(true);
  });

  test("counts copies across printings and sections but exempts basics", () => {
    const cards = [
      card({ arenaId: 1, quantity: 3, name: "Lightning Strike" }),
      card({ arenaId: 2, quantity: 60, name: "Lightning Strike", section: "sideboard" }),
      card({ arenaId: 3, quantity: 24, name: "Forest", typeLine: "Basic Land — Forest" }),
    ];
    const warnings = deckWarnings("Deck", cards);
    expect(warnings.some((w) => w.includes("More than 4 copies of Lightning Strike"))).toBe(true);
    expect(warnings.some((w) => w.includes("Forest"))).toBe(false);
  });

  test("flags missing printings", () => {
    const warnings = deckWarnings("Deck", [
      card({ arenaId: 9, quantity: 60, name: "Ghost Card", missing: true }),
    ]);
    expect(warnings.some((w) => w.includes("Ghost Card") && w.includes("no longer"))).toBe(true);
  });

  test("clean 60/15 deck has no warnings", () => {
    const main = [
      card({ arenaId: 1, quantity: 36, name: "Forest", typeLine: "Basic Land — Forest" }),
      card({ arenaId: 2, quantity: 4, name: "A" }),
      card({ arenaId: 3, quantity: 4, name: "B" }),
      card({ arenaId: 4, quantity: 4, name: "C" }),
      card({ arenaId: 5, quantity: 4, name: "D" }),
      card({ arenaId: 6, quantity: 4, name: "E" }),
      card({ arenaId: 7, quantity: 4, name: "F" }),
    ];
    const sideboard = Array.from({ length: 5 }, (_, i) =>
      card({ arenaId: 100 + i, quantity: 3, name: `Side ${i}`, section: "sideboard" }),
    );
    expect(deckWarnings("My Deck", [...main, ...sideboard])).toEqual([]);
  });
});

describe("card mutations", () => {
  test("addCard merges with existing rows per section", () => {
    let cards = addCard([], definition({ arenaId: 7 }), "main");
    cards = addCard(cards, definition({ arenaId: 7 }), "main");
    cards = addCard(cards, definition({ arenaId: 7 }), "sideboard");
    expect(cards).toHaveLength(2);
    expect(cards.find((c) => c.section === "main")?.quantity).toBe(2);
    expect(cards.find((c) => c.section === "sideboard")?.quantity).toBe(1);
  });

  test("adjustQuantity drops rows at zero", () => {
    const cards = [card({ arenaId: 7, quantity: 1 })];
    expect(adjustQuantity(cards, 7, "main", 1)[0].quantity).toBe(2);
    expect(adjustQuantity(cards, 7, "main", -1)).toHaveLength(0);
    expect(adjustQuantity(cards, 7, "sideboard", -1)).toHaveLength(1);
  });

  test("removeCard removes only the matching section row", () => {
    const cards = [card({ arenaId: 7 }), card({ arenaId: 7, section: "sideboard" })];
    const result = removeCard(cards, 7, "main");
    expect(result).toHaveLength(1);
    expect(result[0].section).toBe("sideboard");
  });

  test("moveCard merges quantities on collision", () => {
    const cards = [
      card({ arenaId: 7, quantity: 2 }),
      card({ arenaId: 7, quantity: 1, section: "sideboard" }),
    ];
    const result = moveCard(cards, 7, "main");
    expect(result).toHaveLength(1);
    expect(result[0].section).toBe("sideboard");
    expect(result[0].quantity).toBe(3);
  });

  test("sectionTotal sums quantities", () => {
    const cards = [card({ quantity: 4 }), card({ arenaId: 2, quantity: 2, section: "sideboard" })];
    expect(sectionTotal(cards, "main")).toBe(4);
    expect(sectionTotal(cards, "sideboard")).toBe(2);
  });
});

describe("sortProjectCards", () => {
  test("orders nonlands by mana value then name, lands last", () => {
    const cards = [
      card({ arenaId: 1, name: "Zenith", manaValue: 1 }),
      card({ arenaId: 2, name: "Forest", manaValue: 0, typeLine: "Basic Land — Forest" }),
      card({ arenaId: 3, name: "Alpha", manaValue: 1 }),
      card({ arenaId: 4, name: "Big Thing", manaValue: 6 }),
    ];
    expect(sortProjectCards(cards).map((c) => c.name)).toEqual(["Alpha", "Zenith", "Big Thing", "Forest"]);
  });
});
