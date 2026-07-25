import type { RankHistoryPoint, RankState } from "./types";

export type Ladder = "constructed" | "limited";
export type SeasonView = "current" | "previous" | "all";

type LadderConfig = {
  label: string;
  tiers: string[];
};

export type GraphPoint = {
  matchNumber: number;
  score: number;
  seasonOrdinal: number;
  rankLabel: string;
  result: RankHistoryPoint["result"];
  eventName: string;
  opponent: string;
  observedAt: string;
  endedAt: string;
};

export type RankProgressSeries = {
  seasonView: SeasonView;
  seasonOrdinal: number | null;
  seasonOrdinals: number[];
  latestState: RankState;
  record: {
    wins: number;
    losses: number;
  } | null;
  points: GraphPoint[];
};

export const LADDER_CONFIG: Record<Ladder, LadderConfig> = {
  constructed: {
    label: "Constructed",
    tiers: ["Spark", "Bronze", "Silver", "Gold", "Platinum", "Diamond", "Mythic"],
  },
  limited: {
    label: "Limited",
    tiers: ["Bronze", "Silver", "Gold", "Platinum", "Diamond", "Mythic"],
  },
};

const leaderboardFormatter = new Intl.NumberFormat(undefined, {
  maximumFractionDigits: 0,
});
const percentileFormatter = new Intl.NumberFormat(undefined, {
  style: "percent",
  maximumFractionDigits: 1,
});

function leaderboardPlaceFor(rank: RankState): number | null {
  const place = rank.leaderboardPlace;
  return place != null && Number.isInteger(place) && place > 0 ? place : null;
}

function percentileFor(rank: RankState): number | null {
  const percentile = rank.percentile;
  return percentile != null &&
    Number.isFinite(percentile) &&
    percentile >= 0 &&
    percentile <= 100
    ? percentile
    : null;
}


function rankClassFor(rank: RankState): string {
  const rankClass = rank.rankClass.trim();
  if (rankClass) return rankClass;
  const percentile = percentileFor(rank);
  if (
    rank.level === 0 ||
    leaderboardPlaceFor(rank) != null ||
    (percentile != null && percentile > 0)
  ) {
    return "Mythic";
  }
  return "Bronze";
}

function mythicScore(rank: RankState, tierIndex: number): number {
  const leaderboardPlace = leaderboardPlaceFor(rank);
  if (leaderboardPlace != null) {
    // Numbered ranks are above percentile ranks. Log compression keeps both
    // movement near the leaderboard cutoff and movement near #1 visible.
    return tierIndex + 0.8 + 0.19 / (1 + Math.log10(leaderboardPlace));
  }

  const percentile = percentileFor(rank);
  if (percentile != null) {
    return tierIndex + 0.01 + 0.77 * (percentile / 100);
  }
  return tierIndex + 0.01;
}

function formatMythicRank(rank: RankState): string {
  const leaderboardPlace = leaderboardPlaceFor(rank);
  if (leaderboardPlace != null) {
    return `Mythic #${leaderboardFormatter.format(leaderboardPlace)}`;
  }
  const percentile = percentileFor(rank);
  if (percentile != null) {
    return `Mythic ${percentileFormatter.format(percentile / 100)}`;
  }
  return "Mythic";
}
export function rankStateFor(point: RankHistoryPoint, ladder: Ladder): RankState {
  return ladder === "constructed" ? point.constructed : point.limited;
}


function stepsPerLevel(ladder: Ladder, rankClass: string): number {
  if (rankClass === "Mythic") return 1;
  if (ladder === "constructed") {
    if (rankClass === "Spark") return 5;
    return 6;
  }
  if (rankClass === "Bronze") return 4;
  return 5;
}

export function formatRankLabel(rank: RankState): string {
  if (rank.seasonOrdinal == null) return "Unranked";
  const rankClass = rankClassFor(rank);
  if (rankClass === "Mythic") return formatMythicRank(rank);
  if (rank.level == null) return "Unranked";
  return `${rankClass} ${rank.level}`;
}

/**
 * Absolute ladder position measured in steps (Arena "pips") from the bottom
 * of the ladder. Mythic returns the ladder's step cap, so climbing into
 * Mythic counts but movement within Mythic (percentile-based) does not.
 * Null when the rank is unknown.
 */
export function rankStepIndex(rank: RankState, ladder: Ladder): number | null {
  if (rank.level == null || rank.seasonOrdinal == null) return null;
  // Without a known class the absolute position is ambiguous — no Bronze
  // fallback here, unlike display labels: step math must not guess tiers.
  const rankClass = rank.rankClass.trim();
  if (!rankClass) return null;
  const config = LADDER_CONFIG[ladder];
  const tierIndex = config.tiers.indexOf(rankClass);
  if (tierIndex === -1) return null;

  let steps = 0;
  for (let index = 0; index < tierIndex; index += 1) {
    const tier = config.tiers[index];
    if (tier === "Mythic") continue;
    steps += 4 * stepsPerLevel(ladder, tier);
  }
  if (rankClass === "Mythic") return steps;

  const totalSteps = stepsPerLevel(ladder, rankClass);
  const level = Math.min(Math.max(rank.level, 1), 4);
  steps += (4 - level) * totalSteps;
  steps += Math.min(Math.max(rank.step ?? 0, 0), totalSteps);
  return steps;
}

