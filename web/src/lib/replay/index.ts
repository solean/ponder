import type { CardPreview } from "../scryfall";
import type {
  MatchCardPlay,
  MatchReplayChange,
  MatchReplayFrame,
  MatchReplayFrameObject,
} from "../types";
import { arenaTurnToFullTurn } from "../turns";

export type PreviewCard = {
  cardId: number;
  cardName?: string;
};

export type ManaCostPart =
  | { kind: "symbol"; token: string }
  | { kind: "separator"; value: string };
export type BoardZoneKind =
  | "hand"
  | "battlefield"
  | "stack"
  | "graveyard"
  | "exile"
  | "revealed"
  | "other";
export type InspectableZoneKind = "graveyard" | "exile";
export type BattlefieldSectionKind =
  | "lands"
  | "creatures"
  | "artifacts_enchantments"
  | "planeswalkers"
  | "battles"
  | "other";
export type ReplayCounterSummary = {
  label: string;
  count: number;
};
export type ReplayConnectionKind =
  | "combat"
  | "spellTarget"
  | "abilityTarget"
  | "attackTarget"
  | "attachment"
  | "trigger"
  | "damage"
  | "crew";
export type ReplayBoardConnection = {
  kind: ReplayConnectionKind;
  sourceId: number;
  targetId: number;
  hiddenUnlessFocused?: boolean;
};
export type ReplayTarget = {
  targetId: number;
  connectionId: number;
  label: string;
  playerSide?: "self" | "opponent";
};
export type ReplayTargetLookup = Map<number, ReplayTarget[]>;
export type ReplayTargetEvent = {
  sourceId: number;
  sourceLabel: string;
  sourceKind: "spell" | "ability";
  targets: ReplayTarget[];
};
export type ReplayDamageEvent = {
  sourceId: number;
  sourceLabel: string;
  target: ReplayTarget;
  amount: number;
};
export type ReplayTriggerEvent = {
  sourceId: number;
  sourceLabel: string;
  triggeringId: number;
  triggeringLabel: string;
};
export type ReplayAttachmentEvent = {
  attachmentId: number;
  attachmentLabel: string;
  hostId: number;
  hostLabel: string;
};
export type ReplayCrewEvent = {
  vehicleId: number;
  vehicleLabel: string;
  crewIds: number[];
  crewLabels: string[];
};
/**
 * Per-frame GRE `ZoneTransfer` categories ("Draw", "Return", "Put", …) keyed by
 * the instance id that moved. The frame diff cannot tell a draw from a reveal on
 * its own — the library is an untracked zone, so an arriving card looks like a
 * bare `enter_public` — and this is the only signal that names the transfer.
 */
export type ReplayZoneTransferLookup = Map<number, Map<number, string>>;
export type ReplayRelationshipIndex = {
  objectsById: Map<number, MatchReplayFrameObject>;
  playerSideBySeatId: Map<number, "self" | "opponent">;
  abilitySourceIdByAbilityId: Map<number, number>;
  spellTargetsBySourceId: ReplayTargetLookup;
  targetEventsByFrameId: Map<number, ReplayTargetEvent[]>;
  damageEventsByFrameId: Map<number, ReplayDamageEvent[]>;
  triggerEventsByFrameId: Map<number, ReplayTriggerEvent[]>;
  zoneTransferCategoryByFrameId: ReplayZoneTransferLookup;
};
export const REPLAY_SELF_PLAYER_CONNECTION_ID = -1;
export const REPLAY_OPPONENT_PLAYER_CONNECTION_ID = -2;
export type ReplayAnnotationDetail = {
  key?: string;
  type?: string;
  valueInt32?: number[];
  valueString?: string[];
};
export type ReplayAnnotation = {
  affectorId?: number;
  affectedIds?: number[];
  type?: string[];
  details?: ReplayAnnotationDetail[];
};
export type ReplayAnnotationPayload = {
  annotations?: ReplayAnnotation[];
  persistentAnnotations?: ReplayAnnotation[];
};
export type ReplayGameSummary = {
  result: "win" | "loss" | "unknown";
  detail: string;
};
export type ReplayGameSummaryOptions = {
  isFinalGame?: boolean;
  matchResult?: "win" | "loss" | "unknown";
};
export type ReplayGameGroup = {
  gameNumber: number;
  frames: MatchReplayFrame[];
  summary: ReplayGameSummary | null;
};

export function sideboardChangeCardTotal(
  cards: ReadonlyArray<{ quantity: number }>,
): number {
  return cards.reduce(
    (total, card) => total + Math.max(0, Math.floor(card.quantity)),
    0,
  );
}
export const BOARD_ZONE_ORDER: BoardZoneKind[] = [
  "hand",
  "battlefield",
  "stack",
  "graveyard",
  "exile",
  "revealed",
  "other",
];
export const BATTLEFIELD_SECTION_ORDER: BattlefieldSectionKind[] = [
  "lands",
  "other",
  "battles",
  "planeswalkers",
  "artifacts_enchantments",
  "creatures",
];
export const SELF_BATTLEFIELD_SECTION_ORDER: BattlefieldSectionKind[] = [
  "creatures",
  "artifacts_enchantments",
  "planeswalkers",
  "battles",
  "other",
  "lands",
];

export function cardDisplayName(card: PreviewCard): string {
  return card.cardName?.trim() || `Card ${card.cardId}`;
}

export function timelinePlayerLabel(playerSide?: string): string {
  if (playerSide === "self") return "You";
  if (playerSide === "opponent") return "Opponent";
  return "Unknown";
}

export function timelineZoneLabel(zone: string): string {
  const trimmed = zone.trim();
  if (!trimmed) return "-";
  return trimmed
    .split("_")
    .filter(Boolean)
    .map((part) => part.slice(0, 1).toUpperCase() + part.slice(1))
    .join(" ");
}

export function timelinePhaseLabel(phase: string | undefined): string {
  const trimmed = phase?.trim() ?? "";
  if (!trimmed) return "-";
  return trimmed
    .split("_")
    .filter(Boolean)
    .map((part) => part.slice(0, 1).toUpperCase() + part.slice(1))
    .join(" ");
}

export function cardFallbackHref(card: PreviewCard): string {
  const name = cardDisplayName(card);
  return card.cardName?.trim()
    ? `https://scryfall.com/search?q=${encodeURIComponent(`!"${name}"`)}`
    : `https://scryfall.com/search?q=${encodeURIComponent(`arenaid:${card.cardId}`)}`;
}

export function parseManaCostParts(manaCost: string): ManaCostPart[] {
  const trimmed = manaCost.trim();
  if (!trimmed) {
    return [];
  }

  const parts: ManaCostPart[] = [];
  const tokenPattern = /\{([^}]+)\}/g;
  let lastIndex = 0;

  while (true) {
    const match = tokenPattern.exec(trimmed);
    if (!match) {
      break;
    }

    const between = trimmed.slice(lastIndex, match.index).trim();
    if (between) {
      parts.push({ kind: "separator", value: between });
    }

    const token = match[1]?.trim();
    if (token) {
      parts.push({ kind: "symbol", token });
    }

    lastIndex = tokenPattern.lastIndex;
  }

  const tail = trimmed.slice(lastIndex).trim();
  if (tail) {
    parts.push({ kind: "separator", value: tail });
  }

  return parts;
}

export function boardZoneKind(zone: string): BoardZoneKind {
  const normalized = zone.trim().toLowerCase();
  if (!normalized) return "other";
  if (normalized.includes("hand")) return "hand";
  if (normalized.includes("battlefield")) return "battlefield";
  if (normalized.includes("stack")) return "stack";
  if (normalized.includes("graveyard")) return "graveyard";
  if (normalized.includes("exile")) return "exile";
  if (normalized.includes("reveal")) return "revealed";
  return "other";
}

export function boardZoneLabel(kind: BoardZoneKind): string {
  if (kind === "hand") return "Hand";
  if (kind === "battlefield") return "Battlefield";
  if (kind === "stack") return "Stack";
  if (kind === "graveyard") return "Graveyard";
  if (kind === "exile") return "Exile";
  if (kind === "revealed") return "Revealed";
  return "Other";
}

export function isInspectableZoneKind(kind: BoardZoneKind): kind is InspectableZoneKind {
  return kind === "graveyard" || kind === "exile";
}

export function boardTurnLabel(turnNumber?: number): string {
  return turnNumber && turnNumber > 0
    ? `T${arenaTurnToFullTurn(turnNumber)}`
    : "Pre";
}

export function boardPlayMeta(play: MatchCardPlay): string {
  const parts = [boardTurnLabel(play.turnNumber)];
  const phase = timelinePhaseLabel(play.phase);
  if (phase !== "-") {
    parts.push(phase);
  }
  return parts.join(" • ");
}

export function replayTurnValue(turnNumber?: number): number {
  return typeof turnNumber === "number" && turnNumber > 0 ? turnNumber : -1;
}

export function replayTurnLabel(turnNumber?: number): string {
  return typeof turnNumber === "number" && turnNumber > 0
    ? `Turn ${arenaTurnToFullTurn(turnNumber)}`
    : "Pre-game";
}

export function replayMomentLabel(play: MatchCardPlay): string {
  const phase = timelinePhaseLabel(play.phase);
  if (phase === "-") {
    return replayTurnLabel(play.turnNumber);
  }
  return `${replayTurnLabel(play.turnNumber)} - ${phase}`;
}

export function replayFrameMomentLabel(frame: MatchReplayFrame): string {
  const phase = timelinePhaseLabel(frame.phase);
  if (phase === "-") {
    return replayTurnLabel(frame.turnNumber);
  }
  return `${replayTurnLabel(frame.turnNumber)} - ${phase}`;
}

