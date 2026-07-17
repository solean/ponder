import { describe, expect, test } from "bun:test";

import {
  currentStreak,
  dailyActivity,
  isLimitedEvent,
  matchAverages,
  recentForm,
  recordOf,
  recordWinRate,
  splitRecords,
} from "../src/lib/overviewStats";
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

describe("recordOf", () => {
  test("counts wins and losses, ignores unknown", () => {
    const record = recordOf([
      makeMatch({ result: "win" }),
      makeMatch({ result: "loss" }),
      makeMatch({ result: "unknown" }),
      makeMatch({ result: "win" }),
    ]);
    expect(record).toEqual({ wins: 2, losses: 1 });
    expect(recordWinRate(record)).toBeCloseTo(2 / 3);
  });

  test("win rate is null with no decided matches", () => {
    expect(recordWinRate({ wins: 0, losses: 0 })).toBeNull();
  });
});

describe("recentForm", () => {
  test("takes the most recent decided matches only", () => {
    const matches = [
      makeMatch({ result: "win" }),
      makeMatch({ result: "unknown" }),
      makeMatch({ result: "loss" }),
      makeMatch({ result: "loss" }),
    ];
    expect(recentForm(matches, 2)).toEqual({ wins: 1, losses: 1 });
  });
});

describe("currentStreak", () => {
  test("counts the leading run and skips unknowns", () => {
    const matches = [
      makeMatch({ result: "win" }),
      makeMatch({ result: "unknown" }),
      makeMatch({ result: "win" }),
      makeMatch({ result: "loss" }),
      makeMatch({ result: "win" }),
    ];
    expect(currentStreak(matches)).toEqual({ result: "win", length: 2 });
  });

  test("returns null when nothing is decided", () => {
    expect(currentStreak([makeMatch({ result: "unknown" })])).toBeNull();
  });
});

describe("isLimitedEvent", () => {
  test.each([
    ["QuickDraft_TMT_20260313", true],
    ["FIN_Quick_Draft", true],
    ["PremierSealed_ECL", true],
    ["JumpIn", true],
    ["Traditional_Ladder", false],
    ["Play", false],
    ["Brawl", false],
  ])("%s → %p", (eventName, expected) => {
    expect(isLimitedEvent(eventName)).toBe(expected);
  });
});

describe("splitRecords", () => {
  test("splits by format, initiative, and match type", () => {
    const matches = [
      makeMatch({ result: "win", eventName: "Ladder", playDraw: "play", bestOf: "bo1" }),
      makeMatch({ result: "loss", eventName: "Ladder", playDraw: "draw", bestOf: "bo3" }),
      makeMatch({ result: "win", eventName: "QuickDraft_TMT_20260313", playDraw: "draw", bestOf: "bo1" }),
      makeMatch({ result: "unknown", eventName: "Ladder", playDraw: "play", bestOf: "bo1" }),
    ];
    const splits = splitRecords(matches);
    expect(splits.constructed).toEqual({ wins: 1, losses: 1 });
    expect(splits.limited).toEqual({ wins: 1, losses: 0 });
    expect(splits.play).toEqual({ wins: 1, losses: 0 });
    expect(splits.draw).toEqual({ wins: 1, losses: 1 });
    expect(splits.bo1).toEqual({ wins: 2, losses: 0 });
    expect(splits.bo3).toEqual({ wins: 0, losses: 1 });
  });
});

describe("dailyActivity", () => {
  test("buckets matches into trailing local days, oldest first", () => {
    const now = new Date(2026, 6, 3, 12, 0, 0); // July 3 2026 local
    const matches = [
      makeMatch({ startedAt: new Date(2026, 6, 3, 9, 0, 0).toISOString() }),
      makeMatch({ startedAt: new Date(2026, 6, 3, 10, 0, 0).toISOString() }),
      makeMatch({ startedAt: new Date(2026, 6, 1, 22, 0, 0).toISOString() }),
      makeMatch({ startedAt: new Date(2026, 5, 1, 22, 0, 0).toISOString() }), // outside window
    ];
    const days = dailyActivity(matches, 3, now);
    expect(days).toHaveLength(3);
    expect(days.map((day) => day.count)).toEqual([1, 0, 2]);
    expect(days[2].date).toBe("2026-07-03");
  });

  test("summarizes daily record, tracked time, and format mix", () => {
    const now = new Date(2026, 6, 3, 12, 0, 0);
    const startedAt = new Date(2026, 6, 3, 9, 0, 0).toISOString();
    const [day] = dailyActivity([
      makeMatch({
        startedAt,
        result: "win",
        eventName: "QuickDraft_TMT_20260313",
        secondsCount: 600,
      }),
      makeMatch({
        startedAt,
        result: "loss",
        eventName: "Traditional_Ladder",
        secondsCount: 300,
      }),
      makeMatch({
        startedAt,
        result: "unknown",
        eventName: "Play",
        secondsCount: null,
      }),
    ], 1, now);

    expect(day).toMatchObject({
      count: 3,
      wins: 1,
      losses: 1,
      unknown: 1,
      trackedSeconds: 900,
      timedMatches: 2,
      limited: 1,
      constructed: 2,
    });
  });
});

describe("matchAverages", () => {
  test("averages only matches with data", () => {
    const matches = [
      makeMatch({ secondsCount: 600, turnCount: 10 }),
      makeMatch({ secondsCount: 1200, turnCount: null }),
      makeMatch({ secondsCount: null, turnCount: 20 }),
    ];
    expect(matchAverages(matches)).toEqual({ seconds: 900, turns: 15 });
  });

  test("returns nulls with no data", () => {
    expect(matchAverages([makeMatch({ secondsCount: null, turnCount: null })])).toEqual({
      seconds: null,
      turns: null,
    });
  });
});
