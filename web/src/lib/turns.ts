/**
 * Convert Arena's per-player turn number into the full-turn number shown to users.
 * Arena turns 1 and 2 make up full turn 1, turns 3 and 4 make up full turn 2,
 * and so on. Pre-game and sentinel values are preserved.
 */
export function arenaTurnToFullTurn(arenaTurnNumber: number): number {
  return arenaTurnNumber > 0 ? Math.ceil(arenaTurnNumber / 2) : arenaTurnNumber;
}
