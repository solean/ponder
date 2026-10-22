import { expect, test } from "bun:test";
import { replayHandIdentityKnown } from "../src/lib/replay";
import type { MatchReplayFrameObject } from "../src/lib/types";
const hand = (cardId: number, visibility: string, instanceId = 1) => ({ cardId, visibility, instanceId, playerSide: "opponent", zoneType: "hand" }) as MatchReplayFrameObject;
test("current snapshots distinguish reveals, bounces, and hidden identities", () => {
  expect(replayHandIdentityKnown(hand(100, "public"))).toBe(true);
  expect(replayHandIdentityKnown(hand(100, "known"))).toBe(true);
  expect(replayHandIdentityKnown(hand(100, "private"))).toBe(false);
  expect(replayHandIdentityKnown(hand(0, "private"))).toBe(false);
});
test("scrubbing earlier and ID changes never borrow later identities", () => {
  const frames = [hand(0, "private"), hand(100, "public"), hand(0, "private", 2)];
  expect(frames.map(replayHandIdentityKnown)).toEqual([false, true, false]);
  expect(replayHandIdentityKnown(frames[0]!)).toBe(false);
});
