import { describe, expect, test } from "bun:test";

import { manaSymbolAssetName } from "../src/lib/manaSymbols";

describe("manaSymbolAssetName", () => {
  test("maps ordinary and hybrid mana tokens to local asset names", () => {
    expect(manaSymbolAssetName("R")).toBe("R");
    expect(manaSymbolAssetName(" w / u ")).toBe("WU");
    expect(manaSymbolAssetName("B/G/P")).toBe("BGP");
    expect(manaSymbolAssetName("2/R")).toBe("2R");
  });

  test("maps Scryfall's non-literal filenames", () => {
    expect(manaSymbolAssetName("½")).toBe("HALF");
    expect(manaSymbolAssetName("∞")).toBe("INFINITY");
  });

  test("rejects tokens that cannot be local asset filenames", () => {
    expect(manaSymbolAssetName("")).toBeNull();
    expect(manaSymbolAssetName("../R")).toBeNull();
    expect(manaSymbolAssetName("W+U")).toBeNull();
  });
});
