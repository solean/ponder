import type { MatchReplayFrame } from "../../lib/types";
import type { ReplayRelationshipIndex } from "../../lib/replay";
import { replayEventDetails } from "../../lib/replay/eventDetails";

const ROUTINE_ACTIONS = ["tap", "untap", "stop_attack", "stop_block", "summoning_sickness_change"];

/** Sub-events worth expanding for a step; empty when the row already says it all. */
export function replayExpandableDetails(frame: MatchReplayFrame, relationships: ReplayRelationshipIndex): string[] {
  const details = replayEventDetails(frame, relationships);
  if (details.length === 1 && ROUTINE_ACTIONS.includes(frame.changes?.[0]?.action ?? "")) return [];
  return details;
}

export function ReplayEventDetails({ details }: { details: string[] }) {
  return (
    <ol className="match-replay-event-details">
      {details.map((detail, index) => (
        <li key={index}>{detail}</li>
      ))}
    </ol>
  );
}
