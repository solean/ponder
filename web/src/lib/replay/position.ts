export type ReplayPosition = { frameId: number; speed: number };
export type ReplayBookmark = { game: number; frameId: number; kind: "frame" | "observed" };
export type ReplayPositionOptions = { key: string; ids: number[]; bookmarkFrameId?: number };
const speeds = [0.5, 1, 2, 4];
const positive = (value: unknown): value is number => typeof value === "number" && Number.isSafeInteger(value) && value > 0;
export function replayPositionKey(matchId: number, game: number, kind: ReplayBookmark["kind"]): string {
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
export function parseReplayBookmark(search: string): { bookmark: ReplayBookmark | null; error: string | null } {
  const params = new URLSearchParams(search);
  if (!["replayGame", "replayFrame", "replayKind"].some(key => params.has(key))) return { bookmark: null, error: null };
  const game = Number(params.get("replayGame"));
  const frameId = Number(params.get("replayFrame"));
  const kind = params.get("replayKind");
  if (!positive(game) || !positive(frameId) || (kind !== "frame" && kind !== "observed")) return { bookmark: null, error: "This replay bookmark is invalid. Choose a game to continue." };
  return { bookmark: { game, frameId, kind }, error: null };
}
export function replayBookmarkPath(matchId: number, bookmark: ReplayBookmark): string {
  return `/matches/${matchId}?${new URLSearchParams({ replayGame: String(bookmark.game), replayFrame: String(bookmark.frameId), replayKind: bookmark.kind })}`;
}

export type SavedReplayBookmark = ReplayBookmark & { matchId: number };
export function parseReplayLink(input: string): SavedReplayBookmark | null {
  try {
    const url = new URL(input.trim(), "http://ponder.local");
    const match = /^\/matches\/([1-9]\d*)\/?$/.exec(url.pathname);
    const parsed = parseReplayBookmark(url.search);
    return match && positive(Number(match[1])) && parsed.bookmark && !parsed.error ? { matchId: Number(match[1]), ...parsed.bookmark } : null;
  } catch { return null; }
}
export function readReplayBookmarks(matchId: number): SavedReplayBookmark[] {
  try {
    const entries: unknown = JSON.parse(window.localStorage.getItem(`ponder.replay.bookmarks.${matchId}`) ?? "[]");
    if (!Array.isArray(entries)) return [];
    return entries.filter((entry): entry is SavedReplayBookmark => entry?.matchId === matchId && positive(entry.game) && positive(entry.frameId) && ["frame", "observed"].includes(entry.kind));
  } catch { return []; }
}
export function writeReplayBookmarks(matchId: number, entries: SavedReplayBookmark[]): boolean {
  try {
    window.localStorage.setItem(`ponder.replay.bookmarks.${matchId}`, JSON.stringify(entries));
    window.dispatchEvent(new window.Event("ponder-replay-bookmarks"));
    return true;
  } catch { return false; }
}

export function readReplayKind(matchId: number, game: number): ReplayBookmark["kind"] | null {
  try { const value = window.localStorage.getItem(`ponder.replay.kind.${matchId}.${game}`); return value === "frame" || value === "observed" ? value : null; } catch { return null; }
}
export function saveReplayKind(matchId: number, game: number, kind: ReplayBookmark["kind"]): void {
  try { window.localStorage.setItem(`ponder.replay.kind.${matchId}.${game}`, kind); } catch { /* Local storage is optional. */ }
}

export function clearReplayKind(matchId: number, game: number): void {
  try { window.localStorage.removeItem(`ponder.replay.kind.${matchId}.${game}`); } catch { /* Local storage is optional. */ }
}
