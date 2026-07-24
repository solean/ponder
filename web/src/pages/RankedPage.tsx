import { useQuery } from "@tanstack/react-query";
import { useEffect, useMemo, useState, type KeyboardEvent } from "react";

import { ContextualLink } from "../components/Breadcrumbs";
import { RankProgressPanel } from "../components/RankProgressPanel";
import { StatusMessage } from "../components/StatusMessage";
import { api } from "../lib/api";
import { formatDuration, pct } from "../lib/format";
import {
  fillMissingRankClasses,
  LADDER_CONFIG,
  ladderMatchPoints,
  rankStateFor,
  rankStepIndex,
  type Ladder,
  type SeasonView,
} from "../lib/rankProgress";
import type { RankHistoryPoint, RankState } from "../lib/types";

const integerFormatter = new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 });
const paceFormatter = new Intl.NumberFormat(undefined, {
  minimumFractionDigits: 1,
  maximumFractionDigits: 1,
});

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
  stepsMeasured: number;
};

function summarizeMatches(matches: LadderMatch[]): RecordSummary {
  const summary: RecordSummary = {
    matches: matches.length,
    wins: 0,
    losses: 0,
    unknown: 0,
    netSteps: 0,
    stepsMeasured: 0,
  };
  for (const match of matches) {
    if (match.point.result === "win") summary.wins += 1;
    else if (match.point.result === "loss") summary.losses += 1;
    else summary.unknown += 1;
    if (match.stepDelta != null) {
      summary.netSteps += match.stepDelta;
      summary.stepsMeasured += 1;
    }
  }
  return summary;
}

function recordLabel(summary: RecordSummary): string {
  return `${summary.wins}W–${summary.losses}L`;
}

function winRateLabel(summary: RecordSummary): string {
  const decided = summary.wins + summary.losses;
  if (decided === 0) return "—";
  return pct(summary.wins / decided);
}

