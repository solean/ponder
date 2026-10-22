import { expect, test } from "bun:test";
import { buildReplayRelationshipIndex } from "../src/lib/replay";
import { isReplayDecision, replayFrameMatches, replayNavigationIndex } from "../src/lib/replay/navigation";
import type { MatchReplayFrame } from "../src/lib/types";
const frame: MatchReplayFrame = { id: 7, changes: [
  { instanceId: 1, cardId: 1, cardName: "Lightning Bolt", playerSide: "self", action: "move_public", fromZoneType: "Hand", toZoneType: "Stack", isToken: false },
  { instanceId: 2, cardId: 2, cardName: "Hidden second card", playerSide: "opponent", action: "move_public", fromZoneType: "Battlefield", toZoneType: "Graveyard", isToken: false },
] };
test("search includes secondary cards and categories intersect search", () => {
 const relationships = buildReplayRelationshipIndex([frame]);
 expect(replayFrameMatches(frame, relationships, " SECOND CARD ", "removal")).toBe(true);
 expect(replayFrameMatches(frame, relationships, "Bolt", "spells")).toBe(true);
 expect(replayFrameMatches(frame, relationships, "Bolt", "combat")).toBe(false);
 expect(replayFrameMatches(frame, relationships, "missing", "all")).toBe(false);
 expect(replayFrameMatches(frame, relationships, "", "all")).toBe(true);
});
test("decision navigation preserves original indices and detailed access", () => {
 expect(isReplayDecision({ id: 2, annotationsJson: "[]", changes: [{ ...frame.changes![0]!, action: "tap" }] })).toBe(false);
 expect(isReplayDecision(frame)).toBe(true);
 expect(replayNavigationIndex(1, 1, 9, [0, 4, 8])).toBe(4);
 expect(replayNavigationIndex(7, -1, 9, [0, 4, 8])).toBe(4);
 expect(replayNavigationIndex(1, 1, 9)).toBe(2);
});

test("decision mode keeps life-only events and ignores irrelevant annotations", () => {
 const previous = { id: 1, selfLifeTotal: 20 };
 const life = { id: 2, selfLifeTotal: 17 };
 expect(isReplayDecision(life, previous, buildReplayRelationshipIndex([previous, life]))).toBe(true);
 const routine = { id: 3, annotationsJson: '[{"type":["AnnotationType_ZoneUpdate"],"affectedIds":[1]}]' };
 expect(isReplayDecision(routine, previous, buildReplayRelationshipIndex([previous, routine]))).toBe(false);
 const cast = { id: 4, changes: [{ ...frame.changes![0]!, action: "cast", toZoneType: undefined, fromZoneType: undefined }] };
 expect(replayFrameMatches(cast, buildReplayRelationshipIndex([cast]), "", "spells")).toBe(true);
});

test("unknown game result and stage do not promote a routine frame", () => {
 const routine = { id: 9, winningPlayerSide: "unknown" as const, gameStage: "unknown", changes: [{ ...frame.changes![0]!, action: "tap" }] };
 expect(isReplayDecision(routine, undefined, buildReplayRelationshipIndex([routine]))).toBe(false);
});


test("search and decision mode ignore same-zone housekeeping", () => {
 const noise = { id: 9, winningPlayerSide: "unknown" as const, changes: [{ ...frame.changes![0]!, cardName: "Craftwork Crusher", fromZoneType: "Battlefield", toZoneType: "Battlefield" }] };
 const relationships = buildReplayRelationshipIndex([noise]);
 expect(isReplayDecision(noise, undefined, relationships)).toBe(false);
 expect(replayFrameMatches(noise, relationships, "Craftwork", "all")).toBe(false);
});
