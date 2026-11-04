import { expect, test } from "bun:test";
import { frontFaceIsLand, landDrawOdds, nextDrawOdds } from "../src/lib/drawOdds";

test("next draw uses current copies and library size, including zero and certainty", () => {
  expect(nextDrawOdds(4, 53)).toBe("7.5%");
  expect(nextDrawOdds(3, 52)).toBe("5.8%");
  expect(nextDrawOdds(0, 10)).toBe("0%");
  expect(nextDrawOdds(1, 1)).toBe("100%");
});

test("missing, empty or inconsistent counts have no percentage", () => {
  expect(nextDrawOdds(undefined, 53)).toBeUndefined();
  expect(nextDrawOdds(1, undefined)).toBeUndefined();
  expect(nextDrawOdds(0, 0)).toBeUndefined();
  expect(nextDrawOdds(3, 2)).toBeUndefined();
});

test("only a land front face counts toward a land draw", () => {
  expect(frontFaceIsLand("Land // Land")).toBe(true);
  expect(frontFaceIsLand("Sorcery // Land")).toBe(false);
  expect(frontFaceIsLand("Basic Land — Forest")).toBe(true);
  expect(frontFaceIsLand("")).toBeUndefined();
});

test("land odds require complete counts and types for the remaining main deck", () => {
  const cards = [
    { section: "main", remaining: 2, isLand: true },
    { section: "main", remaining: 3, isLand: false },
    { section: "main", remaining: 0 },
    { section: "sideboard", isLand: true },
  ];
  expect(landDrawOdds(cards, 5)).toBe("40%");
  expect(landDrawOdds(cards, 6)).toBeUndefined();
  expect(landDrawOdds([...cards, { section: "main", remaining: 1 }], 6)).toBeUndefined();
  expect(landDrawOdds([...cards, { section: "main", isLand: true }], 5)).toBeUndefined();
  expect(landDrawOdds([], 0)).toBeUndefined();
  expect(landDrawOdds([{ section: "main", remaining: 3, isLand: false }], 3)).toBe("0%");
});
