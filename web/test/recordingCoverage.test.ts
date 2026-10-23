import { expect, test } from "bun:test";
import { replayGameNumbers, replayRecordingCoverage } from "../src/lib/replay/recordingCoverage";
import type { MatchReplayFrame } from "../src/lib/types";

const frames: MatchReplayFrame[] = [
  { id: 11, gameNumber: 1, gameStateId: 1, gameStateType: "full", gameStage: "start" },
  { id: 12, gameNumber: 1, gameStateId: 2, prevGameStateId: 1, gameStage: "play", turnNumber: 1 },
  { id: 13, gameNumber: 1, gameStateId: 3, prevGameStateId: 2, gameStage: "gameover" },
];
const coverage = (raw = frames, extra = {}) => replayRecordingCoverage({ frames: raw, hasFallback: false, matchComplete: true, isLatestGame: true, ...extra });

test("complete requires raw start, play, terminal and linked states, independently of match result", () => {
  expect(coverage().state).toBe("complete");
  expect(coverage(frames, { matchComplete: false }).state).toBe("complete");
  expect(coverage(frames.slice(1)).state).toBe("partial");
  expect(coverage(frames.slice(0, 2)).state).toBe("partial");
  expect(coverage([frames[2]!]).state).toBe("partial");
});
test("gaps, repeated inherited previous IDs, and unknown links are never complete", () => {
  expect(coverage([frames[0]!, frames[2]!]).state).toBe("partial");
  expect(coverage(frames.map((frame, i) => i === 2 ? { ...frame, prevGameStateId: 1 } : frame)).state).toBe("partial");
  expect(coverage(frames.map((frame, i) => i === 1 ? { ...frame, prevGameStateId: undefined } : frame)).state).toBe("partial");
  expect(coverage(frames.map((frame, i) => i === 0 ? { ...frame, turnNumber: 15 } : frame)).state).toBe("partial");
  expect(coverage(frames.map((frame, i) => i === 0 ? { ...frame, gameStateType: "diff" } : frame)).state).toBe("partial");
});
test("only recent unfinished latest-game states indicate recording, not an unknown match result", () => {
  const now = Date.parse("2026-10-23T20:00:00Z");
  const live = [...frames.slice(0, 1), { ...frames[1]!, recordedAt: "2026-10-23T19:59:50Z" }];
  expect(coverage(live, { matchComplete: false, now }).state).toBe("recording");
  expect(coverage(live, { matchComplete: true, now }).state).toBe("partial");
  expect(coverage(live, { matchComplete: false, gameEnded: true, now }).state).toBe("partial");
  expect(coverage(live, { matchComplete: false, isLatestGame: false, now }).state).toBe("partial");
  expect(coverage(live, { matchComplete: false, now: now + 300_000 }).state).toBe("partial");
  expect(coverage(frames.slice(0, 2), { matchComplete: false, now }).state).toBe("partial");
});
test("empty historical games explain permanent absence and first-sighting limitations", () => {
  expect(coverage([]).state).toBe("missing");
  expect(coverage([]).detail).toContain("cannot recreate");
  expect(coverage([], { hasFallback: true }).state).toBe("fallback");
  expect(coverage([], { hasFallback: true }).detail).toContain("live stack cannot be reconstructed");
});
test("mixed BO3 includes replay-only, timeline-only and analytics-only games", () => {
  expect(replayGameNumbers(frames, [{ id: 1, gameNumber: 2, cardId: 10, playerSide: "self", firstPublicZone: "battlefield" }], [{ gameNumber: 3 }])).toEqual([1, 2, 3]);
  expect(replayGameNumbers([], [], [])).toEqual([]);
});

test("an empty open latest game waits, without inferring active ingestion", () => {
  expect(coverage([], { matchComplete: false }).state).toBe("waiting");
  expect(coverage([], { matchComplete: false }).detail).toContain("does not confirm");
  expect(coverage([], { matchComplete: false, isLatestGame: false }).state).toBe("missing");
  expect(coverage([], { matchComplete: false, gameEnded: true }).state).toBe("missing");
});
