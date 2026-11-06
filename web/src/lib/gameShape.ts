import type { AnalyticsBucket } from "./types";

/** Game lengths at or past this many turns share one "15+" row. */
export const GAME_LENGTH_CAP = 15;

/**
 * Folds every game-length bucket at or past `cap` turns into a single bucket
 * keyed `cap`, so the long tail of rare marathon games reads as one row.
 */
export function capGameLengths(buckets: AnalyticsBucket[], cap = GAME_LENGTH_CAP): AnalyticsBucket[] {
  const below = buckets.filter((bucket) => bucket.key < cap).sort((a, b) => a.key - b.key);
  const tail = buckets.filter((bucket) => bucket.key >= cap);
  if (tail.length === 0) return below;
  const merged = tail.reduce<AnalyticsBucket>(
    (total, bucket) => ({
      key: cap,
      record: {
        games: total.record.games + bucket.record.games,
        wins: total.record.wins + bucket.record.wins,
        losses: total.record.losses + bucket.record.losses,
        draws: total.record.draws + bucket.record.draws,
      },
      unknownResults: total.unknownResults + bucket.unknownResults,
    }),
    { key: cap, record: { games: 0, wins: 0, losses: 0, draws: 0 }, unknownResults: 0 },
  );
  return [...below, merged];
}

/**
 * Evenly spaced y-axis ticks from 0 that cover `max`: half steps for small
 * per-turn averages like spells cast, whole or double steps for land counts.
 */
export function curveTicks(max: number): number[] {
  const safeMax = Math.max(max, 0.5);
  const step = safeMax <= 2 ? 0.5 : safeMax <= 5 ? 1 : 2;
  const top = Math.ceil(safeMax / step - 1e-9) * step;
  const ticks: number[] = [];
  for (let value = 0; value <= top + 1e-9; value += step) {
    ticks.push(Number(value.toFixed(2)));
  }
  return ticks;
}
