import { describe, expect, test } from "bun:test";

import { arenaTurnToFullTurn } from "../src/lib/turns";

describe("arenaTurnToFullTurn", () => {
  test("pairs both players' turns into one displayed full turn", () => {
    expect(arenaTurnToFullTurn(1)).toBe(1);
    expect(arenaTurnToFullTurn(2)).toBe(1);
    expect(arenaTurnToFullTurn(3)).toBe(2);
    expect(arenaTurnToFullTurn(4)).toBe(2);
    expect(arenaTurnToFullTurn(11)).toBe(6);
  });

  test("preserves pre-game and sentinel values", () => {
    expect(arenaTurnToFullTurn(0)).toBe(0);
    expect(arenaTurnToFullTurn(-1)).toBe(-1);
  });
});
