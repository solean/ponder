/**
 * Hypergeometric draw-odds helpers for the live banner.
 *
 * These are deliberately a best-effort *estimate*: the MTGA log never exposes
 * your hand, so we can't know how many copies of a card are truly left in the
 * library. We compute against the full copy count and an estimated library size
 * (deck − opening hand − turns elapsed). Good enough to feel useful, labeled as
 * an estimate in the UI.
 */

/** Probability the very next draw is a given card: copies / library. */
export function pNextDraw(copies: number, library: number): number {
  if (library <= 0 || copies <= 0) return 0;
  return Math.min(1, copies / library);
}

/**
 * Probability of drawing at least one copy within the next `draws` cards:
 * 1 − (ways to miss every draw) / (ways to draw any cards)
 *   = 1 − Π_{i=0..draws-1} (library − copies − i) / (library − i)
 */
export function pWithin(copies: number, library: number, draws: number): number {
  if (library <= 0 || copies <= 0 || draws <= 0) return 0;
  if (copies >= library) return 1;
  const effectiveDraws = Math.min(draws, library);

  let pMiss = 1;
  for (let i = 0; i < effectiveDraws; i += 1) {
    const remaining = library - i;
    const misses = remaining - copies;
    if (misses <= 0) return 1;
    pMiss *= misses / remaining;
  }
  return 1 - pMiss;
}

/** Formats a 0–1 probability as a whole-number percentage, e.g. 0.43 → "43%". */
export function formatOdds(probability: number): string {
  return `${Math.round(probability * 100)}%`;
}
