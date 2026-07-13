import type { CardPreview } from "../scryfall";
import type {
  MatchCardPlay,
  MatchReplayChange,
  MatchReplayFrame,
  MatchReplayFrameObject,
} from "../types";

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
export type ReplayConnectionKind = "combat" | "spellTarget";
export type ReplayBoardConnection = {
  kind: ReplayConnectionKind;
  sourceId: number;
  targetId: number;
};
export type ReplayTarget = {
  targetId: number;
  label: string;
};
export type ReplayTargetLookup = Map<number, ReplayTarget[]>;
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
  return turnNumber && turnNumber > 0 ? `T${turnNumber}` : "Pre";
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
    ? `Turn ${turnNumber}`
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

export function replayTargetListLabel(targets: ReplayTarget[]): string {
  const labels = targets.map((target) => target.label);
  if (labels.length === 0) return "";
  if (labels.length === 1) return labels[0]!;
  if (labels.length === 2) return `${labels[0]} and ${labels[1]}`;
  return `${labels.slice(0, -1).join(", ")}, and ${labels[labels.length - 1]}`;
}

export function buildReplayTargetLookup(
  frames: MatchReplayFrame[],
): ReplayTargetLookup {
  const objectById = new Map<number, MatchReplayFrameObject>();
  const playerSideBySeatId = new Map<number, "self" | "opponent">();

  for (const frame of frames) {
    for (const object of frame.objects ?? []) {
      objectById.set(object.instanceId, object);
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
  }

  const targetIdsBySourceId = new Map<number, number[]>();
  for (const frame of frames) {
    for (const annotation of replayFrameAnnotations(frame)) {
      if (
        !replayAnnotationHasType(annotation, "AnnotationType_TargetSpec") ||
        typeof annotation.affectorId !== "number"
      ) {
        continue;
      }

      let targetIds = targetIdsBySourceId.get(annotation.affectorId);
      if (!targetIds) {
        targetIds = [];
        targetIdsBySourceId.set(annotation.affectorId, targetIds);
      }
      for (const targetId of annotation.affectedIds ?? []) {
        if (typeof targetId === "number" && !targetIds.includes(targetId)) {
          targetIds.push(targetId);
        }
      }
    }
  }

  const lookup: ReplayTargetLookup = new Map();
  for (const [sourceId, targetIds] of targetIdsBySourceId) {
    const targets = targetIds.map((targetId) => {
      const object = objectById.get(targetId);
      if (object) {
        return {
          targetId,
          label: cardDisplayName(object),
        };
      }

      const playerSide = playerSideBySeatId.get(targetId);
      return {
        targetId,
        label:
          playerSide === "self"
            ? "you"
            : playerSide === "opponent"
              ? "opponent"
              : `target ${targetId}`,
      };
    });
    if (targets.length > 0) {
      lookup.set(sourceId, targets);
    }
  }

  return lookup;
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
  for (const counter of replayObjectCounterSummaries(object)) {
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
  return (frame.changes ?? []).some(
    (change) => !replayChangeIsNoiseMove(change),
  );
}

export function isMeaningfulReplayFrame(
  frame: MatchReplayFrame,
  previousFrame: MatchReplayFrame | null,
): boolean {
  return (
    replayFrameHasNarratableChange(frame) ||
    replayFrameHasTargetSpec(frame) ||
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
    const sourceId = abilityDeleted?.affectorId;
    const sourceObject = (frame.objects ?? []).find(
      (object) => object.instanceId === sourceId,
    );
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

function replayTargetBeat(frame: MatchReplayFrame): ReplayBeat | null {
  const targetsBySourceId = buildReplayTargetLookup([frame]);
  for (const [sourceId, targets] of targetsBySourceId) {
    const source = (frame.objects ?? []).find(
      (object) => object.instanceId === sourceId,
    );
    if (!source) {
      continue;
    }
    return {
      text: `${cardDisplayName(source)} targets ${replayTargetListLabel(targets)}`,
    };
  }
  return null;
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
): ReplayBeat {
  const changes = frame.changes ?? [];
  const withAction = (action: string) =>
    changes.filter((change) => change.action === action);
  const others = (count: number) =>
    count > 1 ? ` and ${count - 1} more` : "";

  const attacks = withAction("attack");
  if (attacks.length > 0) {
    const lead = attacks[0]!;
    return {
      text: `${replayActorVerb(lead.playerSide, "attack")} with ${replayChangeName(lead)}${replayObjectPTSuffix(frame, lead.instanceId)}${others(attacks.length)}`,
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
    return {
      text: `${replayActorVerb(lead.playerSide, "cast")} ${replayChangeName(lead)}`,
    };
  }

  const target = replayTargetBeat(frame);
  if (target) {
    return target;
  }

  const powerToughnessAbility = replayPowerToughnessAbilityBeat(frame);
  if (powerToughnessAbility) {
    return powerToughnessAbility;
  }

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

  const leaves = changes.filter(
    (change) =>
      change.action === "move_public" &&
      boardZoneKind(change.fromZoneType ?? "") === "battlefield" &&
      boardZoneKind(change.toZoneType ?? "") !== "battlefield",
  );
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

  const resolves = changes.filter(
    (change) =>
      change.action === "move_public" &&
      boardZoneKind(change.fromZoneType ?? "") === "stack" &&
      boardZoneKind(change.toZoneType ?? "") !== "battlefield",
  );
  if (resolves.length > 0) {
    const lead = resolves[0]!;
    return { text: `${replayChangeName(lead)} resolves` };
  }

  const reveals = changes.filter(
    (change) =>
      change.action === "enter_public" &&
      boardZoneKind(change.toZoneType ?? "") === "hand",
  );
  if (reveals.length > 0) {
    const lead = reveals[0]!;
    return {
      text: `${replayActorVerb(lead.playerSide, "reveal")} ${replayChangeName(lead)}${others(reveals.length)}`,
    };
  }

  const hides = changes.filter((change) => change.action === "leave_public");
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
