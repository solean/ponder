import { parseEventName } from "./events";
import type { DraftPick, DraftPickCard, DraftSession } from "./types";
import type { CardRarity } from "./scryfall";

export type DraftSessionStatus =
  | "complete"
  | "in-progress"
  | "recording-incomplete"
  | "empty";

export type DraftPickLogPick = {
  pickNumber: number;
  displayPick: number;
  pickedCards: DraftPickCard[];
  packCards: DraftPickCard[];
};

export type DraftPickLogPack = {
  packNumber: number;
  displayPack: number;
  picks: DraftPickLogPick[];
};

function validDateValue(timestamp?: string | null): number | null {
  if (!timestamp) {
    return null;
  }
  const value = new Date(timestamp).getTime();
  return Number.isFinite(value) ? value : null;
}

function parseDraftCardIDs(raw: string): number[] {
  const trimmed = raw.trim();
  if (!trimmed || trimmed === "[]") {
    return [];
  }

  try {
    const decoded = JSON.parse(trimmed) as unknown;
    if (Array.isArray(decoded)) {
      return decoded
        .map((value) => {
          if (typeof value === "number" && Number.isFinite(value)) {
            return Math.trunc(value);
          }
          if (typeof value === "string") {
            const parsed = Number.parseInt(value.trim(), 10);
            return Number.isFinite(parsed) ? parsed : 0;
          }
          return 0;
        })
        .filter((cardID) => cardID > 0);
    }
  } catch {
    // Fall through to the tolerant legacy-log parser.
  }

  return (trimmed.match(/\d+/g) ?? [])
    .map((value) => Number.parseInt(value, 10))
    .filter((cardID) => Number.isFinite(cardID) && cardID > 0);
}

function normalizedDraftCards(cards: DraftPickCard[] | undefined, raw: string): DraftPickCard[] {
  if (Array.isArray(cards) && cards.length > 0) {
    return cards.map((card) => ({
      cardId: card.cardId,
      cardName: card.cardName,
    }));
  }

  return parseDraftCardIDs(raw).map((cardId) => ({ cardId }));
}

export function draftPickLogPacks(picks: DraftPick[]): DraftPickLogPack[] {
  const packOffset = picks.some((pick) => pick.packNumber === 0) ? 1 : 0;
  const pickOffset = picks.some((pick) => pick.pickNumber === 0) ? 1 : 0;
  const grouped = new Map<number, DraftPickLogPick[]>();

  for (const pick of picks) {
    const rows = grouped.get(pick.packNumber) ?? [];
    rows.push({
      pickNumber: pick.pickNumber,
      displayPick: pick.pickNumber + pickOffset,
      pickedCards: normalizedDraftCards(pick.pickedCards, pick.pickedCardIds),
      packCards: normalizedDraftCards(pick.packCards, pick.packCardIds),
    });
    grouped.set(pick.packNumber, rows);
  }

  return [...grouped.entries()]
    .sort((a, b) => a[0] - b[0])
    .map(([packNumber, rows]) => ({
      packNumber,
      displayPack: packNumber + packOffset,
      picks: rows.sort((a, b) => a.pickNumber - b.pickNumber),
    }));
}
export function draftAlternativeCards(pick: DraftPickLogPick): DraftPickCard[] {
  const selectedCounts = new Map<number, number>();
  for (const card of pick.pickedCards) {
    selectedCounts.set(card.cardId, (selectedCounts.get(card.cardId) ?? 0) + 1);
  }

  return pick.packCards.filter((card) => {
    const selected = selectedCounts.get(card.cardId) ?? 0;
    if (selected === 0) {
      return true;
    }
    selectedCounts.set(card.cardId, selected - 1);
    return false;
  });
}

/** 17Lands ratings a pick verdict needs; both are null when 17Lands has too few games. */
export type DraftPickRating = {
  gihWinRate: number | null;
  ata: number | null;
};

/** Picks this far either side of a card's ATA count as taken late or early. */
export const DRAFT_PICK_TIMING_THRESHOLD = 2.5;

export type DraftPickVerdict = {
  /** The rated picked card the verdict describes (the best one when a pick takes several). */
  cardId: number | null;
  gihWinRate: number | null;
  /** GIH WR minus the format average, as a fraction. */
  delta: number | null;
  ata: number | null;
  /** 1-based standing of the pick by GIH WR among the rated cards in its pack. */
  rank: number | null;
  /** Rated cards in the pack, the pick included; rank is out of this. */
  ratedCount: number;
  /** The highest-rated card passed, when it rates above the pick. */
  bestPassed: { card: DraftPickCard; gihWinRate: number } | null;
  timing: "late" | "early" | null;
};

/**
 * How a pick compares with the pack it came from: its GIH WR against the
 * format average and against every other rated card in the pack, and whether
 * it was taken well after (late) or before (early) its 17Lands ATA.
 */