export type ReplayTurnBoundary = {
  turnKey: number;
  firstIndex: number;
  lastIndex: number;
};

export function buildReplayTurnBoundaries<T extends { turnNumber?: number }>(
  items: T[],
): ReplayTurnBoundary[] {
  const firstByTurn = new Map<number, number>();
  const lastByTurn = new Map<number, number>();

  for (let index = 0; index < items.length; index += 1) {
    const turnKey = replayTurnValue(items[index].turnNumber);
    if (!firstByTurn.has(turnKey)) {
      firstByTurn.set(turnKey, index);
    }
    lastByTurn.set(turnKey, index);
  }

  return Array.from(firstByTurn.entries())
    .map(([turnKey, firstIndex]) => ({
      turnKey,
      firstIndex,
      lastIndex: lastByTurn.get(turnKey) ?? firstIndex,
    }))
    .sort((a, b) => a.firstIndex - b.firstIndex);
}

export function replayTurnBoundaryCount(boundary: ReplayTurnBoundary): number {
  return boundary.lastIndex - boundary.firstIndex + 1;
}

export function replayObjectSortValue(object: MatchReplayFrameObject): number {
  return typeof object.zonePosition === "number"
    ? object.zonePosition
    : Number.MAX_SAFE_INTEGER;
}

export function sortReplayObjects(
  a: MatchReplayFrameObject,
  b: MatchReplayFrameObject,
): number {
  return (
    replayObjectSortValue(a) - replayObjectSortValue(b) ||
    a.instanceId - b.instanceId
  );
}

export function replayChangePriority(action: string): number {
  switch (action) {
    case "leave_public":
      return 100;
    case "move_public":
      return 95;
    case "enter_public":
      return 90;
    case "controller_change":
      return 85;
    case "attack":
    case "stop_attack":
    case "block":
    case "stop_block":
      return 80;
    case "tap":
    case "untap":
      return 70;
    case "counters_change":
      return 65;
    case "stat_change":
      return 60;
    case "summoning_sickness_change":
      return 50;
    default:
      return 10;
  }
}

export function replayObjectDetails(
  object: MatchReplayFrameObject,
): Record<string, unknown> | null {
  const raw = object.detailsJson?.trim();
  if (!raw) {
    return null;
  }

  try {
    const parsed = JSON.parse(raw) as unknown;
    if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
      return parsed as Record<string, unknown>;
    }
  } catch {
    return null;
  }

  return null;
}

export function replayObjectCardTypes(object: MatchReplayFrameObject): string[] {
  const details = replayObjectDetails(object);
  const raw = details?.["cardTypes"];
  if (!Array.isArray(raw)) {
    return [];
  }
  return raw.filter((value): value is string => typeof value === "string");
}

export function replayObjectHasType(
  object: MatchReplayFrameObject,
  preview: CardPreview | null,
  type: string,
): boolean {
  const typeLine = preview?.typeLine?.toLowerCase() ?? "";
  if (typeLine.includes(type)) {
    return true;
  }

  const normalized = type.toLowerCase();
  return replayObjectCardTypes(object).some((value) =>
    value.toLowerCase().includes(normalized),
  );
}

export function replayObjectIsAttacking(object: MatchReplayFrameObject): boolean {
  return Boolean(object.attackState?.trim());
}

export function replayObjectIsBlocking(object: MatchReplayFrameObject): boolean {
  // Attackers also carry a blockState ("blocked"/"unblocked" once blockers
  // are declared); only "declared"/"blocking" mark an actual blocker.
  const state = object.blockState?.trim().toLowerCase() ?? "";
  return state === "blocking" || state === "declared";
}

export function replayObjectPTLabel(
  object: MatchReplayFrameObject,
  preview: CardPreview | null,
): string | null {
  if (
    typeof object.power !== "number" ||
    typeof object.toughness !== "number" ||
    !replayObjectHasType(object, preview, "creature")
  ) {
    return null;
  }
  return `${object.power}/${object.toughness}`;
}

export function replayObjectCounterSummaries(
  object: MatchReplayFrameObject,
): ReplayCounterSummary[] {
  const raw = object.counterSummaryJson?.trim();
  if (!raw) {
    return [];
  }

  try {
    const parsed = JSON.parse(raw) as unknown;
    if (!Array.isArray(parsed)) {
      return [];
    }
    return parsed
      .map((entry) => {
        if (!entry || typeof entry !== "object") {
          return null;
        }
        const label =
          typeof (entry as { label?: unknown }).label === "string"
            ? (entry as { label: string }).label.trim()
            : "";
        const count =
          typeof (entry as { count?: unknown }).count === "number"
            ? (entry as { count: number }).count
            : Number.NaN;
        if (!label || !Number.isFinite(count) || count === 0) {
          return null;
        }
        return { label, count };
      })
      .filter((entry): entry is ReplayCounterSummary => entry !== null);
  } catch {
    return [];
  }
}

export function replayObjectLoyalty(
  object: MatchReplayFrameObject,
): number | null {
  const details = replayObjectDetails(object);
  const rawLoyalty = details?.["loyalty"];
  const detailValue =
    typeof rawLoyalty === "number"
      ? rawLoyalty
      : rawLoyalty &&
          typeof rawLoyalty === "object" &&
          !Array.isArray(rawLoyalty)
        ? (rawLoyalty as { value?: unknown }).value
        : null;
  if (typeof detailValue === "number" && Number.isFinite(detailValue)) {
    return detailValue;
  }

  const loyalty = replayObjectCounterSummaries(object).find(
    (counter) => counter.label.trim().toLowerCase() === "loyalty",
  );
  return loyalty?.count ?? null;
}

export function replayObjectBlockCount(object: MatchReplayFrameObject): number {
  return replayObjectBlockAttackerIDs(object).length;
}

export function replayObjectBlockAttackerIDs(object: MatchReplayFrameObject): number[] {
  const raw = object.blockAttackerIdsJson?.trim();
  if (!raw) {
    return [];
  }

  try {
    const parsed = JSON.parse(raw) as unknown;
    return Array.isArray(parsed)
      ? parsed.filter((value): value is number => typeof value === "number")
      : [];
  } catch {
    return [];
  }
}

export function replayFrameAnnotations(frame: MatchReplayFrame | null): ReplayAnnotation[] {
  const raw = frame?.annotationsJson?.trim();
  if (!raw) {
    return [];
  }

  try {
    const parsed = JSON.parse(raw) as unknown;
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
      return [];
    }

    const payload = parsed as ReplayAnnotationPayload;
    const next: ReplayAnnotation[] = [];
    if (Array.isArray(payload.annotations)) {
      next.push(...payload.annotations);
    }
    if (Array.isArray(payload.persistentAnnotations)) {
      next.push(...payload.persistentAnnotations);
    }
    return next.filter(
      (annotation): annotation is ReplayAnnotation =>
        Boolean(annotation) && typeof annotation === "object",
    );
  } catch {
    return [];
  }
}

export function replayFrameHasTargetSpec(frame: MatchReplayFrame): boolean {
  return replayFrameAnnotations(frame).some((annotation) =>
    replayAnnotationHasType(annotation, "AnnotationType_TargetSpec"),
  );
}

export function replayFrameHasRelationshipEvent(frame: MatchReplayFrame): boolean {
  return replayFrameAnnotations(frame).some((annotation) =>
    [
      "AnnotationType_TargetSpec",
      "AnnotationType_DamageDealt",
      "AnnotationType_TriggeringObject",
      "AnnotationType_AttachmentCreated",
      "AnnotationType_CrewedThisTurn",
    ].some((type) => replayAnnotationHasType(annotation, type)),
  );
}

export function replayAnnotationHasType(
  annotation: ReplayAnnotation,
  expectedType: string,
): boolean {
  return Array.isArray(annotation.type) && annotation.type.includes(expectedType);
}

export function replayAnnotationDetailIntValue(
  annotation: ReplayAnnotation,
  key: string,
): number | undefined {
  if (!Array.isArray(annotation.details)) {
    return undefined;
  }

  for (const detail of annotation.details) {
    if (detail?.key !== key || !Array.isArray(detail.valueInt32)) {
      continue;
    }
    const value = detail.valueInt32[0];
    if (typeof value === "number") {
      return value;
    }
  }

  return undefined;
}

function replayAnnotationDetailStringValue(
  annotation: ReplayAnnotation,
  key: string,
): string | undefined {
  if (!Array.isArray(annotation.details)) {
    return undefined;
  }

  for (const detail of annotation.details) {
    if (detail?.key !== key || !Array.isArray(detail.valueString)) {
      continue;
    }
    const value = detail.valueString[0];
    if (typeof value === "string" && value !== "") {
      return value;
    }
  }

  return undefined;
}

export function replayTargetListLabel(targets: ReplayTarget[]): string {
  const labels = targets.map((target) => target.label);
  if (labels.length === 0) return "";
  if (labels.length === 1) return labels[0]!;
  if (labels.length === 2) return `${labels[0]} and ${labels[1]}`;
  return `${labels.slice(0, -1).join(", ")}, and ${labels[labels.length - 1]}`;
}

export function replayPlayerConnectionId(
  side: "self" | "opponent",
): number {
  return side === "self"
    ? REPLAY_SELF_PLAYER_CONNECTION_ID
    : REPLAY_OPPONENT_PLAYER_CONNECTION_ID;
}

export function replayRelationshipTargetForId(
  relationships: ReplayRelationshipIndex,
  targetId: number,
): ReplayTarget {
  return replayRelationshipTarget(
    targetId,
    relationships.objectsById,
    relationships.playerSideBySeatId,
  );
}

