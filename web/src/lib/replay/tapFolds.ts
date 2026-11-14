import type { MatchReplayFrame } from "../types";
import { replayChangeIsCast } from "./index";

export type ReplayTapFold = {
  /** Index of the cast frame whose play-by-play row absorbs this tap frame. */
  hostIndex: number;
  /** Permanents tapped in the folded frame. */
  count: number;
};

function tapOnlyCount(frame: MatchReplayFrame): number {
  const changes = frame.changes ?? [];
  return changes.length > 0 && changes.every((change) => change.action === "tap") ? changes.length : 0;
}

function castsFor(frame: MatchReplayFrame | undefined, side: string | undefined, turnNumber: number | undefined): boolean {
  if (!frame || frame.turnNumber !== turnNumber) return false;
  return (frame.changes ?? []).some((change) => change.playerSide === side && replayChangeIsCast(change));
}

/**
 * Frames that only tap permanents to pay for an adjacent cast by the same
 * player. The play-by-play shows them on the cast's row ("… (tapping 2)")
 * instead of as their own step; the frames themselves stay in the replay.
 */
export function replayTapFolds(frames: MatchReplayFrame[]): Map<number, ReplayTapFold> {
  const folds = new Map<number, ReplayTapFold>();
  frames.forEach((frame, index) => {
    const count = tapOnlyCount(frame);
    if (count === 0) return;
    const side = frame.changes?.[0]?.playerSide;
    if (castsFor(frames[index - 1], side, frame.turnNumber)) {
      folds.set(index, { hostIndex: index - 1, count });
    } else if (castsFor(frames[index + 1], side, frame.turnNumber)) {
      folds.set(index, { hostIndex: index + 1, count });
    }
  });
  return folds;
}
