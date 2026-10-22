import { expect, test } from "bun:test";
import { buildReplayRelationshipIndex } from "../src/lib/replay";
import { replayEventDetails } from "../src/lib/replay/eventDetails";
import type { MatchReplayFrame } from "../src/lib/types";

test("details retain every attacker and transfer card in original order", () => {
  const frame: MatchReplayFrame = { id: 1, changes: ["Alpha", "Beta", "Gamma", "Delta"].map((cardName, index) => ({ instanceId: index + 1, cardId: index + 1, cardName, playerSide: "self", action: index < 2 ? "attack" : "move_public", fromZoneType: index === 2 ? "Library" : "Hand", toZoneType: "Graveyard", isToken: false })) };
  const relationships = buildReplayRelationshipIndex([frame]);
  relationships.zoneTransferCategoryByFrameId.set(1, new Map([[3, "Mill"], [4, "Discard"]]));
  expect(replayEventDetails(frame, relationships)).toEqual([
    "You attacked with Alpha.", "You attacked with Beta.", "You milled Gamma.", "You discarded Delta.",
  ]);
});

test("multi-target and repeated damage preserve quantities, targets and ordering", () => {
  const frame = { id: 1 };
  const relationships = buildReplayRelationshipIndex([frame]);
  const target = (label: string, targetId: number) => ({ label, targetId, connectionId: targetId });
  relationships.targetEventsByFrameId.set(1, [{ sourceId: 4, sourceLabel: "Fire", sourceKind: "spell", targets: [target("Alpha", 1), target("Opponent", 2)] }]);
  relationships.damageEventsByFrameId.set(1, [
    { sourceId: 4, sourceLabel: "Fire", target: target("Alpha", 1), amount: 2 },
    { sourceId: 4, sourceLabel: "Fire", target: target("Opponent", 2), amount: 3 },
    { sourceId: 4, sourceLabel: "Fire", target: target("Opponent", 2), amount: 3 },
  ]);
  expect(replayEventDetails(frame, relationships)).toEqual([
    "Fire targets Alpha, Opponent.", "Fire deals 2 damage to Alpha.", "Fire deals 3 damage to Opponent.", "Fire deals 3 damage to Opponent.",
  ]);
});

test("unknown hand entries remain hidden and do not claim public reveals", () => {
 const frame: MatchReplayFrame = { id: 11, changes: [{ instanceId: 99, cardId: 0, playerSide: "opponent", action: "enter_public", toZoneType: "Hand", isToken: false }] };
 const relationships = buildReplayRelationshipIndex([frame]);
 expect(replayEventDetails(frame, relationships)).toEqual(["Hidden card entered Opponent's hand."]);
 relationships.zoneTransferCategoryByFrameId.set(frame.id, new Map([[99, "Draw"]]));
 expect(replayEventDetails(frame, relationships)).toEqual(["Opponent drew Hidden card."]);
});


test("event details omit same-zone housekeeping", () => {
 const frame: MatchReplayFrame = { id: 7, changes: [{ instanceId: 1, cardId: 1, cardName: "Forest", playerSide: "self", action: "move_public", fromZoneType: "Battlefield", toZoneType: "Battlefield", isToken: false }] };
 expect(replayEventDetails(frame, buildReplayRelationshipIndex([frame]))).toEqual([]);
});