export function replayFrameAttachmentEvents(
  frame: MatchReplayFrame | null,
  relationships: ReplayRelationshipIndex,
): ReplayAttachmentEvent[] {
  const events: ReplayAttachmentEvent[] = [];
  const seen = new Set<string>();
  for (const annotation of replayFrameAnnotations(frame)) {
    if (
      !replayAnnotationHasType(annotation, "AnnotationType_Attachment") &&
      !replayAnnotationHasType(annotation, "AnnotationType_AttachmentCreated")
    ) {
      continue;
    }
    if (typeof annotation.affectorId !== "number") continue;
    const attachment = relationships.objectsById.get(annotation.affectorId);
    if (!attachment) continue;
    for (const hostId of annotation.affectedIds ?? []) {
      if (typeof hostId !== "number") continue;
      const host = relationships.objectsById.get(hostId);
      if (!host) continue;
      const key = `${annotation.affectorId}-${hostId}`;
      if (seen.has(key)) continue;
      seen.add(key);
      events.push({
        attachmentId: annotation.affectorId,
        attachmentLabel: cardDisplayName(attachment),
        hostId,
        hostLabel: cardDisplayName(host),
      });
    }
  }
  return events;
}

export function buildReplayAttachmentState(
  frames: MatchReplayFrame[],
  throughIndex: number,
  relationships: ReplayRelationshipIndex,
): ReplayAttachmentEvent[] {
  const byAttachmentId = new Map<number, ReplayAttachmentEvent>();
  const lastIndex = Math.min(throughIndex, frames.length - 1);
  for (let index = 0; index <= lastIndex; index += 1) {
    for (const event of replayFrameAttachmentEvents(
      frames[index] ?? null,
      relationships,
    )) {
      byAttachmentId.set(event.attachmentId, event);
    }
  }
  return [...byAttachmentId.values()];
}

export function replayFrameCrewEvents(
  frame: MatchReplayFrame | null,
  relationships: ReplayRelationshipIndex,
): ReplayCrewEvent[] {
  const events: ReplayCrewEvent[] = [];
  for (const annotation of replayFrameAnnotations(frame)) {
    if (
      !replayAnnotationHasType(annotation, "AnnotationType_CrewedThisTurn") ||
      typeof annotation.affectorId !== "number"
    ) {
      continue;
    }
    const vehicle = relationships.objectsById.get(annotation.affectorId);
    if (!vehicle) continue;
    const crew = (annotation.affectedIds ?? [])
      .filter((id): id is number => typeof id === "number")
      .map((id) => ({ id, object: relationships.objectsById.get(id) }))
      .filter(
        (entry): entry is { id: number; object: MatchReplayFrameObject } =>
          Boolean(entry.object),
      );
    if (crew.length === 0) continue;
    events.push({
      vehicleId: annotation.affectorId,
      vehicleLabel: cardDisplayName(vehicle),
      crewIds: crew.map((entry) => entry.id),
      crewLabels: crew.map((entry) => cardDisplayName(entry.object)),
    });
  }
  return events;
}

function replayMapPush<K, V>(map: Map<K, V[]>, key: K, value: V): void {
  const values = map.get(key);
  if (values) {
    values.push(value);
  } else {
    map.set(key, [value]);
  }
}

function replayRelationshipTarget(
  targetId: number,
  objectsById: Map<number, MatchReplayFrameObject>,
  playerSideBySeatId: Map<number, "self" | "opponent">,
): ReplayTarget {
  const object = objectsById.get(targetId);
  if (object) {
    return {
      targetId,
      connectionId: targetId,
      label: cardDisplayName(object),
    };
  }

  const playerSide = playerSideBySeatId.get(targetId);
  if (playerSide) {
    return {
      targetId,
      connectionId: replayPlayerConnectionId(playerSide),
      label: playerSide === "self" ? "you" : "opponent",
      playerSide,
    };
  }

  return {
    targetId,
    connectionId: targetId,
    label: `target ${targetId}`,
  };
}

export function buildReplayRelationshipIndex(
  frames: MatchReplayFrame[],
): ReplayRelationshipIndex {
  const objectsById = new Map<number, MatchReplayFrameObject>();
  const playerSideBySeatId = new Map<number, "self" | "opponent">();
  const abilitySourceIdByAbilityId = new Map<number, number>();

  for (const frame of frames) {
    for (const object of frame.objects ?? []) {
      objectsById.set(object.instanceId, object);
      if (object.playerSide !== "self" && object.playerSide !== "opponent") {
        continue;
      }
      if (typeof object.ownerSeatId === "number") {
        playerSideBySeatId.set(object.ownerSeatId, object.playerSide);
      }
      if (typeof object.controllerSeatId === "number") {
        playerSideBySeatId.set(object.controllerSeatId, object.playerSide);
      }
    }

    for (const annotation of replayFrameAnnotations(frame)) {
      if (
        !replayAnnotationHasType(
          annotation,
          "AnnotationType_AbilityInstanceCreated",
        ) ||
        typeof annotation.affectorId !== "number"
      ) {
        continue;
      }
      for (const abilityId of annotation.affectedIds ?? []) {
        if (typeof abilityId === "number") {
          abilitySourceIdByAbilityId.set(abilityId, annotation.affectorId);
        }
      }
    }
  }

  const spellTargetsBySourceId: ReplayTargetLookup = new Map();
  const targetEventsByFrameId = new Map<number, ReplayTargetEvent[]>();
  const damageEventsByFrameId = new Map<number, ReplayDamageEvent[]>();
  const triggerEventsByFrameId = new Map<number, ReplayTriggerEvent[]>();
  const zoneTransferCategoryByFrameId: ReplayZoneTransferLookup = new Map();

  for (const frame of frames) {
    const annotations = replayFrameAnnotations(frame);
    for (const annotation of annotations) {
      // Recorded first: the branches below `continue` past the rest of the
      // annotation once they claim it.
      if (replayAnnotationHasType(annotation, "AnnotationType_ZoneTransfer")) {
        const category = replayAnnotationDetailStringValue(
          annotation,
          "category",
        );
        if (category) {
          let categoryByInstanceId = zoneTransferCategoryByFrameId.get(frame.id);
          if (!categoryByInstanceId) {
            categoryByInstanceId = new Map<number, string>();
            zoneTransferCategoryByFrameId.set(frame.id, categoryByInstanceId);
          }
          for (const movedId of annotation.affectedIds ?? []) {
            if (typeof movedId === "number") {
              categoryByInstanceId.set(movedId, category);
            }
          }
        }
      }

      if (
        replayAnnotationHasType(annotation, "AnnotationType_TargetSpec") &&
        typeof annotation.affectorId === "number"
      ) {
        const rawSourceId = annotation.affectorId;
        const directSource = objectsById.get(rawSourceId);
        const abilitySourceId = abilitySourceIdByAbilityId.get(rawSourceId);
        const displaySourceId = directSource ? rawSourceId : abilitySourceId;
        const displaySource =
          typeof displaySourceId === "number"
            ? objectsById.get(displaySourceId)
            : undefined;
        if (!displaySource || typeof displaySourceId !== "number") {
          continue;
        }

        const targets = (annotation.affectedIds ?? [])
          .filter((targetId): targetId is number => typeof targetId === "number")
          .map((targetId) =>
            replayRelationshipTarget(
              targetId,
              objectsById,
              playerSideBySeatId,
            ),
          );
        if (targets.length === 0) {
          continue;
        }

        const sourceKind = directSource ? "spell" : "ability";
        const event: ReplayTargetEvent = {
          sourceId: displaySourceId,
          sourceLabel: cardDisplayName(displaySource),
          sourceKind,
          targets,
        };
        replayMapPush(targetEventsByFrameId, frame.id, event);

        if (sourceKind === "spell") {
          const existing = spellTargetsBySourceId.get(displaySourceId) ?? [];
          for (const target of targets) {
            if (!existing.some((candidate) => candidate.targetId === target.targetId)) {
              existing.push(target);
            }
          }
          spellTargetsBySourceId.set(displaySourceId, existing);
        }
      }

      if (
        replayAnnotationHasType(annotation, "AnnotationType_DamageDealt") &&
        typeof annotation.affectorId === "number"
      ) {
        const source = objectsById.get(annotation.affectorId);
        const amount = replayAnnotationDetailIntValue(annotation, "damage");
        if (!source || typeof amount !== "number" || amount <= 0) {
          continue;
        }
        for (const targetId of annotation.affectedIds ?? []) {
          if (typeof targetId !== "number") {
            continue;
          }
          replayMapPush(damageEventsByFrameId, frame.id, {
            sourceId: annotation.affectorId,
            sourceLabel: cardDisplayName(source),
            target: replayRelationshipTarget(
              targetId,
              objectsById,
              playerSideBySeatId,
            ),
            amount,
          });
        }
      }

      if (
        replayAnnotationHasType(annotation, "AnnotationType_TriggeringObject") &&
        typeof annotation.affectorId === "number"
      ) {
        const sourceId = abilitySourceIdByAbilityId.get(annotation.affectorId);
        const source =
          typeof sourceId === "number" ? objectsById.get(sourceId) : undefined;
        if (!source || typeof sourceId !== "number") {
          continue;
        }
        for (const triggeringId of annotation.affectedIds ?? []) {
          const triggeringObject =
            typeof triggeringId === "number"
              ? objectsById.get(triggeringId)
              : undefined;
          if (
            !triggeringObject ||
            typeof triggeringId !== "number" ||
            triggeringId === sourceId
          ) {
            continue;
          }
          replayMapPush(triggerEventsByFrameId, frame.id, {
            sourceId,
            sourceLabel: cardDisplayName(source),
            triggeringId,
            triggeringLabel: cardDisplayName(triggeringObject),
          });
        }
      }
    }
  }

  return {
    objectsById,
    playerSideBySeatId,
    abilitySourceIdByAbilityId,
    spellTargetsBySourceId,
    targetEventsByFrameId,
    damageEventsByFrameId,
    triggerEventsByFrameId,
    zoneTransferCategoryByFrameId,
  };
}

