import { useQuery } from "@tanstack/react-query";
import { useEffect, useMemo, type KeyboardEvent } from "react";
import { useSearchParams } from "react-router-dom";

import { StatusMessage } from "../components/StatusMessage";
import { api } from "../lib/api";
import { pct, winRateTone } from "../lib/format";
import {
  formatRankLabel,
  fillMissingRankClasses,
  LADDER_CONFIG,
  ladderMatchPoints,
  preferredLadder,
  rankStateFor,
  rankStepIndex,
  type Ladder,
  type SeasonView,
} from "../lib/rankProgress";
import type { RankHistoryPoint, RankState } from "../lib/types";

const integerFormatter = new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 });

type LadderMatch = {
  point: RankHistoryPoint;
  rank: RankState;
  /** Step movement this match caused; null across season boundaries or in Mythic. */
  stepDelta: number | null;
  /** Rank tier the match was played at (the standing before the match). */
  tierAtPlay: string;
};

function buildLadderMatches(history: RankHistoryPoint[], ladder: Ladder): LadderMatch[] {
  // Arena often omits the rank-class string; anchor missing classes to the
  // explicit ones within each season before computing tier analytics.
  const points = ladderMatchPoints(fillMissingRankClasses(history), ladder);
  const out: LadderMatch[] = [];
  for (let index = 0; index < points.length; index += 1) {
    const point = points[index];
    const rank = rankStateFor(point, ladder);
    if (rank.seasonOrdinal == null) continue;

    const prevRank = index > 0 ? rankStateFor(points[index - 1], ladder) : null;
    const sameSeason = prevRank?.seasonOrdinal === rank.seasonOrdinal;
    const stepIndex = rankStepIndex(rank, ladder);
    const prevStepIndex = sameSeason && prevRank ? rankStepIndex(prevRank, ladder) : null;
    const tierClass = (sameSeason && prevRank ? prevRank : rank).rankClass.trim();
    out.push({
      point,
      rank,
      stepDelta: stepIndex != null && prevStepIndex != null ? stepIndex - prevStepIndex : null,
      tierAtPlay: tierClass || "Unknown",
    });
  }
  return out;
}

type RecordSummary = {
  matches: number;
  wins: number;
  losses: number;
  unknown: number;
  netSteps: number;
};

function recordLabel(summary: RecordSummary): string {
  return `${summary.wins}W–${summary.losses}L`;
}

function winRateValue(summary: RecordSummary): number | null {
  const decided = summary.wins + summary.losses;
  return decided === 0 ? null : summary.wins / decided;
}

function winRateLabel(summary: RecordSummary): string {
  const rate = winRateValue(summary);
  return rate == null ? "—" : pct(rate);
}

function WinRateCell({ summary }: { summary: RecordSummary }) {
  const rate = winRateValue(summary);
  return (
    <td>
      <strong className={`win-rate win-rate--${winRateTone(rate)}`}>{winRateLabel(summary)}</strong>
    </td>
  );
}

// Never guess a tier when Arena omitted the class and no anchor exists.
function strictRankLabel(rank: RankState): string {
  if (rank.level == null || rank.seasonOrdinal == null) return "Unranked";
  const rankClass = rank.rankClass.trim();
  if (rankClass === "Mythic" || rank.level === 0) return formatRankLabel(rank);
  if (!rankClass) return `Level ${rank.level} (tier unknown)`;
  return `${rankClass} ${rank.level}`;
}

function formatSteps(value: number): string {
  if (value === 0) return "±0";
  return `${value > 0 ? "+" : "−"}${integerFormatter.format(Math.abs(value))}`;
}

function stepsTone(value: number): string {
  if (value > 0) return "economy-delta economy-delta--positive";
  if (value < 0) return "economy-delta economy-delta--negative";
  return "economy-delta";
}

function handleSegmentedKeyDown<T extends string>(
  event: KeyboardEvent<HTMLButtonElement>,
  value: T,
  options: readonly T[],
  onChange: (next: T) => void,
) {
  const currentIndex = options.indexOf(value);
  if (currentIndex === -1) return;
  switch (event.key) {
    case "ArrowLeft":
    case "ArrowUp":
      event.preventDefault();
      onChange(options[(currentIndex + options.length - 1) % options.length]);
      break;
    case "ArrowRight":
    case "ArrowDown":
      event.preventDefault();
      onChange(options[(currentIndex + 1) % options.length]);
      break;
    case "Home":
      event.preventDefault();
      onChange(options[0]);
      break;
    case "End":
      event.preventDefault();
      onChange(options[options.length - 1]);
      break;
    default:
      break;
  }
}

