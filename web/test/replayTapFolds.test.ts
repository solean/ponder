import { expect, test } from "bun:test";

import { replayTapFolds } from "../src/lib/replay/tapFolds";

const tap = (instanceId: number, playerSide: "self" | "opponent" = "self") => ({
  instanceId,
  cardId: instanceId,
  cardName: "Island",
  action: "tap",
  playerSide,
  isToken: false,
});
const cast = (playerSide: "self" | "opponent" = "self") => ({
  instanceId: 50,
  cardId: 50,
  cardName: "Opt",
  action: "move_public",
  fromZoneType: "ZoneType_Hand",
  toZoneType: "ZoneType_Stack",
  playerSide,
  isToken: false,
});

test("a tap-only frame folds into the adjacent cast by the same player", () => {
  const frames = [
    { id: 1, turnNumber: 3, changes: [cast()] },
    { id: 2, turnNumber: 3, changes: [tap(1), tap(2)] },
    { id: 3, turnNumber: 3 },
  ];
  expect(replayTapFolds(frames)).toEqual(new Map([[1, { hostIndex: 0, count: 2 }]]));
});

test("taps before the cast fold forward", () => {
  const frames = [
    { id: 1, turnNumber: 3, changes: [tap(1)] },
    { id: 2, turnNumber: 3, changes: [cast()] },
  ];
  expect(replayTapFolds(frames)).toEqual(new Map([[0, { hostIndex: 1, count: 1 }]]));
});

test("taps stay their own step without a matching cast", () => {
  const frames = [
    { id: 1, turnNumber: 3, changes: [cast("opponent")] },
    { id: 2, turnNumber: 3, changes: [tap(1)] },
    { id: 3, turnNumber: 4, changes: [cast()] },
  ];
  expect(replayTapFolds(frames).size).toBe(0);
});