export function buildReplayTargetLookup(
  frames: MatchReplayFrame[],
): ReplayTargetLookup {
  return buildReplayRelationshipIndex(frames).spellTargetsBySourceId;
}

export function replayObjectStatePills(
  object: MatchReplayFrameObject,
): ReplayCounterSummary[] {
  const pills: ReplayCounterSummary[] = [];

  if (object.isTapped) {
    pills.push({ label: "Tapped", count: 1 });
  }
  if (replayObjectIsAttacking(object)) {
    pills.push({ label: "Attacking", count: 1 });
  }
  if (replayObjectIsBlocking(object)) {
    const blockCount = replayObjectBlockCount(object);
    pills.push({
      label: blockCount > 1 ? `Blocking ${blockCount}` : "Blocking",
      count: 1,
    });
  }
  if (object.hasSummoningSickness) {
    pills.push({ label: "Summoning Sick", count: 1 });
  }
  if (
    typeof object.ownerSeatId === "number" &&
    typeof object.controllerSeatId === "number" &&
    object.ownerSeatId !== object.controllerSeatId
  ) {
    pills.push({ label: "Stolen", count: 1 });
  }

  return pills;
}

export function replayObjectStatusText(
  object: MatchReplayFrameObject,
  preview: CardPreview | null,
): string {
  const parts = [
    timelinePlayerLabel(object.playerSide),
    timelineZoneLabel(object.zoneType),
  ];
  const ptLabel = replayObjectPTLabel(object, preview);
  if (ptLabel) {
    parts.push(ptLabel);
  }
  if (object.isTapped) {
    parts.push("Tapped");
  }
  if (replayObjectIsAttacking(object)) {
    parts.push("Attacking");
  }
  if (replayObjectIsBlocking(object)) {
    parts.push("Blocking");
  }
  if (object.hasSummoningSickness) {
    parts.push("Summoning sick");
  }
  const loyalty = replayObjectLoyalty(object);
  if (loyalty != null) {
    parts.push(`${loyalty} loyalty`);
  }
  for (const counter of replayObjectCounterSummaries(object)) {
    if (loyalty != null && counter.label.trim().toLowerCase() === "loyalty") {
      continue;
    }
    parts.push(`${counter.label} x${counter.count}`);
  }
  return parts.join(" • ");
}

export function replayObjectName(
  object: MatchReplayFrameObject,
  preview: CardPreview | null,
): string {
  return (preview?.name ?? cardDisplayName(object)).trim();
}

export function replayObjectIsBasicLand(
  object: MatchReplayFrameObject,
  preview: CardPreview | null,
): boolean {
  const typeLine = preview?.typeLine?.toLowerCase() ?? "";
  if (typeLine.includes("basic")) {
    return true;
  }

  return replayObjectHasType(object, preview, "basic");
}

export function replayObjectBasicLandRank(
  object: MatchReplayFrameObject,
  preview: CardPreview | null,
): number {
  const name = replayObjectName(object, preview).toLowerCase();

  if (name === "plains") return 0;
  if (name === "island") return 1;
  if (name === "swamp") return 2;
  if (name === "mountain") return 3;
  if (name === "forest") return 4;
  if (name === "wastes") return 5;
  return 6;
}

export function sortBattlefieldSectionObjects(
  kind: BattlefieldSectionKind,
  objects: MatchReplayFrameObject[],
  previewByCardID: Map<number, CardPreview | null>,
): MatchReplayFrameObject[] {
  if (kind !== "lands") {
    return objects;
  }

  return [...objects].sort((a, b) => {
    const aPreview = previewByCardID.get(a.cardId) ?? null;
    const bPreview = previewByCardID.get(b.cardId) ?? null;
    const aBasic = replayObjectIsBasicLand(a, aPreview);
    const bBasic = replayObjectIsBasicLand(b, bPreview);

    if (aBasic !== bBasic) {
      return aBasic ? -1 : 1;
    }

    if (aBasic && bBasic) {
      const basicRankDelta =
        replayObjectBasicLandRank(a, aPreview) -
        replayObjectBasicLandRank(b, bPreview);
      if (basicRankDelta !== 0) {
        return basicRankDelta;
      }
    }

    const nameDelta = replayObjectName(a, aPreview).localeCompare(
      replayObjectName(b, bPreview),
    );
    if (nameDelta !== 0) {
      return nameDelta;
    }

    return sortReplayObjects(a, b);
  });
}

export type ReplayCardStack = {
  key: string;
  objects: MatchReplayFrameObject[];
};

export function replayObjectCanStack(
  object: MatchReplayFrameObject,
  preview: CardPreview | null,
): boolean {
  // Cards carrying extra state (combat, counters, stolen, summoning sick,
  // animated with stats) stay standalone so that state remains visible.
  if (replayObjectIsAttacking(object) || replayObjectIsBlocking(object)) {
    return false;
  }
  if (object.hasSummoningSickness) {
    return false;
  }
  if (replayObjectCounterSummaries(object).length > 0) {
    return false;
  }
  if (
    typeof object.ownerSeatId === "number" &&
    typeof object.controllerSeatId === "number" &&
    object.ownerSeatId !== object.controllerSeatId
  ) {
    return false;
  }
  if (replayObjectPTLabel(object, preview)) {
    return false;
  }
  return true;
}

/**
 * Collapse duplicate cards (same name, same tapped state) into stacks so a
 * board with six Islands or three Mutagen tokens reads as one pile instead of
 * six full cards. Order follows the first occurrence of each stack in the
 * input; cards that fail `replayObjectCanStack` (or the optional extra
 * predicate) come through as single-card stacks.
 */
export function groupBattlefieldCardStacks(
  objects: MatchReplayFrameObject[],
  previewByCardID: Map<number, CardPreview | null>,
  canStack?: (object: MatchReplayFrameObject) => boolean,
): ReplayCardStack[] {
  const stacks: ReplayCardStack[] = [];
  const stacksByKey = new Map<string, ReplayCardStack>();

  for (const object of objects) {
    const preview = previewByCardID.get(object.cardId) ?? null;
    const stackable =
      replayObjectCanStack(object, preview) && (canStack?.(object) ?? true);
    if (!stackable) {
      stacks.push({ key: `single-${object.instanceId}`, objects: [object] });
      continue;
    }

    const name = replayObjectName(object, preview).toLowerCase();
    const key = `${name || `card-${object.cardId}`}|${object.isTapped ? "tapped" : "untapped"}`;
    const existing = stacksByKey.get(key);
    if (existing) {
      existing.objects.push(object);
    } else {
      const stack: ReplayCardStack = { key, objects: [object] };
      stacksByKey.set(key, stack);
      stacks.push(stack);
    }
  }

  return stacks;
}

export function replayFrameLifeTotalForSide(
  frame: MatchReplayFrame | null | undefined,
  side: "self" | "opponent",
): number | undefined {
  if (!frame) {
    return undefined;
  }
  return side === "self" ? frame.selfLifeTotal : frame.opponentLifeTotal;
}

export function replayFrameLifeTotalsSummary(
  frame: MatchReplayFrame | null | undefined,
): string | null {
  const selfLifeTotal = replayFrameLifeTotalForSide(frame, "self");
  const opponentLifeTotal = replayFrameLifeTotalForSide(frame, "opponent");
  if (
    typeof selfLifeTotal !== "number" &&
    typeof opponentLifeTotal !== "number"
  ) {
    return null;
  }

  const parts: string[] = [];
  if (typeof selfLifeTotal === "number") {
    parts.push(`You ${selfLifeTotal}`);
  }
  if (typeof opponentLifeTotal === "number") {
    parts.push(`Opponent ${opponentLifeTotal}`);
  }
  return parts.join(" • ");
}

export function replayFrameHasLifeDelta(
  previousFrame: MatchReplayFrame | null,
  frame: MatchReplayFrame,
): boolean {
  if (!previousFrame) {
    return false;
  }
  return (
    replayFrameLifeTotalForSide(previousFrame, "self") !==
      replayFrameLifeTotalForSide(frame, "self") ||
    replayFrameLifeTotalForSide(previousFrame, "opponent") !==
      replayFrameLifeTotalForSide(frame, "opponent")
  );
}

/**
 * A "noise move" is a public move whose source and destination resolve to the
 * same board zone — GRE bookkeeping like "Hand to Hand" or "Limbo to Limbo" that
 * never reads as a real play. These are coalesced out of the timeline.
 */
export function replayChangeIsNoiseMove(change: MatchReplayChange): boolean {
  if (change.action !== "move_public") {
    return false;
  }
  return (
    boardZoneKind(change.fromZoneType ?? "") ===
    boardZoneKind(change.toZoneType ?? "")
  );
}

function replayChangeIsGenuineVisibilityLoss(
  change: MatchReplayChange,
): boolean {
  if (change.action !== "leave_public") {
    return false;
  }
  const from = boardZoneKind(change.fromZoneType ?? "");
  return (
    from === "revealed" ||
    (from === "hand" && change.playerSide === "opponent")
  );
}

