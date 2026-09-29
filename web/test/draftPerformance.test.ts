import { describe, expect, test } from "bun:test";

import { buildDraftPerformance, draftPerformanceOptions } from "../src/lib/draftPerformance";
import type { DraftSession } from "../src/lib/types";

let nextId = 1;

function makeSession(overrides: Partial<DraftSession> = {}): DraftSession {
  const id = nextId++;
  return {
    id,
    eventName: "PremierDraft_HOB_20260811",
    isBotDraft: false,
    startedAt: new Date(Date.UTC(2026, 7, 20, 0, id)).toISOString(),
    completedAt: "",
    picks: 42,
    wins: 3,
    losses: 3,
    economy: null,
    ...overrides,
  };
}

const ALL = { setCode: null, format: null };

describe("buildDraftPerformance", () => {
  test("pools match results rather than averaging event rates", () => {
    const perf = buildDraftPerformance(
      [makeSession({ wins: 7, losses: 1 }), makeSession({ wins: 0, losses: 3 })],
      ALL,
    );
    expect(perf.points.map((point) => point.all)).toEqual([7 / 8, 7 / 11]);
    expect(perf.wins).toBe(7);
    expect(perf.losses).toBe(4);
    expect(perf.trophies).toBe(1);
  });

  test("orders events chronologically and skips sessions without a result", () => {
    const late = makeSession({ startedAt: "2026-09-02T00:00:00Z", wins: 1, losses: 3 });
    const early = makeSession({ startedAt: "2026-09-01T00:00:00Z", wins: 5, losses: 3 });
    const unplayed = makeSession({ wins: null, losses: null });
    const zeroZero = makeSession({ wins: 0, losses: 0 });
    const perf = buildDraftPerformance([late, unplayed, early, zeroZero], ALL);
    expect(perf.points.map((point) => point.sessionId)).toEqual([early.id, late.id]);
    expect(perf.points.map((point) => point.eventNumber)).toEqual([1, 2]);
  });

  test("trailing windows stay empty until they are full, then slide", () => {
    const drafts = Array.from({ length: 12 }, (_, index) =>
      makeSession(index < 2 ? { wins: 0, losses: 3 } : { wins: 7, losses: 0 }),
    );
    const perf = buildDraftPerformance(drafts, ALL);
    expect(perf.points[8].rolling[10]).toBeNull();
    expect(perf.points[9].rolling[10]).toBeCloseTo(56 / 62);
    expect(perf.points[11].rolling[10]).toBe(1);
    expect(perf.points.every((point) => point.rolling[50] == null)).toBe(true);
  });

  test("filters by set and format before numbering events", () => {
    const drafts = [
      makeSession({ eventName: "PremierDraft_HOB_20260811" }),
      makeSession({ eventName: "QuickDraft_HOB_20260820", wins: 7, losses: 1 }),
      makeSession({ eventName: "PremierDraft_LTR_20260825" }),
      makeSession({ eventName: "TradDraft_HOB_20260811", wins: 3, losses: 0 }),
    ];
    const hob = buildDraftPerformance(drafts, { setCode: "HOB", format: null });
    expect(hob.points).toHaveLength(3);
    expect(hob.trophies).toBe(2);

    const premierHob = buildDraftPerformance(drafts, { setCode: "HOB", format: "Premier Draft" });
    expect(premierHob.points.map((point) => point.eventNumber)).toEqual([1]);
  });
});

describe("draftPerformanceOptions", () => {
  test("lists sets newest first and formats by volume", () => {
    const options = draftPerformanceOptions([
      makeSession({ eventName: "PremierDraft_LTR_20260825", startedAt: "2026-08-26T00:00:00Z" }),
      makeSession({ eventName: "PremierDraft_HOB_20260811", startedAt: "2026-09-10T00:00:00Z" }),
      makeSession({ eventName: "QuickDraft_HOB_20260820", startedAt: "2026-08-22T00:00:00Z" }),
      makeSession({ eventName: "QuickDraft_FIN_20250619", startedAt: "2026-08-01T00:00:00Z", wins: null, losses: null }),
    ]);
    expect(options.sets).toEqual(["HOB", "LTR"]);
    expect(options.formats).toEqual(["Premier Draft", "Quick Draft"]);
  });
});
