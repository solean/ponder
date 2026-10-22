import type { MatchReplayFrame } from "../types";
import { boardZoneKind, cardDisplayName, describeReplayChange, replayChangeIsNarratable, replayFrameAttachmentEvents, replayFrameCrewEvents, replayRelationshipTargetForId, timelinePlayerLabel, type ReplayRelationshipIndex } from "./index";

/** Preserve change order and annotation order; never collapse repeated damage. */
export function replayEventDetails(frame: MatchReplayFrame, relationships: ReplayRelationshipIndex): string[] {
  const details: string[] = [];
  const transfers = relationships.zoneTransferCategoryByFrameId.get(frame.id);
  for (const change of (frame.changes ?? []).filter(replayChangeIsNarratable)) {
    const currentName = change.cardName || frame.objects?.find(object => object.instanceId === change.instanceId)?.cardName;
    const name = cardDisplayName({ cardId: change.cardId, cardName: currentName || (change.cardId <= 0 ? "Hidden card" : undefined) });
    const category = transfers?.get(change.instanceId);
    const actor = timelinePlayerLabel(change.playerSide);
    let text = category === "Mill" ? `${actor} milled ${name}.`
      : category === "Discard" ? `${actor} discarded ${name}.`
      : change.action === "enter_public" && boardZoneKind(change.toZoneType ?? "") === "hand"
        ? category === "Draw" ? `${actor} drew ${name}.` : `${name} entered ${actor === "You" ? "your" : `${actor}'s`} hand.`
        : describeReplayChange({ ...change, cardName: name });
    const object = frame.objects?.find(object => object.instanceId === change.instanceId);
    if (change.action === "attack" && object?.attackTargetId != null) {
      text += ` Target: ${replayRelationshipTargetForId(relationships, object.attackTargetId).label}.`;
    }
    details.push(text);
  }
  for (const event of relationships.targetEventsByFrameId.get(frame.id) ?? []) {
    details.push(`${event.sourceLabel}${event.sourceKind === "ability" ? " ability" : ""} targets ${event.targets.map(target => target.label).join(", ")}.`);
  }
  for (const event of relationships.damageEventsByFrameId.get(frame.id) ?? []) {
    const side = relationships.objectsById.get(event.sourceId)?.playerSide;
    details.push(`${side ? `${timelinePlayerLabel(side)}: ` : ""}${event.sourceLabel} deals ${event.amount} damage to ${event.target.label}.`);
  }
  for (const event of relationships.triggerEventsByFrameId.get(frame.id) ?? []) {
    details.push(`${event.sourceLabel} ability triggered by ${event.triggeringLabel}.`);
  }
  for (const event of replayFrameAttachmentEvents(frame, relationships)) details.push(`${event.attachmentLabel} attaches to ${event.hostLabel}.`);
  for (const event of replayFrameCrewEvents(frame, relationships)) details.push(`${event.crewLabels.join(", ")} crew ${event.vehicleLabel}.`);
  return details;
}