function replayChangeIsNarratable(change: MatchReplayChange): boolean {
  if (replayChangeIsNoiseMove(change)) {
    return false;
  }
  if (change.action !== "leave_public") {
    return true;
  }

  const from = boardZoneKind(change.fromZoneType ?? "");
  return (
    replayChangeIsGenuineVisibilityLoss(change) ||
    from === "battlefield" ||
    from === "stack"
  );
}

function replayChangeIsCast(change: MatchReplayChange): boolean {
  if (change.action !== "enter_public" && change.action !== "move_public") {
    return false;
  }
  return (
    boardZoneKind(change.toZoneType ?? "") === "stack" &&
    boardZoneKind(change.fromZoneType ?? "") !== "stack"
  );
}

function replayFrameHasNarratableChange(frame: MatchReplayFrame): boolean {
  return (frame.changes ?? []).some(replayChangeIsNarratable);
}

export function isMeaningfulReplayFrame(
  frame: MatchReplayFrame,
  previousFrame: MatchReplayFrame | null,
): boolean {
  return (
    replayFrameHasNarratableChange(frame) ||
    replayFrameHasRelationshipEvent(frame) ||
    replayFrameHasLifeDelta(previousFrame, frame)
  );
}

export function filterMeaningfulReplayFrames(
  frames: MatchReplayFrame[],
): MatchReplayFrame[] {
  if (frames.length <= 1) {
    return frames;
  }

  const meaningfulFrames: MatchReplayFrame[] = [];
  for (let index = 0; index < frames.length; index += 1) {
    const frame = frames[index];
    const previousFrame = index > 0 ? frames[index - 1] ?? null : null;
    if (isMeaningfulReplayFrame(frame, previousFrame)) {
      meaningfulFrames.push(frame);
    }
  }

  if (meaningfulFrames.length > 0) {
    return meaningfulFrames;
  }

  const lastFrame = frames[frames.length - 1];
  return lastFrame ? [lastFrame] : [];
}

/**
 * Arena can begin a later game with a setup snapshot that still carries the
 * previous game's final turn number. Keep ordinary pre-game snapshots, but
 * discard that inherited-turn prefix once the new game reaches turn 1.
 */
export function trimInheritedReplayTurnPrefix(
  frames: MatchReplayFrame[],
): MatchReplayFrame[] {
  const turnOnePlayIndex = frames.findIndex(
    (frame) =>
      frame.turnNumber === 1 &&
      (frame.gameStage ?? "").trim().toLowerCase() === "play",
  );
  if (turnOnePlayIndex <= 0) {
    return frames;
  }

  const hasInheritedTurn = frames
    .slice(0, turnOnePlayIndex)
    .some((frame) => replayTurnValue(frame.turnNumber) > 1);
  return hasInheritedTurn ? frames.slice(turnOnePlayIndex) : frames;
}

export function summarizeReplayFrameZones(
  objects: MatchReplayFrameObject[],
): Map<BoardZoneKind, number> {
  const counts = new Map<BoardZoneKind, number>();
  for (const kind of BOARD_ZONE_ORDER) {
    counts.set(kind, 0);
  }

  for (const object of objects) {
    const kind = boardZoneKind(object.zoneType);
    counts.set(kind, (counts.get(kind) ?? 0) + 1);
  }

  return counts;
}

export function replayFramePrimaryChange(
  frame: MatchReplayFrame | null,
): MatchReplayChange | null {
  const changes = frame?.changes ?? [];
  if (changes.length === 0) {
    return null;
  }
  return [...changes].sort(
    (a, b) =>
      replayChangePriority(b.action) - replayChangePriority(a.action),
  )[0] ?? null;
}

export function replayFramePrimarySummary(
  frame: MatchReplayFrame | null,
  previousFrame: MatchReplayFrame | null,
): string {
  const primaryChange = replayFramePrimaryChange(frame);
  if (primaryChange) {
    return describeReplayChange(primaryChange);
  }
  if (frame && replayFrameHasLifeDelta(previousFrame, frame)) {
    const summary = replayFrameLifeTotalsSummary(frame);
    return summary ? `Life totals changed. ${summary}.` : "Life totals changed.";
  }
  return "Initial replay snapshot for this game state.";
}

export function replayFrameWinningPlayerSide(
  frame: MatchReplayFrame | null | undefined,
): "self" | "opponent" | "unknown" {
  const side = frame?.winningPlayerSide;
  return side === "self" || side === "opponent" ? side : "unknown";
}

export function normalizeReplayWinReason(reason?: string | null): string {
  return (reason ?? "")
    .trim()
    .replace(/^ResultReason_/, "")
    .replace(/^WinningReason_/, "");
}

export function formatReplayWinReason(reason: string): string {
  const normalized = normalizeReplayWinReason(reason);
  if (!normalized) {
    return "";
  }
  return normalized
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .replace(/_/g, " ")
    .toLowerCase();
}

export function replayFrameLifeTotalWinner(
  frame: MatchReplayFrame,
): "self" | "opponent" | "unknown" {
  const selfLifeTotal = frame.selfLifeTotal;
  const opponentLifeTotal = frame.opponentLifeTotal;
  const selfIsDead =
    typeof selfLifeTotal === "number" && Number.isFinite(selfLifeTotal) && selfLifeTotal <= 0;
  const opponentIsDead =
    typeof opponentLifeTotal === "number" &&
    Number.isFinite(opponentLifeTotal) &&
    opponentLifeTotal <= 0;

  if (selfIsDead === opponentIsDead) {
    return "unknown";
  }
  return selfIsDead ? "opponent" : "self";
}

export function terminalReplayFrameConfidence(frame: MatchReplayFrame): number {
  const explicitWinner = replayFrameWinningPlayerSide(frame);
  if (explicitWinner !== "unknown") {
    return 4;
  }
  if (replayFrameLifeTotalWinner(frame) !== "unknown") {
    return 3;
  }
  if (normalizeReplayWinReason(frame.winReason) !== "") {
    return 2;
  }
  if ((frame.gameStage ?? "").trim().toLowerCase() === "gameover") {
    return 1;
  }
  return 0;
}

export function summarizeReplayGame(
  frames: MatchReplayFrame[],
  options: ReplayGameSummaryOptions = {},
): ReplayGameSummary | null {
  if (frames.length === 0) {
    return null;
  }

  let terminalFrame: MatchReplayFrame | null = null;
  let bestConfidence = 0;
  for (let index = frames.length - 1; index >= 0; index -= 1) {
    const frame = frames[index];
    const confidence = terminalReplayFrameConfidence(frame);
    if (confidence === 0) {
      continue;
    }
    if (!terminalFrame || confidence > bestConfidence) {
      terminalFrame = frame;
      bestConfidence = confidence;
      if (confidence >= 4) {
        break;
      }
    }
  }
  terminalFrame ??= frames[frames.length - 1] ?? null;
  if (!terminalFrame) {
    return null;
  }

  const lifeTotalWinner = replayFrameLifeTotalWinner(terminalFrame);
  // Prefer terminal life totals when they clearly identify a winner. Arena can
  // occasionally report a concede reason on the final frame after lethal damage.
  const winningPlayerSide =
    lifeTotalWinner !== "unknown"
      ? lifeTotalWinner
      : replayFrameWinningPlayerSide(terminalFrame);

  let result: "win" | "loss" | "unknown" =
    winningPlayerSide === "self"
      ? "win"
      : winningPlayerSide === "opponent"
        ? "loss"
        : "unknown";
  const normalizedReason = normalizeReplayWinReason(terminalFrame.winReason);

  let detail = "";
  if (lifeTotalWinner === "opponent") {
    detail = "You went to 0 life.";
  } else if (lifeTotalWinner === "self") {
    detail = "Opponent went to 0 life.";
  } else if (normalizedReason === "Concede") {
    const concedingPlayerSide =
      winningPlayerSide === "self"
        ? "opponent"
        : winningPlayerSide === "opponent"
          ? "self"
          : "unknown";
    detail =
      concedingPlayerSide === "unknown"
        ? "A player conceded."
        : `${timelinePlayerLabel(concedingPlayerSide)} conceded.`;
  } else if (normalizedReason) {
    detail = `Ended by ${formatReplayWinReason(normalizedReason)}.`;
  } else if (result === "win") {
    detail = "You won this game.";
  } else if (result === "loss") {
    detail = "You lost this game.";
  } else {
    detail = "Game result recorded.";
  }

  if (
    options.isFinalGame &&
    (options.matchResult === "win" || options.matchResult === "loss") &&
    options.matchResult !== result
  ) {
    result = options.matchResult;
    if (normalizedReason === "Concede") {
      detail = result === "win" ? "Opponent conceded." : "You conceded.";
    } else if (result === "win") {
      detail =
        typeof terminalFrame.opponentLifeTotal === "number" &&
        terminalFrame.opponentLifeTotal <= 0
          ? "Opponent went to 0 life."
          : "You won this game.";
    } else {
      detail =
        typeof terminalFrame.selfLifeTotal === "number" &&
        terminalFrame.selfLifeTotal <= 0
          ? "You went to 0 life."
          : "You lost this game.";
    }
  }

  return { result, detail };
}

