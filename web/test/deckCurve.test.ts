import { describe, expect, test } from "bun:test";

import {
  curveBucketLabel,
  curveColorKey,
  deckCurve,
  primaryCardType,
  type CurveCard,
} from "../src/lib/deckCurve";

function card(overrides: Partial<CurveCard>): CurveCard {
  return { cardId: 1, name: "Card", copies: 1, manaValue: 2, typeLine: "Instant", colors: ["U"], ...overrides };
}

describe("deck curve", () => {
  test("counts copies per mana value and folds 6+ into the last bucket", () => {
    const curve = deckCurve([
      card({ cardId: 1, manaValue: 1 }),
      card({ cardId: 2, manaValue: 2, copies: 2, typeLine: "Creature — Bird" }),
      card({ cardId: 3, manaValue: 6 }),
      card({ cardId: 4, manaValue: 8, typeLine: "Legendary Creature — Dragon" }),
    ]);
    expect(curve.buckets.map((bucket) => bucket.count)).toEqual([0, 1, 2, 0, 0, 0, 2]);
    expect(curve.total).toBe(5);
    expect(curve.creatureCount).toBe(3);
    expect(curve.topEndCount).toBe(2);
    expect(curve.averageManaValue).toBeCloseTo((1 + 4 + 6 + 8) / 5, 6);
  });

  test("leaves out lands, unused cards and cards without a mana value", () => {
    const curve = deckCurve([
      card({ cardId: 1, typeLine: "Basic Land — Island", manaValue: 0 }),
      card({ cardId: 2, copies: 0 }),
      card({ cardId: 3, manaValue: null, copies: 2 }),
      card({ cardId: 4, manaValue: 3 }),
    ]);
    expect(curve.total).toBe(1);
    expect(curve.unknownCount).toBe(2);
    expect(curve.buckets[0].count).toBe(0);
  });

  test("picks the cheapest of the tallest buckets as the peak", () => {
    const curve = deckCurve([
      card({ cardId: 1, manaValue: 3, copies: 2 }),
      card({ cardId: 2, manaValue: 2, copies: 2 }),
    ]);
    expect(curve.peakBucket).toBe(2);
    expect(deckCurve([]).peakBucket).toBeNull();
    expect(deckCurve([]).averageManaValue).toBeNull();
  });

  test("counts nonland copies per color, multicolor cards toward each color", () => {
    const curve = deckCurve([
      card({ cardId: 1, colors: ["U"], copies: 2 }),
      card({ cardId: 2, colors: ["W", "U"] }),
      card({ cardId: 3, colors: ["B"], manaValue: null }),
      card({ cardId: 4, colors: ["G"], typeLine: "Land", manaValue: 0 }),
      card({ cardId: 5, colors: [] }),
    ]);
    expect(curve.colorCounts).toEqual([
      { color: "W", count: 1 },
      { color: "U", count: 3 },
      { color: "B", count: 1 },
    ]);
  });

  test("lists creatures first, then by name", () => {
    const curve = deckCurve([
      card({ cardId: 1, name: "Opt" }),
      card({ cardId: 2, name: "Zephyr Sprite", typeLine: "Creature — Faerie" }),
      card({ cardId: 3, name: "Bird", typeLine: "Artifact Creature — Bird" }),
    ]);
    expect(curve.buckets[2].cards.map((entry) => entry.name)).toEqual(["Bird", "Zephyr Sprite", "Opt"]);
    expect(curve.buckets[2].creatureCount).toBe(2);
  });
});

describe("deck curve labels", () => {
  test("reads the main type from a type line", () => {
    expect(primaryCardType("Legendary Creature — Human Wizard")).toBe("Creature");
    expect(primaryCardType("Artifact Creature — Golem")).toBe("Creature");
    expect(primaryCardType("Enchantment — Aura")).toBe("Enchantment");
    expect(primaryCardType("Kindred Instant — Elf")).toBe("Instant");
    expect(primaryCardType("Instant // Sorcery")).toBe("Instant");
    expect(primaryCardType(undefined)).toBe("Card");
  });

  test("groups colors for the color view", () => {
    expect(curveColorKey(["U"])).toBe("U");
    expect(curveColorKey(["W", "U"])).toBe("M");
    expect(curveColorKey([])).toBe("C");
    expect(curveColorKey(undefined)).toBe("C");
  });

  test("labels the top bucket 6+", () => {
    expect(curveBucketLabel(3)).toBe("3");
    expect(curveBucketLabel(6)).toBe("6+");
  });
});
