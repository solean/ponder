import type { Match } from "./types";

/**
 * The one-line count/record header above the match table. It exists as its own
 * module because the count is easy to get subtly wrong: the record is tallied
 * over the rows actually fetched, so whenever the fetch is capped below the
 * server's total the header has to say so instead of passing the limit off as
 * the user's lifetime count.
 */

export type MatchRecord = {
  wins: number;
  losses: number;
  unknown: number;
};

export function tallyRecord(matches: Match[]): MatchRecord {
  const record: MatchRecord = { wins: 0, losses: 0, unknown: 0 };
  for (const match of matches) {
    if (match.result === "win") record.wins += 1;
    else if (match.result === "loss") record.losses += 1;
    else record.unknown += 1;
  }
  return record;
}

export function formatRecord(record: MatchRecord): string {
  return `${record.wins}-${record.losses}`;
}

export function formatWinRate(record: MatchRecord): string {
  const decided = record.wins + record.losses;
  if (decided === 0) return "-";
  return `${Math.round((record.wins / decided) * 100)}%`;
}

export function matchListSummary({
  shown,
  fetched,
  total,
  filtersActive,
  record,
}: {
  shown: number;
  fetched: number;
  total: number;
  filtersActive: boolean;
  record: MatchRecord;
}): string {
  const truncated = fetched < total;
  const scope = filtersActive
    ? truncated
      ? `${shown} of ${fetched} shown (${total.toLocaleString()} total)`
      : `${shown} of ${fetched} matches`
    : truncated
      ? `showing ${fetched} of ${total.toLocaleString()} matches`
      : `${fetched} matches`;
  return `${scope} • ${formatRecord(record)} (${formatWinRate(record)})`;
}
