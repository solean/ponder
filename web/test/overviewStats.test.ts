import { describe, expect, test } from "bun:test";

import {
  currentStreak,
  activityDayBoundaries,
  dailyActivity,
  labelDailyActivity,
  isLimitedEvent,
  matchAverages,
  recentForm,
  recordOf,
  recordWinRate,
  splitRecords,
  timeOfDayPerformance,
  weekdayPerformance,
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

describe("server activity helpers", () => {
  test("generates the complete trailing range through tomorrow's local midnight", () => {
    const now = new Date(2026, 6, 3, 12);
    const days = activityDayBoundaries(365, now);
    expect(days).toHaveLength(365);
    expect(days[0]).toEqual({ date: "2025-07-04", start: new Date(2025, 6, 4).toISOString(), end: new Date(2025, 6, 5).toISOString() });
    expect(days[364]).toEqual({ date: "2026-07-03", start: new Date(2026, 6, 3).toISOString(), end: new Date(2026, 6, 4).toISOString() });
    expect(days.slice(1).every((day, i) => day.start === days[i].end)).toBe(true);
  });

  test("uses 23/25-hour calendar days in the browser timezone", () => {
    // Isolate TZ from other tests so the host/browser's usual timezone cannot
    // hide DST mistakes or affect unrelated formatting tests.
    const helperPath = new URL("../src/lib/overviewStats.ts", import.meta.url).pathname;
    const result = Bun.spawnSync([process.execPath, "--eval", `
      import { activityDayBoundaries } from ${JSON.stringify(helperPath)};
      console.log(JSON.stringify([
        activityDayBoundaries(3, new Date(2026, 2, 9, 12)),
        activityDayBoundaries(3, new Date(2026, 10, 2, 12)),
      ]));
    `], { env: { ...process.env, TZ: "America/New_York" } });
    expect(result.exitCode).toBe(0);
    const [spring, fall] = JSON.parse(result.stdout.toString()) as ReturnType<typeof activityDayBoundaries>[];
    expect(spring.map((day) => (Date.parse(day.end) - Date.parse(day.start)) / 3_600_000)).toEqual([24, 23, 24]);
    expect(fall.map((day) => (Date.parse(day.end) - Date.parse(day.start)) / 3_600_000)).toEqual([24, 25, 24]);
    expect(spring[1]).toEqual({ date: "2026-03-08", start: "2026-03-08T05:00:00.000Z", end: "2026-03-09T04:00:00.000Z" });
    expect(fall[1]).toEqual({ date: "2026-11-01", start: "2026-11-01T04:00:00.000Z", end: "2026-11-02T05:00:00.000Z" });
  });

  test("labels server totals without capping or recomputing hover fields", () => {
    const totals = { date: "2026-07-03", count: 601, wins: 200, losses: 200, unknown: 201, trackedSeconds: 36000, timedMatches: 600, limited: 400, constructed: 201 };
    const [day] = labelDailyActivity([totals]);
    expect(day).toEqual({ ...totals, label: new Date(2026, 6, 3).toLocaleDateString(undefined, { month: "short", day: "numeric" }) });
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

describe("weekdayPerformance", () => {
  test("buckets by local weekday, Sunday first", () => {
    const buckets = weekdayPerformance([
      makeMatch({ startedAt: new Date(2026, 6, 5, 9, 0, 0).toISOString(), result: "win" }), // Sunday
      makeMatch({ startedAt: new Date(2026, 6, 5, 23, 0, 0).toISOString(), result: "loss" }), // Sunday
      makeMatch({ startedAt: new Date(2026, 6, 8, 9, 0, 0).toISOString(), result: "win" }), // Wednesday
      makeMatch({ startedAt: new Date(2026, 6, 8, 9, 0, 0).toISOString(), result: "unknown" }),
      makeMatch({ startedAt: "not-a-date" }),
    ]);

    expect(buckets).toHaveLength(7);
    expect(buckets.map((bucket) => bucket.key)).toEqual([0, 1, 2, 3, 4, 5, 6]);
    expect(buckets[0]).toMatchObject({ matches: 2, record: { wins: 1, losses: 1 } });
    expect(buckets[3]).toMatchObject({ matches: 2, record: { wins: 1, losses: 0 } });
    expect(buckets[1]).toMatchObject({ matches: 0, record: { wins: 0, losses: 0 } });
    expect(buckets.every((bucket) => bucket.label.length > 0)).toBe(true);
  });
});

describe("timeOfDayPerformance", () => {
  test("buckets into four-hour local blocks", () => {
    const buckets = timeOfDayPerformance([
      makeMatch({ startedAt: new Date(2026, 6, 5, 0, 30, 0).toISOString(), result: "win" }),
      makeMatch({ startedAt: new Date(2026, 6, 5, 3, 59, 0).toISOString(), result: "loss" }),
      makeMatch({ startedAt: new Date(2026, 6, 5, 4, 0, 0).toISOString(), result: "win" }),
      makeMatch({ startedAt: new Date(2026, 6, 5, 23, 15, 0).toISOString(), result: "loss" }),
    ]);

    expect(buckets.map((bucket) => bucket.key)).toEqual([0, 4, 8, 12, 16, 20]);
    expect(buckets[0]).toMatchObject({ matches: 2, record: { wins: 1, losses: 1 } });
    expect(buckets[1]).toMatchObject({ matches: 1, record: { wins: 1, losses: 0 } });
    expect(buckets[5]).toMatchObject({ matches: 1, record: { wins: 0, losses: 1 } });
    expect(buckets.every((bucket) => bucket.range.length > 0)).toBe(true);
  });
});
