import type { MatchReplayFrame } from "../types";
import { boardZoneKind, replayChangeIsNarratable, replayFrameAttachmentEvents, replayFrameCrewEvents, type ReplayRelationshipIndex } from "./index";

export type ReplayCategory = "all" | "spells" | "combat" | "removal";
const routine = new Set(["tap", "untap", "stop_attack", "stop_block", "summoning_sickness_change"]);
export function isReplayDecision(frame: MatchReplayFrame, previousFrame?: MatchReplayFrame, relationships?: ReplayRelationshipIndex): boolean {
  if ((frame.winningPlayerSide === "self" || frame.winningPlayerSide === "opponent") || (frame.changes ?? []).some(change => replayChangeIsNarratable(change) && !routine.has(change.action))) return true;
  if (previousFrame && (["selfLifeTotal", "opponentLifeTotal"] as const).some(key => typeof frame[key] === "number" && typeof previousFrame[key] === "number" && frame[key] !== previousFrame[key])) return true;
  return Boolean(relationships && (
    relationships.damageEventsByFrameId.get(frame.id)?.length ||
    relationships.targetEventsByFrameId.get(frame.id)?.length ||
    relationships.triggerEventsByFrameId.get(frame.id)?.length ||
    replayFrameAttachmentEvents(frame, relationships).length ||
    replayFrameCrewEvents(frame, relationships).length
  ));
}
export function replayNavigationIndex(index: number, direction: 1 | -1, length: number, decisions?: number[]): number {
  if (!decisions) return Math.max(0, Math.min(length - 1, index + direction));
  return (direction > 0 ? decisions.find(value => value > index) : [...decisions].reverse().find(value => value < index)) ?? (direction > 0 ? Math.max(0, length - 1) : 0);
}
export function replayFrameMatches(frame: MatchReplayFrame, relationships: ReplayRelationshipIndex, query: string, category: ReplayCategory): boolean {
  const changes = (frame.changes ?? []).filter(replayChangeIsNarratable);
  const targets = relationships.targetEventsByFrameId.get(frame.id) ?? [];
  const damage = relationships.damageEventsByFrameId.get(frame.id) ?? [];
  const names = [
    ...changes.map(change => change.cardName ?? frame.objects?.find(object => object.instanceId === change.instanceId)?.cardName ?? ""),
    ...targets.flatMap(event => [event.sourceLabel, ...event.targets.map(target => target.label)]),
    ...damage.flatMap(event => [event.sourceLabel, event.target.label]),
    ...replayFrameAttachmentEvents(frame, relationships).flatMap(event => [event.attachmentLabel, event.hostLabel]),
    ...replayFrameCrewEvents(frame, relationships).flatMap(event => [event.vehicleLabel, ...event.crewLabels]),
    ...(relationships.triggerEventsByFrameId.get(frame.id) ?? []).flatMap(event => [event.sourceLabel, event.triggeringLabel]),
  ];
  if (query.trim() && !names.some(name => name.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase()))) return false;
  if (category === "spells") return targets.some(event => event.sourceKind === "spell") || changes.some(change => change.action === "cast" || boardZoneKind(change.toZoneType ?? "") === "stack" || boardZoneKind(change.fromZoneType ?? "") === "stack");
  if (category === "combat") return damage.length > 0 || changes.some(change => ["attack", "block", "stop_attack", "stop_block"].includes(change.action));
  if (category === "removal") return changes.some(change => boardZoneKind(change.fromZoneType ?? "") === "battlefield" && ["graveyard", "exile", "hand", "library"].includes(boardZoneKind(change.toZoneType ?? "")));
  return true;
}
