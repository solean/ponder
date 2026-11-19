import { useEffect, useRef } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";

import { api } from "../lib/api";
import { formatRelativeTime } from "../lib/format";
import { parseEventName } from "../lib/events";
import type { RuntimeStatus } from "../lib/types";

function trackerSummary(status: RuntimeStatus | undefined, isError: boolean) {
  if (isError) return { state: "live tracker unknown", detail: "", dotClass: "sync-dot is-unknown" };
  if (!status) return { state: "checking status", detail: "", dotClass: "sync-dot" };

  const lastSyncedAt = [status.lastImport?.completedAt ?? "", status.liveLastTickAt ?? ""]
    .filter(Boolean)
    .sort((a, b) => new Date(a).getTime() - new Date(b).getTime())
    .pop();

  if (status.liveRunning) {
    return {
      state: "live tracker on",
      detail: lastSyncedAt ? formatRelativeTime(lastSyncedAt) : "waiting for activity",
      dotClass: "sync-dot is-live",
    };
  }
  return {
    state: "live tracker off",
    detail: lastSyncedAt ? `synced ${formatRelativeTime(lastSyncedAt)}` : "never synced",
    dotClass: "sync-dot",
  };
}

/**
 * Tracker state in the titlebar. Shows the match in progress and links to it;
 * otherwise shows whether live tracking is running and links to Settings.
 */
export function LiveStatus() {
  const queryClient = useQueryClient();
  const { data } = useQuery({
    queryKey: ["live"],
    queryFn: api.live,
    refetchInterval: (query) => (query.state.data?.live ? 2000 : 5000),
    refetchIntervalInBackground: false,
  });
  const statusQuery = useQuery({
    queryKey: ["runtime-status"],
    queryFn: api.runtimeStatus,
    refetchInterval: 30_000,
  });
  const overviewQuery = useQuery({
    queryKey: ["overview"],
    queryFn: api.overview,
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

  if (live) {
    const { match } = live;
    const opponent = match.opponent || "Unknown";
    const context = [parseEventName(match.eventName).kindLabel, match.deckName].filter(Boolean).join(" · ");
    return (
      <Link
        className="live-status is-match"
        to={`/matches/${match.id}`}
        aria-label={`Open live match against ${opponent}`}
        title={context || undefined}
      >
        <span className="sync-dot is-live" aria-hidden="true" />
        <strong className="live-status-badge">Live</strong>
        <span className="live-status-main">vs {opponent}</span>
        <span className="live-status-detail">
          Game {Math.max(live.gameNumber, 1)} · Turn {Math.max(live.turnNumber, 0)}
        </span>
        {match.deckName ? <span className="live-status-deck">{match.deckName}</span> : null}
      </Link>
    );
  }

  const tracker = trackerSummary(statusQuery.data, statusQuery.isError);
  const playerName = overviewQuery.data?.playerName;
  return (
    <Link className="live-status" to="/settings" title="Open tracking settings">
      <span className={tracker.dotClass} aria-hidden="true" />
      {playerName ? <span className="live-status-name">{playerName}</span> : null}
      <span className="live-status-main">{tracker.state}</span>
      {tracker.detail ? <span className="live-status-detail">{tracker.detail}</span> : null}
    </Link>
  );
}
