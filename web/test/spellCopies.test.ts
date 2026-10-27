import { expect, test } from "bun:test";
import { buildReplayBeat, describeReplayChange, replayObjectStatePills } from "../src/lib/replay";
import type { MatchReplayChange, MatchReplayFrameObject } from "../src/lib/types";

const copy: MatchReplayChange = {
  instanceId: 236,
  cardId: 106300,
  cardName: "Omit Variables",
  playerSide: "self",
  action: "enter_public",
  toZoneType: "stack",
  isToken: false,
  isCopy: true,
};

test("replay distinguishes observed stack copies from confirmed casts", () => {
  const observed = buildReplayBeat({ id: 1, changes: [copy] }, null);
  expect(observed.text).toBe("A copy of Omit Variables appears on the stack");
  const cast = buildReplayBeat({ id: 1, changes: [{ ...copy, castConfirmed: true }] }, null);
  expect(cast.text).toBe("You cast a copy of Omit Variables");
  const physical = buildReplayBeat({ id: 1, changes: [{ ...copy, isCopy: false }] }, null);
  expect(physical.text).toBe("You cast Omit Variables");
  expect(describeReplayChange(copy)).toContain("copy of Omit Variables");
});

test("copy objects receive their own replay label without becoming tokens", () => {
  const object: MatchReplayFrameObject = {
    id: 1, frameId: 1, instanceId: 236, cardId: 106300,
    playerSide: "self", zoneType: "stack", isCopy: true,
    isToken: false, isTapped: false, hasSummoningSickness: false,
  };
  expect(replayObjectStatePills(object)).toContainEqual({ label: "Copy", count: 1 });
});
