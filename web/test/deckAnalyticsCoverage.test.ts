import { describe, expect, test } from "bun:test";

import {
  buildDeckAnalyticsCoveragePresentation,
  coverageSampleLabel,
} from "../src/lib/deckAnalyticsCoverage";
import type { DeckAnalyticsCoverage } from "../src/lib/types";

const complete: DeckAnalyticsCoverage = {
  matches: 5,
  matchesWithVersion: 5,
  gameCount: 12,
  gamesWithResult: 12,
  gamesWithOpeningHand: 12,
  gamesWithPlayDraw: 12,
  gamesWithCardStats: 12,
  gamesWithTurnStats: 12,
  gamesWithLandJudged: 12,
};

function coverage(overrides: Partial<DeckAnalyticsCoverage> = {}): DeckAnalyticsCoverage {
  return { ...complete, ...overrides };
}

describe("deck analytics coverage presentation", () => {
  test("collapses complete coverage into one concise status", () => {
    expect(buildDeckAnalyticsCoveragePresentation(complete)).toEqual({
      state: "complete",
      statusLabel: "All 12 games analyzed",
      issues: [],
      issueSummary: "",
      versionNote: undefined,
    });
  });

  test.each([
    "gamesWithResult",
    "gamesWithOpeningHand",
    "gamesWithPlayDraw",
    "gamesWithCardStats",
    "gamesWithTurnStats",
    "gamesWithLandJudged",
  ] as const)("treats incomplete %s as partial coverage", (field) => {
    const result = buildDeckAnalyticsCoveragePresentation(coverage({ [field]: 11 }));
    expect(result.state).toBe("partial");
    expect(result.issues).toHaveLength(1);
    expect(result.issues[0]?.available).toBe(11);
  });

  test("lists only missing datasets and summarizes the first two", () => {
    const result = buildDeckAnalyticsCoveragePresentation(
      coverage({
        gamesWithOpeningHand: 9,
        gamesWithTurnStats: 10,
        gamesWithLandJudged: 8,
      }),
    );

    expect(result.issues.map(({ label, available, total }) => `${label} — ${available} of ${total} games`)).toEqual([
      "Opening-hand analysis — 9 of 12 games",
      "Turn analysis — 10 of 12 games",
      "Land-drop analysis — 8 of 12 games",
    ]);
    expect(result.issueSummary).toBe("Opening hands 9/12 · Turn data 10/12 · +1 more");
  });

  test("reports unversioned matches independently in the all-versions view", () => {
    expect(
      buildDeckAnalyticsCoveragePresentation(coverage({ matchesWithVersion: 3 }), 0).versionNote,
    ).toBe("2 matches aren’t linked to a deck version");
    expect(
      buildDeckAnalyticsCoveragePresentation(coverage({ matchesWithVersion: 4 }), 0).versionNote,
    ).toBe("1 match isn’t linked to a deck version");
    expect(
      buildDeckAnalyticsCoveragePresentation(coverage({ matchesWithVersion: 3 }), 7).versionNote,
    ).toBeUndefined();
  });

  test("keeps the empty state free of misleading data gaps", () => {
    const result = buildDeckAnalyticsCoveragePresentation(
      coverage({
        gameCount: 0,
        gamesWithResult: 0,
        gamesWithOpeningHand: 0,
        gamesWithPlayDraw: 0,
        gamesWithCardStats: 0,
        gamesWithTurnStats: 0,
        gamesWithLandJudged: 0,
      }),
    );
    expect(result.state).toBe("empty");
    expect(result.issues).toEqual([]);
  });

  test("formats section-local sample labels only when coverage is incomplete", () => {
    expect(coverageSampleLabel(9, 12)).toBe("9 of 12 games");
    expect(coverageSampleLabel(1, 1)).toBeUndefined();
    expect(coverageSampleLabel(0, 0)).toBeUndefined();
  });
});