export function SeasonDetailsPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const requestedLadder = searchParams.get("ladder");
  const requestedSeason = searchParams.get("season");
  const { data, isLoading, error } = useQuery({
    queryKey: ["rank-history"],
    queryFn: api.rankHistory,
  });
  const ladder: Ladder =
    requestedLadder === "constructed" || requestedLadder === "limited"
      ? requestedLadder
      : data ? preferredLadder(data) : "constructed";
  const ladderOptions = ["constructed", "limited"] as const satisfies readonly Ladder[];

  const allMatches = useMemo(() => (data ? buildLadderMatches(data, ladder) : []), [data, ladder]);
  const seasonOrdinals = useMemo(() => {
    const ordinals: number[] = [];
    for (const match of allMatches) {
      const ordinal = match.rank.seasonOrdinal;
      if (ordinal != null && ordinals[ordinals.length - 1] !== ordinal) ordinals.push(ordinal);
    }
    return ordinals;
  }, [allMatches]);
  const hasPreviousSeason = seasonOrdinals.length > 1;
  const currentSeasonOrdinal = seasonOrdinals[seasonOrdinals.length - 1];
  const previousSeasonOrdinal = seasonOrdinals[seasonOrdinals.length - 2];

  const seasonView: SeasonView =
    requestedSeason === "all"
      ? "all"
      : requestedSeason === "previous" && hasPreviousSeason ? "previous" : "current";

  useEffect(() => {
    if (!data || (requestedLadder === ladder && requestedSeason === seasonView)) return;
    setSearchParams((params) => {
      const next = new URLSearchParams(params);
      next.set("ladder", ladder);
      next.set("season", seasonView);
      return next;
    }, { replace: true });
  }, [data, ladder, requestedLadder, requestedSeason, seasonView, setSearchParams]);

  function setLadder(value: Ladder) {
    setSearchParams((params) => {
      const next = new URLSearchParams(params);
      next.set("ladder", value);
      next.set("season", seasonView);
      return next;
    }, { replace: true });
  }

  function setSeasonView(value: SeasonView) {
    setSearchParams((params) => {
      const next = new URLSearchParams(params);
      next.set("ladder", ladder);
      next.set("season", value);
      return next;
    }, { replace: true });
  }

  const selectedSeason =
    seasonView === "all"
      ? null
      : seasonView === "previous"
        ? previousSeasonOrdinal ?? null
        : currentSeasonOrdinal ?? null;

  const matches = useMemo(
    () =>
      selectedSeason == null
        ? allMatches
        : allMatches.filter((match) => match.rank.seasonOrdinal === selectedSeason),
    [allMatches, selectedSeason],
  );

  const tierRows = useMemo(() => {
    const byTier = new Map<string, RecordSummary & { tier: string }>();
    for (const match of matches) {
      let row = byTier.get(match.tierAtPlay);
      if (!row) {
        row = { tier: match.tierAtPlay, matches: 0, wins: 0, losses: 0, unknown: 0, netSteps: 0 };
        byTier.set(match.tierAtPlay, row);
      }
      row.matches += 1;
      if (match.point.result === "win") row.wins += 1;
      else if (match.point.result === "loss") row.losses += 1;
      else row.unknown += 1;
      if (match.stepDelta != null) {
        row.netSteps += match.stepDelta;
      }
    }
    const tierOrder = LADDER_CONFIG[ladder].tiers;
    return [...byTier.values()].sort(
      (left, right) => tierOrder.indexOf(right.tier) - tierOrder.indexOf(left.tier),
    );
  }, [ladder, matches]);

  const seasonRows = useMemo(() => {
    type SeasonRow = RecordSummary & { season: number; lastRecordedRank: string };
    const bySeason = new Map<number, SeasonRow>();
    for (const match of allMatches) {
      const ordinal = match.rank.seasonOrdinal;
      if (ordinal == null) continue;
      let row = bySeason.get(ordinal);
      if (!row) {
        row = { season: ordinal, lastRecordedRank: "", matches: 0, wins: 0, losses: 0, unknown: 0, netSteps: 0 };
        bySeason.set(ordinal, row);
      }
      row.matches += 1;
      row.lastRecordedRank = strictRankLabel(match.rank);
      if (match.point.result === "win") row.wins += 1;
      else if (match.point.result === "loss") row.losses += 1;
      else row.unknown += 1;
      if (match.stepDelta != null) row.netSteps += match.stepDelta;
    }
    return [...bySeason.values()].sort((left, right) => right.season - left.season);
  }, [allMatches]);

  if (isLoading) return <StatusMessage>Loading season details…</StatusMessage>;
  if (error) return <StatusMessage tone="error">{(error as Error).message}</StatusMessage>;

  const seasonScopeLabel =
    selectedSeason == null ? "all seasons" : `season ${selectedSeason}`;

  return (
    <div className="stack-lg season-details-page">
      <header className="page-heading">
        <div>
          <p className="eyebrow">{LADDER_CONFIG[ladder].label} · {seasonScopeLabel}</p>
          <h2>Season details</h2>
        </div>
        <div className="rank-controls" role="group" aria-label="Season details filters">
          <div className="tabs rank-toggle" role="group" aria-label="Ladder">
            {ladderOptions.map((value) => (
              <button
                key={value}
                type="button"
                aria-pressed={ladder === value}
                className={`tab rank-toggle-button ${ladder === value ? "is-active" : ""}`}
                onClick={() => setLadder(value)}
                onKeyDown={(event) => handleSegmentedKeyDown(event, value, ladderOptions, setLadder)}
              >
                {LADDER_CONFIG[value].label}
              </button>
            ))}
          </div>
          <label className="rank-season-select">
            <span>Season</span>
            <select
              value={seasonView}
              onChange={(event) => setSeasonView(event.target.value as SeasonView)}
            >
              <option value="all">All seasons</option>
              <option value="current">
                {currentSeasonOrdinal == null ? "Current season" : `Season ${currentSeasonOrdinal}`}
              </option>
              {hasPreviousSeason ? (
                <option value="previous">Season {previousSeasonOrdinal}</option>
              ) : null}
            </select>
          </label>
        </div>
      </header>

      {matches.length === 0 ? (
        <section className="panel empty-panel">
          <h3>No ranked matches tracked for this selection</h3>
          <p>
            Rank snapshots are captured after each ranked match. Play a{" "}
            {LADDER_CONFIG[ladder].label.toLowerCase()} ladder match with tracking running and this
            page will populate.
          </p>
        </section>
      ) : (
        <>
          <section className="panel" aria-labelledby="tier-winrate-heading">
            <div className="panel-head">
              <div>
                <h3 id="tier-winrate-heading">Win rate by tier</h3>
                <p>Tracked matches grouped by the rank tier you held going into them ({seasonScopeLabel})</p>
              </div>
            </div>
            <div className="table-wrap">
              <table className="data-table compact">
                <thead>
                  <tr>
                    <th scope="col">Tier</th>
                    <th scope="col">Matches</th>
                    <th scope="col">Record</th>
                    <th scope="col">Win rate</th>
                    <th scope="col">Net steps</th>
                  </tr>
                </thead>
                <tbody>
                  {tierRows.map((row) => (
                    <tr key={row.tier}>
                      <td>{row.tier}</td>
                      <td>{integerFormatter.format(row.matches)}</td>
                      <td>
                        {recordLabel(row)}
                        {row.unknown > 0 ? ` (+${row.unknown} unknown)` : ""}
                      </td>
                      <WinRateCell summary={row} />
                      <td className={stepsTone(row.netSteps)}>{formatSteps(row.netSteps)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <p className="state">
              Steps are Arena ladder pips; movement across a season reset or within Mythic is not
              counted. Records include tracked matches only, not necessarily the full season.
            </p>
          </section>

          {seasonRows.length > 1 ? (
            <section className="panel" aria-labelledby="season-over-season-heading">
              <div className="panel-head">
                <div>
                  <h3 id="season-over-season-heading">Season history</h3>
                  <p>Tracked matches from every {LADDER_CONFIG[ladder].label.toLowerCase()} season</p>
                </div>
              </div>
              <div className="table-wrap">
                <table className="data-table compact">
                  <thead>
                    <tr>
                      <th scope="col">Season</th>
                      <th scope="col">Matches</th>
                      <th scope="col">Record</th>
                      <th scope="col">Win rate</th>
                      <th scope="col">Last recorded rank</th>
                      <th scope="col">Net steps</th>
                    </tr>
                  </thead>
                  <tbody>
                    {seasonRows.map((row) => (
                      <tr key={row.season}>
                        <td>Season {row.season}</td>
                        <td>{integerFormatter.format(row.matches)}</td>
                        <td>
                          {recordLabel(row)}
                          {row.unknown > 0 ? ` (+${row.unknown} unknown)` : ""}
                        </td>
                        <WinRateCell summary={row} />
                        <td>{row.lastRecordedRank}</td>
                        <td className={stepsTone(row.netSteps)}>{formatSteps(row.netSteps)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </section>
          ) : null}
        </>
      )}
    </div>
  );
}
