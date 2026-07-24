import type { RankState } from "./types";

const RANK_SPRITE_START: Record<string, number> = {
  Bronze: 1,
  Silver: 5,
  Gold: 9,
  Platinum: 13,
  Diamond: 17,
};

/**
 * Position of a standing in Arena's 48px rank sprite. The neutral badge at
 * index 0 is used when Arena reports an incomplete or unsupported standing.
 */
export function rankSymbolIndex(rank: Pick<RankState, "rankClass" | "level"> | null): number {
  if (!rank) return 0;

  const rankClass = rank.rankClass.trim();
  if (rankClass === "Mythic") return 21;

  const start = RANK_SPRITE_START[rankClass];
  const level = rank.level;
  if (start == null || level == null || !Number.isInteger(level) || level < 1 || level > 4) {
    return 0;
  }

  return start + level - 1;
}
