import { describe, expect, test } from "bun:test";

import {
  draftPickLogPacks,
  draftReplayCoverage,
  draftSessionDurationSeconds,
  draftSessionRecord,
  draftSessionStatus,
  draftSessionStatusLabel,
  draftSessionType,
} from "../src/lib/draftReport";
import type { DraftPick, DraftSession } from "../src/lib/types";

function makeSession(overrides: Partial<DraftSession> = {}): DraftSession {
  return {
    id: 5,
    eventName: "PremierDraft_TMT_20260303",
    isBotDraft: false,
    startedAt: "2026-04-04T00:33:13Z",
    completedAt: "2026-04-04T00:46:25Z",
    picks: 42,
    wins: 7,
    losses: 2,
    ...overrides,
  };
}

function makePick(overrides: Partial<DraftPick> = {}): DraftPick {
  return {
    id: 1,
    packNumber: 0,
    pickNumber: 0,
    pickedCardIds: "[101]",
    packCardIds: "[]",
    pickTs: "2026-04-04T00:33:20Z",
    ...overrides,
  };
}

describe("draft session overview", () => {
  test("derives completion status without relying on pick count", () => {
    expect(draftSessionStatus(makeSession({ picks: 0 }))).toBe("complete");
    expect(draftSessionStatusLabel("complete")).toBe("Complete");
  });

  test("distinguishes active, incomplete, and empty recordings", () => {
    expect(draftSessionStatus(makeSession({ completedAt: "" }))).toBe("in-progress");
    expect(draftSessionStatus(makeSession({ startedAt: "", completedAt: "" }))).toBe(
      "recording-incomplete",
    );
    expect(draftSessionStatus(makeSession({ startedAt: "", completedAt: "", picks: 0 }))).toBe(
      "empty",
    );
  });

  test("calculates duration only from a valid forward timestamp range", () => {
    expect(draftSessionDurationSeconds(makeSession())).toBe(792);
    expect(draftSessionDurationSeconds(makeSession({ completedAt: "" }))).toBeNull();
    expect(
      draftSessionDurationSeconds(
        makeSession({
          startedAt: "2026-04-04T00:46:25Z",
          completedAt: "2026-04-04T00:33:13Z",
        }),
      ),
    ).toBeNull();
  });

  test("formats result and derives win rate when both sides are known", () => {
    expect(draftSessionRecord(makeSession())).toEqual({ label: "7–2", winRate: 7 / 9 });
    expect(draftSessionRecord(makeSession({ losses: null }))).toBeNull();
    expect(draftSessionRecord(makeSession({ wins: 0, losses: 0 }))).toEqual({
      label: "0–0",
      winRate: null,
    });
  });

  test("uses parsed event type and falls back to draft source", () => {
    expect(draftSessionType(makeSession())).toBe("Premier Draft");
    expect(draftSessionType(makeSession({ eventName: "", isBotDraft: true }))).toBe("Bot Draft");
    expect(draftSessionType(makeSession({ eventName: "", isBotDraft: false }))).toBe("Player Draft");
  });
});

describe("draft pick log", () => {
  test("groups and sorts zero-based Arena picks for one-based display", () => {
    const packs = draftPickLogPacks([
      makePick({ id: 3, packNumber: 1, pickNumber: 0, pickedCards: [{ cardId: 301, cardName: "C" }] }),
      makePick({ id: 2, packNumber: 0, pickNumber: 1, pickedCards: [{ cardId: 202, cardName: "B" }] }),
      makePick({ id: 1, packNumber: 0, pickNumber: 0, pickedCards: [{ cardId: 101, cardName: "A" }] }),
    ]);

    expect(packs.map((pack) => pack.displayPack)).toEqual([1, 2]);
    expect(packs[0].picks.map((pick) => pick.displayPick)).toEqual([1, 2]);
    expect(packs[0].picks.map((pick) => pick.pickedCards[0].cardName)).toEqual(["A", "B"]);
  });

  test("keeps one-based values unchanged and parses legacy card ID strings", () => {
    const [pack] = draftPickLogPacks([
      makePick({ packNumber: 1, pickNumber: 1, pickedCardIds: "cards: 901, 902" }),
    ]);

    expect(pack.displayPack).toBe(1);
    expect(pack.picks[0].displayPick).toBe(1);
    expect(pack.picks[0].pickedCards).toEqual([{ cardId: 901 }, { cardId: 902 }]);
  });

  test("counts only picks with recorded pack contents as replayable", () => {
    expect(
      draftReplayCoverage([
        makePick({ packCards: [{ cardId: 1 }] }),
        makePick({ id: 2, pickNumber: 1, packCards: [] }),
        makePick({ id: 3, pickNumber: 2 }),
      ]),
    ).toEqual({ covered: 1, total: 3 });
  });
});