// Unlike the trend chart's label, this never guesses a tier: when Arena's
// responses omitted the class and no anchor exists, the label says so.
function strictRankLabel(rank: RankState): string {
  if (rank.level == null || rank.seasonOrdinal == null) return "Unranked";
  const rankClass = rank.rankClass.trim();
  if (rankClass === "Mythic") return "Mythic";
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

export function RankedPage() {
  const [ladder, setLadder] = useState<Ladder>("constructed");
  const [seasonView, setSeasonView] = useState<SeasonView>("current");
  const { data, isLoading, error } = useQuery({
    queryKey: ["rank-history"],
    queryFn: api.rankHistory,
  });
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

  useEffect(() => {
    if (seasonView === "previous" && !hasPreviousSeason) setSeasonView("current");
  }, [hasPreviousSeason, seasonView]);

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

  const summary = useMemo(() => summarizeMatches(matches), [matches]);
  const firstRankLabel = matches.length > 0 ? strictRankLabel(matches[0].rank) : null;
  const lastRankLabel = matches.length > 0 ? strictRankLabel(matches[matches.length - 1].rank) : null;

  const pace = useMemo(() => {
    let steps = 0;
    let seconds = 0;
    let timedMatches = 0;
    for (const match of matches) {
      if (match.stepDelta == null || match.point.secondsCount == null) continue;
      steps += match.stepDelta;
      seconds += match.point.secondsCount;
      timedMatches += 1;
    }
    if (timedMatches === 0 || seconds === 0) return null;
    return { stepsPerHour: (steps / seconds) * 3600, seconds, timedMatches };
  }, [matches]);

  const tierRows = useMemo(() => {
    const byTier = new Map<string, RecordSummary & { tier: string }>();
    for (const match of matches) {
      let row = byTier.get(match.tierAtPlay);
      if (!row) {
        row = { tier: match.tierAtPlay, matches: 0, wins: 0, losses: 0, unknown: 0, netSteps: 0, stepsMeasured: 0 };
        byTier.set(match.tierAtPlay, row);
      }
      row.matches += 1;
      if (match.point.result === "win") row.wins += 1;
      else if (match.point.result === "loss") row.losses += 1;
      else row.unknown += 1;
      if (match.stepDelta != null) {
        row.netSteps += match.stepDelta;
        row.stepsMeasured += 1;
      }
    }
    const tierOrder = LADDER_CONFIG[ladder].tiers;
    return [...byTier.values()].sort(
      (left, right) => tierOrder.indexOf(right.tier) - tierOrder.indexOf(left.tier),
    );
  }, [ladder, matches]);

  const deckRows = useMemo(() => {
    type DeckRow = RecordSummary & { deckId: number | null; deckName: string };
    const byDeck = new Map<string, DeckRow>();
    for (const match of matches) {
      const key = match.point.deckId != null ? `deck-${match.point.deckId}` : "unknown";
      let row = byDeck.get(key);
      if (!row) {
        row = {
          deckId: match.point.deckId,
          deckName: match.point.deckName || "Unknown deck",
          matches: 0,
          wins: 0,
          losses: 0,
          unknown: 0,
          netSteps: 0,
          stepsMeasured: 0,
        };
        byDeck.set(key, row);
      }
      row.matches += 1;
      if (match.point.result === "win") row.wins += 1;
      else if (match.point.result === "loss") row.losses += 1;
      else row.unknown += 1;
      if (match.stepDelta != null) {
        row.netSteps += match.stepDelta;
        row.stepsMeasured += 1;
      }
    }
    return [...byDeck.values()].sort(
      (left, right) => right.matches - left.matches || right.netSteps - left.netSteps,
    );
  }, [matches]);

  const seasonRows = useMemo(() => {
    type SeasonRow = RecordSummary & { season: number; endRank: string };
    const bySeason = new Map<number, SeasonRow>();
    for (const match of allMatches) {
      const ordinal = match.rank.seasonOrdinal;
      if (ordinal == null) continue;
      let row = bySeason.get(ordinal);
      if (!row) {
        row = { season: ordinal, endRank: "", matches: 0, wins: 0, losses: 0, unknown: 0, netSteps: 0, stepsMeasured: 0 };
        bySeason.set(ordinal, row);
      }
      row.matches += 1;
      row.endRank = strictRankLabel(match.rank);
      if (match.point.result === "win") row.wins += 1;
      else if (match.point.result === "loss") row.losses += 1;
      else row.unknown += 1;
      if (match.stepDelta != null) row.netSteps += match.stepDelta;
    }
    return [...bySeason.values()].sort((left, right) => right.season - left.season);
  }, [allMatches]);

  if (isLoading) return <StatusMessage>Loading ranked analytics…</StatusMessage>;
  if (error) return <StatusMessage tone="error">{(error as Error).message}</StatusMessage>;

  const seasonScopeLabel =
    selectedSeason == null ? "all seasons" : `season ${selectedSeason}`;

  return (
    <div className="stack-lg ranked-page">
      <header className="page-heading">
        <div>
          <p className="eyebrow">Ladder efficiency</p>
          <h2>Ranked</h2>
        </div>
        <div className="rank-controls" role="group" aria-label="Ranked analytics filters">
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
          <section className="metrics-grid" aria-label="Ranked efficiency summary">
            <article className="metric-card">
              <p>Ranked matches</p>
              <div className="metric-value">{integerFormatter.format(summary.matches)}</div>
              <small className="metric-sub">
                {recordLabel(summary)}
                {summary.unknown > 0 ? ` · ${summary.unknown} unknown` : ""} · {seasonScopeLabel}
              </small>
            </article>
            <article className="metric-card">
              <p>Win rate</p>
              <div className="metric-value">{winRateLabel(summary)}</div>
              <small className="metric-sub">
                {summary.wins + summary.losses} decided matches
                {summary.unknown > 0 ? `, ${summary.unknown} excluded` : ""}
              </small>
            </article>
            <article className="metric-card">
              <p>Net movement</p>
              <div className={`metric-value ${summary.netSteps > 0 ? "" : ""}`.trim()}>
                {formatSteps(summary.netSteps)} steps
              </div>
              <small className="metric-sub">
                {firstRankLabel && lastRankLabel && firstRankLabel !== lastRankLabel
                  ? `${firstRankLabel} → ${lastRankLabel}`
                  : lastRankLabel ?? ""}
                {summary.stepsMeasured < summary.matches
                  ? ` · measured over ${summary.stepsMeasured} of ${summary.matches} matches`
                  : ""}
              </small>
            </article>
            <article className="metric-card">
              <p>Climb pace</p>
              <div className="metric-value">
                {pace ? `${paceFormatter.format(pace.stepsPerHour)} steps/hr` : "—"}
              </div>
              <small className="metric-sub">
                {pace
                  ? `${formatDuration(pace.seconds)} across ${pace.timedMatches} timed matches`
                  : "no match durations captured yet"}
              </small>
            </article>
          </section>

          <section className="panel" aria-labelledby="tier-winrate-heading">
            <div className="panel-head">
              <div>
                <h3 id="tier-winrate-heading">Win rate by tier</h3>
                <p>Matches grouped by the rank tier you held going into them ({seasonScopeLabel})</p>
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
                      <td>{winRateLabel(row)}</td>
                      <td className={stepsTone(row.netSteps)}>{formatSteps(row.netSteps)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </section>

          <section className="panel" aria-labelledby="deck-impact-heading">
            <div className="panel-head">
              <div>
                <h3 id="deck-impact-heading">Rank impact by deck</h3>
                <p>Net ladder movement attributed to each deck ({seasonScopeLabel})</p>
              </div>
            </div>
            <div className="table-wrap">
              <table className="data-table compact">
                <thead>
                  <tr>
                    <th scope="col">Deck</th>
                    <th scope="col">Matches</th>
                    <th scope="col">Record</th>
                    <th scope="col">Win rate</th>
                    <th scope="col">Net steps</th>
                  </tr>
                </thead>
                <tbody>
                  {deckRows.map((row) => (
                    <tr key={row.deckId ?? "unknown"}>
                      <td>
                        {row.deckId != null ? (
                          <ContextualLink to={`/decks/${row.deckId}`}>
                            {row.deckName}
                          </ContextualLink>
                        ) : (
                          row.deckName
                        )}
                      </td>
                      <td>{integerFormatter.format(row.matches)}</td>
                      <td>
                        {recordLabel(row)}
                        {row.unknown > 0 ? ` (+${row.unknown} unknown)` : ""}
                      </td>
                      <td>{winRateLabel(row)}</td>
                      <td className={stepsTone(row.netSteps)}>{formatSteps(row.netSteps)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <p className="state">
              Steps are Arena ladder pips; movement across a season reset or within Mythic is not
              counted. Small samples say more about variance than about the deck.
            </p>
          </section>

          {seasonRows.length > 1 ? (
            <section className="panel" aria-labelledby="season-over-season-heading">
              <div className="panel-head">
                <div>
                  <h3 id="season-over-season-heading">Season over season</h3>
                  <p>Every tracked {LADDER_CONFIG[ladder].label.toLowerCase()} season</p>
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
                      <th scope="col">Final rank</th>
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
                        <td>{winRateLabel(row)}</td>
                        <td>{row.endRank}</td>
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

      <RankProgressPanel />
    </div>
  );
}
