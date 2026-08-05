import { describe, expect, test } from "bun:test";

import {
  formatRecord,
  formatWinRate,
  matchListSummary,
  tallyRecord,
  type MatchRecord,
} from "../src/lib/matchListSummary";
import type { Match } from "../src/lib/types";

function makeMatch(overrides: Partial<Match>): Match {
  return {
    id: 1,
    arenaMatchId: "m",
    eventName: "Ladder",
    opponent: "Opp",
    startedAt: "2026-07-01T12:00:00Z",
    endedAt: "2026-07-01T12:15:00Z",
    result: "win",
    winReason: "",
    ...overrides,
  };
}

function record(wins: number, losses: number, unknown = 0): MatchRecord {
  return { wins, losses, unknown };
}

describe("tallyRecord", () => {
  test("counts anything that is neither a win nor a loss as unknown", () => {
    const tally = tallyRecord([
      makeMatch({ result: "win" }),
      makeMatch({ result: "loss" }),
      makeMatch({ result: "draw" }),
      makeMatch({ result: "unknown" }),
    ]);
    expect(tally).toEqual({ wins: 1, losses: 1, unknown: 2 });
  });
});

describe("formatWinRate", () => {
  test("excludes undecided matches from the denominator", () => {
    expect(formatWinRate(record(3, 1, 96))).toBe("75%");
  });

  test("renders a dash rather than dividing by zero", () => {
    expect(formatWinRate(record(0, 0, 5))).toBe("-");
  });

  test("formatRecord omits the unknown bucket", () => {
    expect(formatRecord(record(3, 1, 96))).toBe("3-1");
  });
});

describe("matchListSummary", () => {
  test("reports the fetched count when the whole corpus was fetched", () => {
    expect(
      matchListSummary({
        shown: 10010,
        fetched: 10010,
        total: 10010,
        filtersActive: false,
        record: record(5733, 4095, 182),
      }),
    ).toBe("10010 matches • 5733-4095 (58%)");
  });

  // The bug this module exists to prevent: a capped fetch used to render its
  // own length as the user's lifetime count, so a 10k-match player saw "1000
  // matches" and a win rate computed from only the newest 1000.
  test("discloses the server total when the fetch was capped", () => {
    expect(
      matchListSummary({
        shown: 1000,
        fetched: 1000,
        total: 10010,
        filtersActive: false,
        record: record(617, 369, 14),
      }),
    ).toBe("showing 1000 of 10,010 matches • 617-369 (63%)");
  });

  test("keeps the filtered count relative to the fetched rows", () => {
    expect(
      matchListSummary({
        shown: 12,
        fetched: 1000,
        total: 1000,
        filtersActive: true,
        record: record(9, 3),
      }),
    ).toBe("12 of 1000 matches • 9-3 (75%)");
  });

  test("distinguishes the filtered subset from the fetched cap and the true total", () => {
    expect(
      matchListSummary({
        shown: 12,
        fetched: 1000,
        total: 10010,
        filtersActive: true,
        record: record(9, 3),
      }),
    ).toBe("12 of 1000 shown (10,010 total) • 9-3 (75%)");
  });

  test("never claims a total it did not tally over", () => {
    const summary = matchListSummary({
      shown: 1000,
      fetched: 1000,
      total: 10010,
      filtersActive: false,
      record: record(617, 369),
    });
    expect(summary.startsWith("10010 matches")).toBe(false);
    expect(summary).toContain("of 10,010");
  });
});
