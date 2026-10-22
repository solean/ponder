import type { MatchReplayFrameObject } from "../types";

/** Uses only the selected snapshot, never a relationship index of later frames. */
export function replayHandIdentityKnown(object: MatchReplayFrameObject): boolean {
  const visibility = object.visibility?.replace(/^Visibility_/, "").toLowerCase();
  return object.cardId > 0 &&
    (object.playerSide === "self" || visibility === "public" || visibility === "known");
}