export function buildReplayGameGroups(
  replayFrames: MatchReplayFrame[],
  matchResult: ReplayGameSummaryOptions["matchResult"] = "unknown",
): ReplayGameGroup[] {
  const framesByGame = new Map<number, MatchReplayFrame[]>();
  for (const frame of replayFrames) {
    const gameNumber =
      frame.gameNumber && frame.gameNumber > 0 ? frame.gameNumber : 1;
    const frames = framesByGame.get(gameNumber);
    if (frames) {
      frames.push(frame);
    } else {
      framesByGame.set(gameNumber, [frame]);
    }
  }

  const games = Array.from(framesByGame.entries()).sort(
    ([leftGameNumber], [rightGameNumber]) =>
      leftGameNumber - rightGameNumber,
  );
  const finalGameNumber = games[games.length - 1]?.[0] ?? null;

  return games.map(([gameNumber, rawFrames]) => {
    const gameFrames = trimInheritedReplayTurnPrefix(rawFrames);
    const frames = filterMeaningfulReplayFrames(gameFrames);
    const visibleFrames = new Set(frames);
    const summaryFrames = rawFrames.filter(
      (frame) =>
        visibleFrames.has(frame) ||
        (frame.gameStage ?? "").trim().toLowerCase() === "gameover",
    );

    return {
      gameNumber,
      frames,
      summary: summarizeReplayGame(summaryFrames, {
        isFinalGame: gameNumber === finalGameNumber,
        matchResult,
      }),
    };
  });
}

export function preferredReplayFrameIndex(frames: MatchReplayFrame[]): number {
  if (frames.length === 0) {
    return 0;
  }

  for (let index = frames.length - 1; index >= 0; index -= 1) {
    if ((frames[index]?.objects?.length ?? 0) > 0) {
      return index;
    }
  }

  return frames.length - 1;
}

export function describeReplayChange(change: MatchReplayChange): string {
  const actor = timelinePlayerLabel(change.playerSide);
  const name = cardDisplayName({
    cardId: change.cardId,
    cardName: change.cardName,
  });

  if (change.action === "enter_public") {
    return `${actor} showed ${name} in ${timelineZoneLabel(change.toZoneType ?? "")}.`;
  }
  if (change.action === "move_public") {
    return `${actor} moved ${name} from ${timelineZoneLabel(change.fromZoneType ?? "")} to ${timelineZoneLabel(change.toZoneType ?? "")}.`;
  }
  if (change.action === "leave_public") {
    return `${actor} lost public visibility of ${name} from ${timelineZoneLabel(change.fromZoneType ?? "")}.`;
  }
  if (change.action === "controller_change") {
    return `${actor} took control of ${name}.`;
  }
  if (change.action === "tap") {
    return `${actor} tapped ${name}.`;
  }
  if (change.action === "untap") {
    return `${actor} untapped ${name}.`;
  }
  if (change.action === "attack") {
    return `${actor} attacked with ${name}.`;
  }
  if (change.action === "stop_attack") {
    return `${name} stopped attacking.`;
  }
  if (change.action === "block") {
    return `${actor} declared ${name} as a blocker.`;
  }
  if (change.action === "stop_block") {
    return `${name} stopped blocking.`;
  }
  if (change.action === "summoning_sickness_change") {
    return `${name}'s summoning-sickness state changed.`;
  }
  if (change.action === "stat_change") {
    return `${name}'s power and toughness changed.`;
  }
  if (change.action === "counters_change") {
    return `${name}'s counters changed.`;
  }
  return `${actor} changed ${name}.`;
}

export function previewIsPermanent(preview: CardPreview | null): boolean {
  const typeLine = preview?.typeLine?.toLowerCase() ?? "";
  if (!typeLine) {
    return false;
  }

  return (
    typeLine.includes("artifact") ||
    typeLine.includes("battle") ||
    typeLine.includes("creature") ||
    typeLine.includes("enchantment") ||
    typeLine.includes("land") ||
    typeLine.includes("planeswalker")
  );
}

export function shouldRenderOnBattlefield(
  play: MatchCardPlay,
  preview: CardPreview | null,
  activePlayID: number,
): boolean {
  const zone = boardZoneKind(play.firstPublicZone);
  if (zone === "battlefield") {
    return true;
  }

  // Reconstruct likely permanent resolution: if a permanent first appeared on
  // the stack and it is no longer the current action, show it on the board.
  if (
    zone === "stack" &&
    play.id !== activePlayID &&
    previewIsPermanent(preview)
  ) {
    return true;
  }

  return false;
}

export function battlefieldSectionKind(
  preview: CardPreview | null,
  object?: MatchReplayFrameObject,
): BattlefieldSectionKind {
  const typeLine = preview?.typeLine?.toLowerCase() ?? "";
  const fallbackTypes = object
    ? replayObjectCardTypes(object).map((value) => value.toLowerCase())
    : [];
  const hasType = (type: string) =>
    typeLine.includes(type) ||
    fallbackTypes.some((value) => value.includes(type));

  if (!typeLine && fallbackTypes.length === 0) {
    return "other";
  }
  if (hasType("land")) {
    return "lands";
  }
  if (hasType("creature")) {
    return "creatures";
  }
  if (hasType("planeswalker")) {
    return "planeswalkers";
  }
  if (hasType("battle")) {
    return "battles";
  }
  if (hasType("artifact") || hasType("enchantment")) {
    return "artifacts_enchantments";
  }
  return "other";
}

export function battlefieldSectionLabel(kind: BattlefieldSectionKind): string {
  switch (kind) {
    case "lands":
      return "Lands";
    case "creatures":
      return "Creatures";
    case "artifacts_enchantments":
      return "Artifacts + Enchantments";
    case "planeswalkers":
      return "Planeswalkers";
    case "battles":
      return "Battles";
    default:
      return "Other Permanents";
  }
}

export function battlefieldSectionOrder(
  side: "self" | "opponent",
): BattlefieldSectionKind[] {
  return side === "self"
    ? SELF_BATTLEFIELD_SECTION_ORDER
    : BATTLEFIELD_SECTION_ORDER;
}

export function summarizeReplayZones(
  plays: MatchCardPlay[],
): Map<BoardZoneKind, number> {
  const counts = new Map<BoardZoneKind, number>();
  for (const kind of BOARD_ZONE_ORDER) {
    counts.set(kind, 0);
  }

  for (const play of plays) {
    const kind = boardZoneKind(play.firstPublicZone);
    counts.set(kind, (counts.get(kind) ?? 0) + 1);
  }

  return counts;
}


/**
 * Signed life change for one side between two frames, or null when either total
 * is missing or unchanged. Drives the HUD delta flash.
 */
export function replayLifeDelta(
  previousFrame: MatchReplayFrame | null | undefined,
  frame: MatchReplayFrame | null | undefined,
  side: "self" | "opponent",
): number | null {
  const previous = replayFrameLifeTotalForSide(previousFrame, side);
  const current = replayFrameLifeTotalForSide(frame, side);
  if (typeof previous !== "number" || typeof current !== "number") {
    return null;
  }
  const delta = current - previous;
  return delta === 0 ? null : delta;
}

export type ReplayLifePoint = { self: number | null; opponent: number | null };
export type ReplayLifeDomain = { min: number; max: number };
export type ReplayTickKind = "combat" | "spell" | "life" | "other";

/**
 * Per-step life totals for both players, carrying the last known value forward
 * across frames that omit a total. Drives the scrubber's dual sparkline.
 */
export function buildReplayLifeSeries(
  frames: MatchReplayFrame[],
): ReplayLifePoint[] {
  let lastSelf: number | null = null;
  let lastOpponent: number | null = null;
  return frames.map((frame) => {
    if (typeof frame.selfLifeTotal === "number") {
      lastSelf = frame.selfLifeTotal;
    }
    if (typeof frame.opponentLifeTotal === "number") {
      lastOpponent = frame.opponentLifeTotal;
    }
    return { self: lastSelf, opponent: lastOpponent };
  });
}

/** Y-axis domain for a life series, always including the 0–20 baseline range. */
export function replayLifeSeriesDomain(
  series: ReplayLifePoint[],
): ReplayLifeDomain {
  let min = 0;
  let max = 20;
  for (const point of series) {
    if (typeof point.self === "number") {
      min = Math.min(min, point.self);
      max = Math.max(max, point.self);
    }
    if (typeof point.opponent === "number") {
      min = Math.min(min, point.opponent);
      max = Math.max(max, point.opponent);
    }
  }
  return { min, max };
}

export type ReplaySideCensus = {
  creatures: number;
  /** Combined power of the counted creatures. */
  power: number;
  lands: number;
  /** Null when the hand is a hidden zone (opponent with no revealed cards). */
  hand: number | null;
  graveyard: number;
};

export type ReplayBoardCensus = {
  self: ReplaySideCensus;
  opponent: ReplaySideCensus;
};

function emptySideCensus(): ReplaySideCensus {
  return { creatures: 0, power: 0, lands: 0, hand: 0, graveyard: 0 };
}

function objectTypeIncludes(object: MatchReplayFrameObject, type: string): boolean {
  return replayObjectCardTypes(object).some((value) =>
    value.toLowerCase().includes(type),
  );
}

/**
 * Head count of each player's public state in a frame: battlefield creatures
 * (with combined power) and lands, plus hand and graveyard sizes. Creatures are
 * recognized by card type or by carrying power/toughness, so animated lands
 * count as creatures rather than lands. Drives the scrubber hover snapshot.
 */
