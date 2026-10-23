import type { GameAnalytics, MatchCardPlay, MatchReplayFrame } from "../types";

export type ReplayRecordingState = "complete" | "partial" | "recording" | "waiting" | "missing" | "fallback";
export type ReplayRecordingCoverage = { state: ReplayRecordingState; label: string; detail: string };

const stage = (frame: MatchReplayFrame) => (frame.gameStage ?? "").toLowerCase().replace(/^gamestage_/, "");
const gameOf = (value: { gameNumber?: number }) => value.gameNumber && value.gameNumber > 0 ? value.gameNumber : 1;

export function replayGameNumbers(frames: MatchReplayFrame[], plays: MatchCardPlay[], games: Pick<GameAnalytics, "gameNumber">[] = []): number[] {
  return [...new Set([...frames.map(gameOf), ...plays.map(gameOf), ...games.map(gameOf)])].sort((a, b) => a - b);
}

/** Raw frames are essential: presentation filtering removes start/end evidence. */
export function replayRecordingCoverage({ frames, hasFallback, matchComplete, gameEnded, isLatestGame, now = Date.now() }: {
  frames: MatchReplayFrame[];
  hasFallback: boolean;
  matchComplete?: boolean;
  gameEnded?: boolean;
  isLatestGame: boolean;
  now?: number;
}): ReplayRecordingCoverage {
  if (!frames.length) {
    if (!hasFallback && matchComplete === false && !gameEnded && isLatestGame) {
      return { state: "waiting", label: "Waiting for recording", detail: "No game states have been saved. The saved match is still open, so Ponder will check automatically for incoming Arena logs. This does not confirm that recording is active. If this is an older game, missing events require original Arena logs that still contain them." };
    }
    return hasFallback
      ? { state: "fallback", label: "First-sighting fallback", detail: "Only first public card sightings were saved. This is not a full replay: later moves, hidden cards and the live stack cannot be reconstructed. Missing events can only be recovered from original Arena logs that still contain them." }
      : { state: "missing", label: "No recording", detail: "No replay frames or public card sightings were saved for this game. Refreshing cannot recreate missing events. If the original Arena logs still exist, import them to recover any recorded events." };
  }
  const first = frames[0]!;
  const last = frames[frames.length - 1]!;
  // State IDs restart per game. A full snapshot in the middle of a game is not
  // start evidence; inherited stages/previous IDs must not certify coverage.
  const hasStart = first.gameStateId === 1 && !first.prevGameStateId && stage(first) === "start" &&
    (first.gameStateType ?? "").toLowerCase().replace(/^gamestatetype_/, "") === "full" &&
    (!first.turnNumber || first.turnNumber <= 1);
  const hasEnd = stage(last) === "gameover";
  const linked = frames.every((frame, index) => {
    if (!Number.isInteger(frame.gameStateId) || frame.gameStateId! <= 0) return false;
    if (!index) return true;
    const previous = frames[index - 1]!;
    return frame.gameStateId! > previous.gameStateId! && frame.prevGameStateId === previous.gameStateId;
  });
  if (hasStart && hasEnd && linked && frames.some(frame => stage(frame) === "play")) {
    return { state: "complete", label: "Complete recording", detail: "The game start and end were captured with a linked sequence of game states. Hidden information remains limited to what Arena recorded." };
  }
  const recordedAt = Date.parse(last.recordedAt ?? "");
  const recent = Number.isFinite(recordedAt) && now - recordedAt >= 0 && now - recordedAt < 120_000;
  if (matchComplete === false && !gameEnded && !hasEnd && isLatestGame && recent) {
    return { state: "recording", label: "Recording in progress", detail: "Recent game states are arriving. This replay updates automatically while Arena logs are collected; recording completeness is not verified yet." };
  }
  const limitations = [!hasStart ? "the game start is not verified" : "", !hasEnd ? "the game end is not captured" : "", !linked ? "some state links are missing or inconsistent" : ""].filter(Boolean);
  return { state: "partial", label: "Partial recording", detail: `Some events may be absent: ${limitations.join("; ") || "the full game sequence is not verified"}. Only saved states are shown. A finished match does not guarantee a complete recording. Import original Arena logs if they still contain the missing events.` };
}
