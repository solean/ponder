import { type InspectableZoneKind } from "../../lib/replay";
import type { MatchCardPlay, MatchReplayFrameObject } from "../../lib/types";

export type MatchReplayZoneDialogState =
  | {
      source: "replay";
      side: "self" | "opponent";
      zone: InspectableZoneKind;
      objects: MatchReplayFrameObject[];
    }
  | {
      source: "observed";
      side: "self" | "opponent";
      zone: InspectableZoneKind;
      plays: MatchCardPlay[];
    };