export function buildReplayBoardCensus(frame: MatchReplayFrame): ReplayBoardCensus {
  const census: ReplayBoardCensus = {
    self: emptySideCensus(),
    opponent: emptySideCensus(),
  };

  for (const object of frame.objects ?? []) {
    const side =
      object.playerSide === "self"
        ? census.self
        : object.playerSide === "opponent"
          ? census.opponent
          : null;
    if (!side) {
      continue;
    }

    const kind = boardZoneKind(object.zoneType);
    if (kind === "hand") {
      side.hand = (side.hand ?? 0) + 1;
      continue;
    }
    if (kind === "graveyard") {
      side.graveyard += 1;
      continue;
    }
    if (kind !== "battlefield") {
      continue;
    }

    const isCreature =
      objectTypeIncludes(object, "creature") ||
      (typeof object.power === "number" && typeof object.toughness === "number");
    if (isCreature) {
      side.creatures += 1;
      if (typeof object.power === "number") {
        side.power += object.power;
      }
      continue;
    }
    if (objectTypeIncludes(object, "land")) {
      side.lands += 1;
    }
  }

  // Replay frames only carry public objects, so the opponent's hand is
  // invisible unless cards were revealed; zero visible cards means "unknown",
  // not "empty". Your own hand is always fully visible.
  if (census.opponent.hand === 0) {
    census.opponent.hand = null;
  }

  return census;
}

/** Classifies a frame's primary event for scrubber tick coloring. */
export function replayFrameTickKind(
  frame: MatchReplayFrame,
  previousFrame: MatchReplayFrame | null,
): ReplayTickKind {
  if (
    replayLifeDelta(previousFrame, frame, "self") !== null ||
    replayLifeDelta(previousFrame, frame, "opponent") !== null
  ) {
    return "life";
  }
  const changes = frame.changes ?? [];
  if (
    changes.some(
      (change) =>
        change.action === "attack" ||
        change.action === "block" ||
        change.action === "stop_attack" ||
        change.action === "stop_block",
    )
  ) {
    return "combat";
  }
  if (changes.some(replayChangeIsCast)) {
    return "spell";
  }
  return "other";
}

/** Tick kind for every frame, in order. */
export function buildReplayTickKinds(
  frames: MatchReplayFrame[],
): ReplayTickKind[] {
  return frames.map((frame, index) =>
    replayFrameTickKind(frame, index > 0 ? frames[index - 1] ?? null : null),
  );
}

export type ReplayBeat = { text: string; note?: string };

function replayObjectPTSuffix(
  frame: MatchReplayFrame,
  instanceId: number,
): string {
  const object = (frame.objects ?? []).find(
    (candidate) => candidate.instanceId === instanceId,
  );
  if (
    object &&
    typeof object.power === "number" &&
    typeof object.toughness === "number"
  ) {
    return ` (${object.power}/${object.toughness})`;
  }
  return "";
}

function replayChangeName(change: MatchReplayChange): string {
  return cardDisplayName({ cardId: change.cardId, cardName: change.cardName });
}

function replaySignedStatModifier(value: number): string {
  return value >= 0 ? `+${value}` : `${value}`;
}

function replayPowerToughnessAbilityBeat(
  frame: MatchReplayFrame,
  relationships: ReplayRelationshipIndex,
): ReplayBeat | null {
  const annotations = replayFrameAnnotations(frame);
  const resolutions = annotations.filter((annotation) =>
    replayAnnotationHasType(annotation, "AnnotationType_ResolutionStart"),
  );

  for (const resolution of resolutions) {
    const abilityId = resolution.affectorId;
    if (typeof abilityId !== "number") {
      continue;
    }

    const modifier = annotations.find(
      (annotation) =>
        annotation.affectorId === abilityId &&
        replayAnnotationHasType(
          annotation,
          "AnnotationType_PowerToughnessModCreated",
        ),
    );
    const targetId = modifier?.affectedIds?.find(
      (candidate): candidate is number => typeof candidate === "number",
    );
    const power = modifier
      ? replayAnnotationDetailIntValue(modifier, "power")
      : undefined;
    const toughness = modifier
      ? replayAnnotationDetailIntValue(modifier, "toughness")
      : undefined;
    if (
      typeof targetId !== "number" ||
      typeof power !== "number" ||
      typeof toughness !== "number"
    ) {
      continue;
    }

    const targetChange = (frame.changes ?? []).find(
      (change) =>
        change.instanceId === targetId && change.action === "stat_change",
    );
    if (!targetChange) {
      continue;
    }

    const abilityDeleted = annotations.find(
      (annotation) =>
        replayAnnotationHasType(
          annotation,
          "AnnotationType_AbilityInstanceDeleted",
        ) && annotation.affectedIds?.includes(abilityId),
    );
    const sourceId =
      abilityDeleted?.affectorId ??
      relationships.abilitySourceIdByAbilityId.get(abilityId);
    const sourceObject =
      (frame.objects ?? []).find((object) => object.instanceId === sourceId) ??
      (typeof sourceId === "number"
        ? relationships.objectsById.get(sourceId)
        : undefined);
    const sourceName = sourceObject
      ? cardDisplayName(sourceObject)
      : replayChangeName(targetChange);
    const targetName = replayChangeName(targetChange);
    const recipient = sourceId === targetId ? "it" : targetName;

    return {
      text: `${sourceName}'s ability gives ${recipient} ${replaySignedStatModifier(power)}/${replaySignedStatModifier(toughness)}`,
    };
  }

  return null;
}

function replayTargetBeat(
  frame: MatchReplayFrame,
  relationships: ReplayRelationshipIndex,
): ReplayBeat | null {
  for (const event of relationships.targetEventsByFrameId.get(frame.id) ?? []) {
    return {
      text:
        event.sourceKind === "ability"
          ? `${event.sourceLabel}'s ability targets ${replayTargetListLabel(event.targets)}`
          : `${event.sourceLabel} targets ${replayTargetListLabel(event.targets)}`,
    };
  }
  return null;
}

function replayDamageBeat(
  frame: MatchReplayFrame,
  relationships: ReplayRelationshipIndex,
): ReplayBeat | null {
  const events = relationships.damageEventsByFrameId.get(frame.id) ?? [];
  const lead = events[0];
  if (!lead) return null;
  return {
    text: `${lead.sourceLabel} deals ${lead.amount} damage to ${lead.target.label}`,
    note:
      events.length > 1
        ? `${events.length - 1} more damage event${events.length === 2 ? "" : "s"}`
        : undefined,
  };
}

function replayTriggerBeat(
  frame: MatchReplayFrame,
  relationships: ReplayRelationshipIndex,
): ReplayBeat | null {
  const events = relationships.triggerEventsByFrameId.get(frame.id) ?? [];
  const lead = events[0];
  if (!lead) return null;
  return {
    text: `${lead.sourceLabel}'s ability triggers`,
    note: `triggered by ${lead.triggeringLabel}`,
  };
}

function replayTriggerSourceListLabel(events: ReplayTriggerEvent[]): string {
  const counts = new Map<string, number>();
  for (const event of events) {
    counts.set(event.sourceLabel, (counts.get(event.sourceLabel) ?? 0) + 1);
  }
  const labels = [...counts].map(([label, count]) =>
    count > 1 ? `${count} ${label} abilities` : label,
  );
  if (labels.length <= 1) return labels[0] ?? "";
  if (labels.length === 2) return `${labels[0]} and ${labels[1]}`;
  return `${labels.slice(0, -1).join(", ")}, and ${labels[labels.length - 1]}`;
}

function replayAttachmentBeat(
  frame: MatchReplayFrame,
  relationships: ReplayRelationshipIndex,
): ReplayBeat | null {
  const event = replayFrameAttachmentEvents(frame, relationships)[0];
  return event
    ? { text: `${event.attachmentLabel} becomes attached to ${event.hostLabel}` }
    : null;
}

function replayCrewBeat(
  frame: MatchReplayFrame,
  relationships: ReplayRelationshipIndex,
): ReplayBeat | null {
  const event = replayFrameCrewEvents(frame, relationships)[0];
  if (!event) return null;
  const crewLabel =
    event.crewLabels.length === 1
      ? event.crewLabels[0]!
      : `${event.crewLabels[0]} and ${event.crewLabels.length - 1} more`;
  return { text: `${crewLabel} crews ${event.vehicleLabel}` };
}

/**
 * Subject + correctly conjugated verb, e.g. "You attack" vs "Opponent attacks".
 * The base verbs here are all regular, so the third-person form just adds "s".
 */
function replayActorVerb(playerSide: string | undefined, base: string): string {
  const subject = timelinePlayerLabel(playerSide);
  const verb = subject === "You" ? base : `${base}s`;
  return `${subject} ${verb}`;
}

/**
 * Coalesces a frame's raw GRE changes into a single human-readable play-by-play
 * beat (with an optional short note), e.g. "Opponent attacks with Otter (2/2)" or
 * "Combat damage · opponent 20 → 18". Falls back to the primary change when no
 * richer pattern matches. This is the narration layer behind the move list and
 * the HUD headline.
 */
