import { useEffect, useRef } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";

import { api } from "../lib/api";
import { useEventSets } from "../lib/useEventSets";
import { EventLabel } from "./EventLabel";

/**
 * Global "now playing" banner. Polls /api/live and links to the active match.
 * Renders nothing when no match is in progress.
 */
export function LiveMatchBanner() {
  const queryClient = useQueryClient();
  const { data } = useQuery({
    queryKey: ["live"],
    queryFn: api.live,
    refetchInterval: (query) => (query.state.data?.live ? 2000 : 5000),
    refetchIntervalInBackground: false,
  });

  const live = data?.live ?? null;
  const liveMatchId = live?.match.id ?? null;

  // When a match starts or ends, nudge the rest of the app to refresh so the
  // Matches/Overview views reflect it without a manual reload.
  const previousMatchId = useRef<number | null>(null);
  useEffect(() => {
    if (previousMatchId.current !== liveMatchId) {
      previousMatchId.current = liveMatchId;
      queryClient.invalidateQueries({ queryKey: ["matches"] });
      queryClient.invalidateQueries({ queryKey: ["overview"] });
    }
  }, [liveMatchId, queryClient]);

  const { lookup } = useEventSets(live ? [live.match.eventName] : []);

  if (!live) return null;

  const { match } = live;
  const opponent = match.opponent || "Unknown";

  return (
    <Link
      className="live-banner"
      to={`/matches/${match.id}`}
      aria-label={`Open live match against ${opponent}`}
    >
      <span className="live-banner-head">
        <span className="live-badge">Live</span>
        <span className="live-vs">
          vs <strong>{opponent}</strong>
        </span>
        <EventLabel eventName={match.eventName} lookup={lookup} />
        <span className="live-progress">
          Game {Math.max(live.gameNumber, 1)} · Turn {Math.max(live.turnNumber, 0)}
        </span>
        {match.deckName ? <span className="live-deck">{match.deckName}</span> : null}
        <span className="live-open" aria-hidden="true">
          View match →
        </span>
      </span>
    </Link>
  );
}