/**
 * Every match recorded against the given ladder: the points where that
 * ladder's state (rank or win/loss counters) actually changed.
 */
export function ladderMatchPoints(history: RankHistoryPoint[], ladder: Ladder): RankHistoryPoint[] {
  return changedPointsFor(history, ladder);
}

export function normalizedRankClass(rank: RankState): string {
  return rankClassFor(rank);
}

/**
 * Arena's rank responses frequently omit the rank-class string while still
 * reporting level and step. Within one season the class only moves up at a
 * promotion (level 1 -> level 4 of the next tier), so missing classes can be
 * inferred by carrying known classes forward — and backward across the same
 * boundary rule for points before the first explicit class. Points that
 * cannot be anchored to any explicit class in their season keep an empty
 * class. Returns copies; the input is not mutated.
 */
export function fillMissingRankClasses(history: RankHistoryPoint[]): RankHistoryPoint[] {
  const out = history.map((point) => ({
    ...point,
    constructed: { ...point.constructed },
    limited: { ...point.limited },
  }));

  for (const ladder of ["constructed", "limited"] as const) {
    const tiers = LADDER_CONFIG[ladder].tiers;
    const bySeason = new Map<number, RankState[]>();
    for (const point of out) {
      const rank = ladder === "constructed" ? point.constructed : point.limited;
      const percentile = percentileFor(rank);
      const hasMythicStanding =
        leaderboardPlaceFor(rank) != null || (percentile != null && percentile > 0);
      if (rank.seasonOrdinal == null || (rank.level == null && !hasMythicStanding)) continue;
      const group = bySeason.get(rank.seasonOrdinal);
      if (group) group.push(rank);
      else bySeason.set(rank.seasonOrdinal, [rank]);
    }

    for (const ranks of bySeason.values()) {
      const assigned: Array<number | null> = ranks.map(() => null);

      const promoted = (prev: RankState, next: RankState) =>
        prev.level === 1 && (next.level === 4 || next.level === 0);

      let tier: number | null = null;
      for (let index = 0; index < ranks.length; index += 1) {
        const explicit = ranks[index].rankClass.trim();
        const percentile = percentileFor(ranks[index]);
        const hasMythicStanding =
          leaderboardPlaceFor(ranks[index]) != null || (percentile != null && percentile > 0);
        const explicitTier = explicit
          ? tiers.indexOf(explicit)
          : ranks[index].level === 0 || hasMythicStanding
            ? tiers.length - 1
            : -1;
        if (explicitTier !== -1) {
          tier = explicitTier;
        } else if (tier != null && index > 0 && promoted(ranks[index - 1], ranks[index])) {
          tier = Math.min(tier + 1, tiers.length - 1);
        }
        assigned[index] = tier;
      }

      let nextTier: number | null = null;
      for (let index = ranks.length - 1; index >= 0; index -= 1) {
        if (assigned[index] != null) {
          nextTier = assigned[index];
          continue;
        }
        if (nextTier != null) {
          if (promoted(ranks[index], ranks[index + 1])) {
            nextTier = Math.max(nextTier - 1, 0);
          }
          assigned[index] = nextTier;
        }
      }

      for (let index = 0; index < ranks.length; index += 1) {
        const tierIndex = assigned[index];
        if (tierIndex != null && !ranks[index].rankClass.trim()) {
          ranks[index].rankClass = tiers[tierIndex];
        }
      }
    }
  }

  return out;
}

function rankScore(rank: RankState, ladder: Ladder): number | null {
  if (rank.seasonOrdinal == null) return null;

  const rankClass = rankClassFor(rank);
  const config = LADDER_CONFIG[ladder];
  const tierIndex = config.tiers.indexOf(rankClass);
  if (tierIndex === -1) return null;
  if (rankClass === "Mythic") return mythicScore(rank, tierIndex);
  if (rank.level == null) return null;
  const level = Math.min(Math.max(rank.level, 1), 4);
  const totalSteps = stepsPerLevel(ladder, rankClass);
  const stepProgress =
    rank.step != null ? Math.min(Math.max(rank.step, 0), totalSteps) / totalSteps : 0;

  return tierIndex + ((4 - level) + stepProgress) / 4;
}

function sameNullableNumber(a?: number | null, b?: number | null): boolean {
  return a == null ? b == null : a === b;
}