export function draftPickVerdict(
  pick: DraftPickLogPick,
  ratingOf: (cardId: number) => DraftPickRating | undefined,
  formatMean: number | null,
): DraftPickVerdict {
  let picked: { card: DraftPickCard; rating: DraftPickRating | undefined } | null = null;
  for (const card of pick.pickedCards) {
    const rating = ratingOf(card.cardId);
    const gih = rating?.gihWinRate ?? null;
    const best = picked?.rating?.gihWinRate ?? null;
    if (picked == null || (gih != null && (best == null || gih > best))) {
      picked = { card, rating };
    }
  }

  const gihWinRate = picked?.rating?.gihWinRate ?? null;
  const ata = picked?.rating?.ata ?? null;
  const verdict: DraftPickVerdict = {
    cardId: picked?.card.cardId ?? null,
    gihWinRate,
    delta: gihWinRate != null && formatMean != null ? gihWinRate - formatMean : null,
    ata,
    rank: null,
    ratedCount: 0,
    bestPassed: null,
    timing: null,
  };

  if (ata != null) {
    const lateBy = pick.displayPick - ata;
    if (lateBy >= DRAFT_PICK_TIMING_THRESHOLD) verdict.timing = "late";
    else if (lateBy <= -DRAFT_PICK_TIMING_THRESHOLD) verdict.timing = "early";
  }

  if (gihWinRate == null || pick.packCards.length === 0) {
    return verdict;
  }

  let rank = 1;
  let ratedCount = 1;
  for (const card of draftAlternativeCards(pick)) {
    const gih = ratingOf(card.cardId)?.gihWinRate ?? null;
    if (gih == null) continue;
    ratedCount += 1;
    if (gih > gihWinRate) {
      rank += 1;
      if (verdict.bestPassed == null || gih > verdict.bestPassed.gihWinRate) {
        verdict.bestPassed = { card, gihWinRate: gih };
      }
    }
  }
  verdict.rank = rank;
  verdict.ratedCount = ratedCount;
  return verdict;
}

export type DraftPickVerdictSummary = {
  /** Mean GIH WR delta over the rated picks, as a fraction. */
  averageDelta: number | null;
  /** Picks that rated highest in a pack with at least one other rated card. */
  bestCount: number;
  /** Picks that could be ranked against at least one other rated card. */
  rankedCount: number;
  lateCount: number;
};

export function summarizeDraftPickVerdicts(verdicts: DraftPickVerdict[]): DraftPickVerdictSummary {
  let deltaTotal = 0;
  let deltaCount = 0;
  let bestCount = 0;
  let rankedCount = 0;
  let lateCount = 0;
  for (const verdict of verdicts) {
    if (verdict.delta != null) {
      deltaTotal += verdict.delta;
      deltaCount += 1;
    }
    if (verdict.rank != null && verdict.ratedCount > 1) {
      rankedCount += 1;
      if (verdict.rank === 1) bestCount += 1;
    }
    if (verdict.timing === "late") lateCount += 1;
  }
  return {
    averageDelta: deltaCount > 0 ? deltaTotal / deltaCount : null,
    bestCount,
    rankedCount,
    lateCount,
  };
}


export function draftSessionStatus(session: DraftSession): DraftSessionStatus {
  if (validDateValue(session.completedAt) != null) {
    return "complete";
  }
  if (validDateValue(session.startedAt) != null) {
    return "in-progress";
  }
  if (session.picks > 0) {
    return "recording-incomplete";
  }
  return "empty";
}

export function draftSessionStatusLabel(status: DraftSessionStatus): string {
  switch (status) {
    case "complete":
      return "Complete";
    case "in-progress":
      return "In progress";
    case "recording-incomplete":
      return "Recording incomplete";
    case "empty":
      return "No picks recorded";
  }
}

export function draftSessionDurationSeconds(session: DraftSession): number | null {
  const startedAt = validDateValue(session.startedAt);
  const completedAt = validDateValue(session.completedAt);
  if (startedAt == null || completedAt == null || completedAt <= startedAt) {
    return null;
  }
  return Math.floor((completedAt - startedAt) / 1000);
}

export function draftSessionType(session: DraftSession): string {
  const parsed = parseEventName(session.eventName);
  if (parsed.kindLabel !== "Unknown event") {
    return parsed.kindLabel;
  }
  return session.isBotDraft ? "Bot Draft" : "Player Draft";
}

export function draftSessionRecord(session: DraftSession): {
  label: string;
  winRate: number | null;
} | null {
  if (session.wins == null || session.losses == null) {
    return null;
  }
  const games = session.wins + session.losses;
  return {
    label: `${session.wins}–${session.losses}`,
    winRate: games > 0 ? session.wins / games : null,
  };
}

export function draftReplayCoverage(picks: DraftPick[]): {
  covered: number;
  total: number;
} {
  return {
    covered: picks.filter((pick) => (pick.packCards?.length ?? 0) > 0).length,
    total: picks.length,
  };
}

const PACK_RARITY_RANK: Record<CardRarity, number> = { mythic: 0, rare: 1, uncommon: 2, common: 3 };

/** Orders pack cards mythic → common with unknown rarity last; ties keep pack order. */
export function sortPackByRarity<T>(cards: readonly T[], rarityOf: (card: T) => CardRarity | undefined): T[] {
  const rank = (card: T) => {
    const rarity = rarityOf(card);
    return rarity ? PACK_RARITY_RANK[rarity] : 4;
  };
  // Array.prototype.sort is stable, so equal ranks keep their original order.
  return [...cards].sort((left, right) => rank(left) - rank(right));
}
