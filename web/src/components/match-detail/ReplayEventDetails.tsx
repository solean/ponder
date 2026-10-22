import type { MatchReplayFrame } from "../../lib/types";
import type { ReplayRelationshipIndex } from "../../lib/replay";
import { replayEventDetails } from "../../lib/replay/eventDetails";

export function ReplayEventDetails({ frame, relationships }: { frame: MatchReplayFrame; relationships: ReplayRelationshipIndex }) {
  const details = replayEventDetails(frame, relationships);
  if (!details.length || (details.length === 1 && ["tap", "untap", "stop_attack", "stop_block", "summoning_sickness_change"].includes(frame.changes?.[0]?.action ?? ""))) return null;
  return <details className="match-replay-event-details">
    <summary>Event details ({details.length})</summary>
    <ol>{details.map((detail, index) => <li key={index}>{detail}</li>)}</ol>
  </details>;
}
