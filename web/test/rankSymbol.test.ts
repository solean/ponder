import { describe, expect, test } from "bun:test";

import { rankSymbolIndex } from "../src/lib/rankSymbol";

describe("rankSymbolIndex", () => {
  test("maps Arena rank classes and sub-tiers to the sprite", () => {
    expect(rankSymbolIndex({ rankClass: "Bronze", level: 1 })).toBe(1);
    expect(rankSymbolIndex({ rankClass: "Bronze", level: 4 })).toBe(4);
    expect(rankSymbolIndex({ rankClass: "Silver", level: 2 })).toBe(6);
    expect(rankSymbolIndex({ rankClass: "Gold", level: 3 })).toBe(11);
    expect(rankSymbolIndex({ rankClass: "Platinum", level: 4 })).toBe(16);
    expect(rankSymbolIndex({ rankClass: "Diamond", level: 1 })).toBe(17);
    expect(rankSymbolIndex({ rankClass: "Mythic", level: null })).toBe(21);
  });

  test("uses the neutral badge for unsupported or incomplete ranks", () => {
    expect(rankSymbolIndex(null)).toBe(0);
    expect(rankSymbolIndex({ rankClass: "", level: 2 })).toBe(0);
    expect(rankSymbolIndex({ rankClass: "Spark", level: 3 })).toBe(0);
    expect(rankSymbolIndex({ rankClass: "Gold", level: null })).toBe(0);
    expect(rankSymbolIndex({ rankClass: "Gold", level: 5 })).toBe(0);
  });
});
