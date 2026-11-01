export type ReplayPosition = { frameId: number; speed: number };
export type ReplayKind = "frame" | "observed";
export type ReplayPositionOptions = { key: string; ids: number[] };
const speeds = [0.5, 1, 2, 4];
const positive = (value: unknown): value is number => typeof value === "number" && Number.isSafeInteger(value) && value > 0;
export function replayPositionKey(matchId: number, game: number, kind: ReplayKind): string {
  return `ponder.replay.position.${matchId}.${game}.${kind}`;
}
export function readReplayPosition(key: string): ReplayPosition | null {
  try {
    const data = JSON.parse(window.localStorage.getItem(key) ?? "null");
    return data && positive(data.frameId) && speeds.includes(data.speed) ? data : null;
  } catch { return null; }
}
export function saveReplayPosition(key: string, position: ReplayPosition): void {
  try { window.localStorage.setItem(key, JSON.stringify(position)); } catch { /* Storage can be unavailable. Transport still works. */ }
}
export function readReplayGame(matchId: number): number | null {
  try { const value = Number(window.localStorage.getItem(`ponder.replay.game.${matchId}`)); return positive(value) ? value : null; } catch { return null; }
}
export function saveReplayGame(matchId: number, game: number): void {
  try { window.localStorage.setItem(`ponder.replay.game.${matchId}`, String(game)); } catch { /* Local storage is optional. */ }
}
export function readReplayKind(matchId: number, game: number): ReplayKind | null {
  try { const value = window.localStorage.getItem(`ponder.replay.kind.${matchId}.${game}`); return value === "frame" || value === "observed" ? value : null; } catch { return null; }
}
export function saveReplayKind(matchId: number, game: number, kind: ReplayKind): void {
  try { window.localStorage.setItem(`ponder.replay.kind.${matchId}.${game}`, kind); } catch { /* Local storage is optional. */ }
}

export function clearReplayKind(matchId: number, game: number): void {
  try { window.localStorage.removeItem(`ponder.replay.kind.${matchId}.${game}`); } catch { /* Local storage is optional. */ }
}
