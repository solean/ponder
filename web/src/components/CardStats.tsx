import { useMemo, type MouseEvent } from "react";
import { useQuery, type UseQueryResult } from "@tanstack/react-query";

import { api } from "../lib/api";
import { formatRelativeTime, pct } from "../lib/format";
import { SEVENTEEN_LANDS_FORMAT_LABELS, SEVENTEEN_LANDS_USER_GROUP_LABELS } from "../lib/seventeenLands";
import type { CardStat, CardStatsResponse } from "../lib/types";

// GIH WR within two points of the format average is noise-level for most cards.
const GIH_TONE_THRESHOLD = 0.02;

export type DraftCardStats = {
  query: UseQueryResult<CardStatsResponse>;
  byId: Map<number, CardStat>;
  mean: number | null;
};

/** 17Lands ratings for the draft format of one Arena event, keyed by Arena card ID. */
export function useDraftCardStats(eventName: string): DraftCardStats {
  const query = useQuery({
    queryKey: ["card-stats", eventName],
    queryFn: () => api.cardStats(eventName),
    enabled: eventName !== "",
    // The backend caches for a day; this only avoids refetching across pages.
    staleTime: 1000 * 60 * 60,
    retry: 1,
  });
  const byId = useMemo(
    () => new Map((query.data?.cards ?? []).map((card) => [card.arenaId, card])),
    [query.data],
  );
  return { query, byId, mean: query.data?.meanGihWinRate ?? null };
}

function gihTone(gih: number | null, mean: number | null): "positive" | "negative" | "neutral" {
  if (gih == null || mean == null) return "neutral";
  if (gih - mean >= GIH_TONE_THRESHOLD) return "positive";
  if (mean - gih >= GIH_TONE_THRESHOLD) return "negative";
  return "neutral";
}

function signedPoints(delta: number): string {
  const points = (delta * 100).toFixed(1);
  return delta >= 0 ? `+${points}` : points;
}

function statTooltip(stat: CardStat, mean: number | null): string {
  const lines = [
    stat.gihWinRate == null
      ? `GIH WR: not enough games (${stat.gihGameCount.toLocaleString()})`
      : `GIH WR: ${pct(stat.gihWinRate)} over ${stat.gihGameCount.toLocaleString()} games` +
        (mean == null ? "" : ` (${signedPoints(stat.gihWinRate - mean)} pts vs ${pct(mean)} format average)`),
    `GP WR: ${stat.gpWinRate == null ? "—" : pct(stat.gpWinRate)} over ${stat.gameCount.toLocaleString()} games`,
    `IWD: ${stat.iwd == null ? "—" : `${signedPoints(stat.iwd)} pts`}`,
    `ATA: ${stat.ata == null ? "—" : stat.ata.toFixed(1)} · ALSA: ${stat.alsa == null ? "—" : stat.alsa.toFixed(1)}`,
  ];
  return lines.join("\n");
}

/**
 * GIH WR / ATA readout for one card; renders nothing for unrated cards.
 * `compact` renders the bare GIH WR value for lists that label the column once.
 */
export function CardStatLine({
  cardId,
  stats,
  compact = false,
}: {
  cardId: number;
  stats: DraftCardStats;
  compact?: boolean;
}) {
  const stat = stats.byId.get(cardId);
  if (!stat) return null;
  const tone = gihTone(stat.gihWinRate, stats.mean);
  const value = (
    <strong className={`win-rate win-rate--${tone}`}>{stat.gihWinRate == null ? "—" : pct(stat.gihWinRate)}</strong>
  );
  if (compact) {
    return (
      <span className="card-stat-line" title={statTooltip(stat, stats.mean)}>
        {value}
      </span>
    );
  }
  return (
    <span className="card-stat-line" title={statTooltip(stat, stats.mean)}>
      <span className="card-stat">
        <span className="card-stat-label">GIH</span> {value}
      </span>
      <span className="card-stat">
        <span className="card-stat-label">ATA</span> {stat.ata == null ? "—" : stat.ata.toFixed(1)}
      </span>
    </span>
  );
}

function openExternal(event: MouseEvent<HTMLAnchorElement>) {
  if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
  event.preventDefault();
  const href = event.currentTarget.href;
  void api.openExternalURL(href).catch(() => {
    window.open(href, "_blank", "noopener,noreferrer");
  });
}

/** Source line shown wherever 17Lands ratings appear. */
export function CardStatsSource({ stats }: { stats: DraftCardStats }) {
  const { query } = stats;
  if (query.isPending) {
    return <p className="card-stats-source">Loading 17Lands card ratings…</p>;
  }
  if (query.isError) {
    return <p className="card-stats-source">17Lands card ratings are unavailable right now.</p>;
  }
  const data = query.data;
  if (data.status !== "ok") {
    return <p className="card-stats-source">17Lands has no card ratings for this event format.</p>;
  }
  if (data.cards.length === 0) {
    return <p className="card-stats-source">17Lands has no card ratings for {data.expansion} yet.</p>;
  }
  const format = SEVENTEEN_LANDS_FORMAT_LABELS[data.format as keyof typeof SEVENTEEN_LANDS_FORMAT_LABELS] ?? data.format;
  return (
    <p className="card-stats-source">
      Card ratings from{" "}
      <a className="text-link" href={data.sourceUrl} target="_blank" rel="noreferrer" onClick={openExternal}>
        17Lands
      </a>{" "}
      · {data.expansion} {format}
      {data.userGroup ? ` · ${SEVENTEEN_LANDS_USER_GROUP_LABELS[data.userGroup].toLowerCase()}` : ""} · updated {formatRelativeTime(data.fetchedAt ?? "")}
      {data.stale ? " (refresh failed; showing cached ratings)" : ""}
      {data.meanGihWinRate == null ? "" : ` · format GIH WR ${pct(data.meanGihWinRate)}`}
    </p>
  );
}