export function buildReplayBeat(
  frame: MatchReplayFrame,
  previousFrame: MatchReplayFrame | null,
  relationships: ReplayRelationshipIndex = buildReplayRelationshipIndex([frame]),
): ReplayBeat {
  const changes = frame.changes ?? [];
  const withAction = (action: string) =>
    changes.filter((change) => change.action === action);
  const others = (count: number) =>
    count > 1 ? ` and ${count - 1} more` : "";

  const attacks = withAction("attack");
  if (attacks.length > 0) {
    const lead = attacks[0]!;
    const attacker =
      (frame.objects ?? []).find(
        (object) => object.instanceId === lead.instanceId,
      ) ?? relationships.objectsById.get(lead.instanceId);
    const destination =
      typeof attacker?.attackTargetId === "number"
        ? replayRelationshipTargetForId(relationships, attacker.attackTargetId)
        : null;
    const destinationNote =
      destination && !destination.playerSide
        ? `attacking ${destination.label}`
        : undefined;
    return {
      text: `${replayActorVerb(lead.playerSide, "attack")} with ${replayChangeName(lead)}${replayObjectPTSuffix(frame, lead.instanceId)}${others(attacks.length)}`,
      note: destinationNote,
    };
  }

  const blocks = withAction("block");
  if (blocks.length > 0) {
    const lead = blocks[0]!;
    const deaths = changes.filter(
      (change) =>
        change.action === "move_public" &&
        boardZoneKind(change.fromZoneType ?? "") === "battlefield" &&
        boardZoneKind(change.toZoneType ?? "") === "graveyard",
    ).length;
    return {
      text: `${replayActorVerb(lead.playerSide, "block")} with ${replayChangeName(lead)}${replayObjectPTSuffix(frame, lead.instanceId)}${others(blocks.length)}`,
      note: deaths > 0 ? (deaths === 1 ? "a creature dies" : `${deaths} creatures die`) : undefined,
    };
  }

  const casts = changes.filter(replayChangeIsCast);
  if (casts.length > 0) {
    const lead = casts[0]!;
    const triggers = relationships.triggerEventsByFrameId.get(frame.id) ?? [];
    return {
      text: `${replayActorVerb(lead.playerSide, "cast")} ${replayChangeName(lead)}`,
      note:
        triggers.length > 0
          ? `triggers ${replayTriggerSourceListLabel(triggers)}`
          : undefined,
    };
  }

  const target = replayTargetBeat(frame, relationships);
  if (target) {
    return target;
  }

  const powerToughnessAbility = replayPowerToughnessAbilityBeat(
    frame,
    relationships,
  );
  if (powerToughnessAbility) {
    return powerToughnessAbility;
  }

  const damage = replayDamageBeat(frame, relationships);
  if (damage) return damage;

  const attachment = replayAttachmentBeat(frame, relationships);
  if (attachment) return attachment;

  const crew = replayCrewBeat(frame, relationships);
  if (crew) return crew;

  const trigger = replayTriggerBeat(frame, relationships);
  if (trigger) return trigger;

  const enters = changes.filter((change) => {
    if (
      change.action !== "move_public" ||
      boardZoneKind(change.toZoneType ?? "") !== "battlefield"
    ) {
      return false;
    }
    // A real play resolves from hand or the stack; entries from limbo/other are
    // board-state resync bookkeeping, not a card being played.
    const from = boardZoneKind(change.fromZoneType ?? "");
    return from === "hand" || from === "stack";
  });
  if (enters.length > 0) {
    const lead = enters[0]!;
    const object = (frame.objects ?? []).find(
      (candidate) => candidate.instanceId === lead.instanceId,
    );
    return {
      text: `${replayActorVerb(lead.playerSide, "play")} ${replayChangeName(lead)}${others(enters.length)}`,
      note: object?.isTapped ? "tapped" : undefined,
    };
  }

  const leaves = changes.filter((change) => {
    if (boardZoneKind(change.fromZoneType ?? "") !== "battlefield") {
      return false;
    }
    return (
      change.action === "leave_public" ||
      (change.action === "move_public" &&
        boardZoneKind(change.toZoneType ?? "") !== "battlefield")
    );
  });
  if (leaves.length > 0) {
    const lead = leaves[0]!;
    const destination = boardZoneKind(lead.toZoneType ?? "");
    const name = replayChangeName(lead);
    const text =
      destination === "graveyard"
        ? `${name} is put into the graveyard${others(leaves.length)}`
        : destination === "exile"
          ? `${name} is exiled${others(leaves.length)}`
          : destination === "hand"
            ? `${name} returns to hand${others(leaves.length)}`
            : `${name} leaves the battlefield${others(leaves.length)}`;
    return { text };
  }

  const resolves = changes.filter((change) => {
    if (boardZoneKind(change.fromZoneType ?? "") !== "stack") {
      return false;
    }
    return (
      change.action === "leave_public" ||
      (change.action === "move_public" &&
        boardZoneKind(change.toZoneType ?? "") !== "battlefield")
    );
  });
  if (resolves.length > 0) {
    const lead = resolves[0]!;
    return { text: `${replayChangeName(lead)} resolves` };
  }

  const handEntries = changes.filter(
    (change) =>
      change.action === "enter_public" &&
      boardZoneKind(change.toZoneType ?? "") === "hand",
  );
  if (handEntries.length > 0) {
    const categoryByInstanceId =
      relationships.zoneTransferCategoryByFrameId.get(frame.id);
    // Prefer a card GRE actually explained; uncategorized arrivals in the same
    // frame are resync backfill and make a poor headline.
    const lead =
      handEntries.find((change) =>
        categoryByInstanceId?.has(change.instanceId),
      ) ?? handEntries[0]!;
    const category = categoryByInstanceId?.get(lead.instanceId);
    const name = replayChangeName(lead);
    const rest = others(handEntries.length);
    if (category === "Draw") {
      return {
        text: `${replayActorVerb(lead.playerSide, "draw")} ${name}${rest}`,
      };
    }
    if (category === "Return") {
      return { text: `${name} returns to hand${rest}` };
    }
    if (category === "Put") {
      return {
        text: `${replayActorVerb(lead.playerSide, "put")} ${name} into hand${rest}`,
      };
    }
    // Your own hand is fully tracked, so a card surfacing there is never a
    // reveal — it is the opening hand or a post-resync backfill. Only the
    // opponent's hidden hand produces genuine reveals.
    if (lead.playerSide === "self") {
      return {
        text: `${name}${rest} ${handEntries.length > 1 ? "enter" : "enters"} your hand`,
      };
    }
    return {
      text: `${replayActorVerb(lead.playerSide, "reveal")} ${name}${rest}`,
    };
  }

  const hides = changes.filter(replayChangeIsGenuineVisibilityLoss);
  if (hides.length > 0) {
    const lead = hides[0]!;
    return {
      text: `${replayChangeName(lead)} is no longer revealed${others(hides.length)}`,
    };
  }

  const selfDelta = replayLifeDelta(previousFrame, frame, "self");
  const opponentDelta = replayLifeDelta(previousFrame, frame, "opponent");
  if (selfDelta !== null || opponentDelta !== null) {
    const segments: string[] = [];
    if (opponentDelta !== null) {
      const before = replayFrameLifeTotalForSide(previousFrame, "opponent");
      segments.push(`opponent ${before} → ${frame.opponentLifeTotal}`);
    }
    if (selfDelta !== null) {
      const before = replayFrameLifeTotalForSide(previousFrame, "self");
      segments.push(`you ${before} → ${frame.selfLifeTotal}`);
    }
    return { text: `Life change · ${segments.join(" · ")}` };
  }

  const taps = withAction("tap").length;
  const untaps = withAction("untap").length;
  if (untaps > 0 && untaps >= taps) {
    const lead = withAction("untap")[0]!;
    return {
      text: `${replayActorVerb(lead.playerSide, "untap")} ${untaps === 1 ? replayChangeName(lead) : `${untaps} permanents`}`,
    };
  }
  if (taps > 0) {
    const lead = withAction("tap")[0]!;
    return {
      text: `${replayActorVerb(lead.playerSide, "tap")} ${taps === 1 ? replayChangeName(lead) : `${taps} permanents`}`,
    };
  }

  const narratable = changes.filter(
    (change) => !replayChangeIsNoiseMove(change),
  );
  const primary = [...narratable].sort(
    (a, b) => replayChangePriority(b.action) - replayChangePriority(a.action),
  )[0];
  if (primary) {
    return { text: describeReplayChange(primary).replace(/\.$/, "") };
  }
  return { text: replayFramePrimarySummary(frame, previousFrame).replace(/\.$/, "") };
}

export type ReplayKeyMoment = {
  index: number;
  kind: "swing" | "decisive";
  label: string;
};

/**
 * Auto-detects the handful of steps worth jumping straight to: the moment a
 * player hits 0 life (decisive), and the biggest life swings of the game. Pinned
 * on the scrubber so a long replay reads as a story.
 */
export function findReplayKeyMoments(
  frames: MatchReplayFrame[],
): ReplayKeyMoment[] {
  if (frames.length === 0) {
    return [];
  }

  const byIndex = new Map<number, ReplayKeyMoment>();

  for (let index = 0; index < frames.length; index += 1) {
    const winner = replayFrameLifeTotalWinner(frames[index]!);
    if (winner !== "unknown") {
      const loser = winner === "self" ? "Opponent" : "You";
      byIndex.set(index, {
        index,
        kind: "decisive",
        label: `${loser} hit 0 life`,
      });
      break;
    }
  }

  const swings: { index: number; magnitude: number; label: string }[] = [];
  for (let index = 1; index < frames.length; index += 1) {
    const selfDelta = replayLifeDelta(frames[index - 1]!, frames[index]!, "self") ?? 0;
    const opponentDelta =
      replayLifeDelta(frames[index - 1]!, frames[index]!, "opponent") ?? 0;
    const magnitude = Math.abs(selfDelta) + Math.abs(opponentDelta);
    if (magnitude < 3) {
      continue;
    }
    const parts: string[] = [];
    if (opponentDelta !== 0) {
      parts.push(`opponent ${opponentDelta > 0 ? "+" : ""}${opponentDelta}`);
    }
    if (selfDelta !== 0) {
      parts.push(`you ${selfDelta > 0 ? "+" : ""}${selfDelta}`);
    }
    swings.push({ index, magnitude, label: `Life swing · ${parts.join(", ")}` });
  }
  swings.sort((a, b) => b.magnitude - a.magnitude || a.index - b.index);
  for (const swing of swings.slice(0, 3)) {
    if (!byIndex.has(swing.index)) {
      byIndex.set(swing.index, {
        index: swing.index,
        kind: "swing",
        label: swing.label,
      });
    }
  }

  return [...byIndex.values()].sort((a, b) => a.index - b.index);
}