function sameRankState(a: RankState | null | undefined, b: RankState | null | undefined): boolean {
  if (!a || !b) return a === b;

  return (
    sameNullableNumber(a.seasonOrdinal, b.seasonOrdinal) &&
    rankClassFor(a) === rankClassFor(b) &&
    sameNullableNumber(a.level, b.level) &&
    sameNullableNumber(a.step, b.step) &&
    sameNullableNumber(a.percentile, b.percentile) &&
    sameNullableNumber(a.leaderboardPlace, b.leaderboardPlace) &&
    sameNullableNumber(a.matchesWon, b.matchesWon) &&
    sameNullableNumber(a.matchesLost, b.matchesLost)
  );
}

function ladderStateChanged(
  previousPoint: RankHistoryPoint | null,
  point: RankHistoryPoint,
  ladder: Ladder,
): boolean {
  const current = rankStateFor(point, ladder);
  if (current.seasonOrdinal == null) return false;
  if (!previousPoint) return true;
  return !sameRankState(rankStateFor(previousPoint, ladder), current);
}

function changedPointsFor(history: RankHistoryPoint[], ladder: Ladder): RankHistoryPoint[] {
  return history.filter((point, index) =>
    ladderStateChanged(index > 0 ? history[index - 1] : null, point, ladder),
  );
}

export function seasonOrdinalsFor(history: RankHistoryPoint[], ladder: Ladder): number[] {
  const ordinals: number[] = [];

  for (const point of changedPointsFor(history, ladder)) {
    const seasonOrdinal = rankStateFor(point, ladder).seasonOrdinal;
    if (seasonOrdinal == null) continue;
    if (ordinals[ordinals.length - 1] !== seasonOrdinal) {
      ordinals.push(seasonOrdinal);
    }
  }

  return ordinals;
}

function recordForSeries(
  history: RankHistoryPoint[],
  ladder: Ladder,
  seasonView: SeasonView,
): RankProgressSeries["record"] {
  if (history.length === 0) return null;

  if (seasonView !== "all") {
    const latestState = rankStateFor(history[history.length - 1], ladder);
    if (latestState.matchesWon == null || latestState.matchesLost == null) return null;
    return {
      wins: latestState.matchesWon,
      losses: latestState.matchesLost,
    };
  }

  const finalSeasonStates = new Map<number, RankState>();
  for (const point of history) {
    const rank = rankStateFor(point, ladder);
    if (rank.seasonOrdinal == null) continue;
    finalSeasonStates.set(rank.seasonOrdinal, rank);
  }
  if (finalSeasonStates.size === 0) return null;

  let wins = 0;
  let losses = 0;
  for (const rank of finalSeasonStates.values()) {
    if (rank.matchesWon == null || rank.matchesLost == null) return null;
    wins += rank.matchesWon;
    losses += rank.matchesLost;
  }

  return { wins, losses };
}

export function buildGraphPoints(
  history: RankHistoryPoint[],
  ladder: Ladder,
  seasonView: SeasonView = "current",
): RankProgressSeries | null {
  const changedPoints = changedPointsFor(history, ladder);
  if (changedPoints.length === 0) return null;

  const seasonOrdinals = seasonOrdinalsFor(history, ladder);
  if (seasonOrdinals.length === 0) return null;

  const seasonOrdinal =
    seasonView === "all"
      ? null
      : seasonView === "previous"
        ? seasonOrdinals[seasonOrdinals.length - 2] ?? null
        : seasonOrdinals[seasonOrdinals.length - 1] ?? null;
  if (seasonView !== "all" && seasonOrdinal == null) return null;

  const filteredPoints = changedPoints.filter((point) =>
    seasonOrdinal == null ? true : rankStateFor(point, ladder).seasonOrdinal === seasonOrdinal,
  );
  if (filteredPoints.length === 0) return null;

  const points = filteredPoints
    .map((point, index) => {
      const rank = rankStateFor(point, ladder);
      const score = rankScore(rank, ladder);
      const pointSeasonOrdinal = rank.seasonOrdinal;
      if (pointSeasonOrdinal == null) return null;
      if (score == null) return null;
      return {
        matchNumber: index + 1,
        score,
        seasonOrdinal: pointSeasonOrdinal,
        rankLabel: formatRankLabel(rank),
        result: point.result,
        eventName: point.eventName,
        opponent: point.opponent,
        observedAt: point.observedAt,
        endedAt: point.endedAt,
      } satisfies GraphPoint;
    })
    .filter((point): point is GraphPoint => point !== null);

  if (points.length === 0) return null;
  return {
    seasonView,
    seasonOrdinal,
    seasonOrdinals,
    latestState: rankStateFor(filteredPoints[filteredPoints.length - 1], ladder),
    record: recordForSeries(filteredPoints, ladder, seasonView),
    points,
  };
}

export function tierLabelAt(value: number, ladder: Ladder): string {
  const rounded = Math.round(value);
  if (Math.abs(value - rounded) > 0.001) return "";
  return LADDER_CONFIG[ladder].tiers[rounded] ?? "";
}
