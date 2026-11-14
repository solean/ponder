import { describe, expect, test } from "bun:test";

import { matchTagSuggestions, normalizeMatchTag, STARTER_MATCH_TAGS, withMatchTag } from "../src/lib/matchTags";

describe("normalizeMatchTag", () => {
  test("trims, collapses whitespace and lowercases", () => {
    expect(normalizeMatchTag("  Land   Flooded ")).toBe("land flooded");
  });
  test("blank stays blank", () => {
    expect(normalizeMatchTag("   ")).toBe("");
  });
});

describe("withMatchTag", () => {
  test("adds in sorted order", () => {
    expect(withMatchTag(["mulligan"], "Misplayed")).toEqual(["misplayed", "mulligan"]);
  });
  test("ignores duplicates and blanks", () => {
    const tags = ["misplayed"];
    expect(withMatchTag(tags, " MISPLAYED ")).toBe(tags);
    expect(withMatchTag(tags, "  ")).toBe(tags);
  });
});

describe("matchTagSuggestions", () => {
  test("offers starters before anything is tagged", () => {
    expect(matchTagSuggestions([], [], "")).toEqual(STARTER_MATCH_TAGS);
  });
  test("uses tags in use and drops the match's own", () => {
    expect(matchTagSuggestions(["misplayed", "flooded"], ["flooded"], "")).toEqual(["misplayed"]);
  });
  test("prefix matches come before substring matches", () => {
    expect(matchTagSuggestions(["mana screwed", "screw-up", "misplayed"], [], "scr")).toEqual([
      "screw-up",
      "mana screwed",
    ]);
  });
});
