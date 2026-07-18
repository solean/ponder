import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { keepPreviousData, useQuery } from "@tanstack/react-query";

import { CardPreviewName } from "./CardPreviewName";
import { EventLabel } from "./EventLabel";
import { ResultPill } from "./ResultPill";
import { StatusMessage } from "./StatusMessage";
import { api } from "../lib/api";
import { formatDateTime, pct } from "../lib/format";
import { useEventSets } from "../lib/useEventSets";
import type {
  AnalyticsBucket,
  DeckAnalyticsCardFacet,
  DeckAnalyticsGamesParams,
  DeckCardPerformance,
  DeckVersion,
  RecordAgg,
} from "../lib/types";

/** Below this many known-result games a rate is flagged as a small sample. */
const MIN_SAMPLE_GAMES = 5;
/** A rate must clear the baseline by this margin before it is colored. */
const BASELINE_MARGIN = 0.05;

type DrillDown = {
  label: string;
  params: DeckAnalyticsGamesParams;
};

type CardSortKey =
  | "name"
  | "seen"
  | "inHand"
  | "opening"
  | "drawn"
  | "played"
  | "notPlayed"
  | "stranded"
  | "mulled"
  | "avgPlayedTurn";

function winRate(record: RecordAgg): number | null {
  return record.games > 0 ? record.wins / record.games : null;
}

function recordLabel(record: RecordAgg): string {
  const base = `${record.wins}–${record.losses}`;
  return record.draws > 0 ? `${base}–${record.draws}` : base;
}

function rateTone(record: RecordAgg, baseline: number | null): string {
  const rate = winRate(record);
  if (rate == null) {
    return "is-empty";
  }
  if (record.games < MIN_SAMPLE_GAMES) {
    return "is-small-sample";
  }
  if (baseline != null && rate >= baseline + BASELINE_MARGIN) {
    return "is-above";
  }
  if (baseline != null && rate <= baseline - BASELINE_MARGIN) {
    return "is-below";
  }
  return "is-neutral";
}

function cardSortValue(card: DeckCardPerformance, key: CardSortKey): number {
  switch (key) {
    case "seen":
      return card.gamesSeen;
    case "inHand":
      return winRate(card.inHand) ?? -1;
    case "opening":
      return winRate(card.openingHand) ?? -1;
    case "drawn":
      return winRate(card.drawn) ?? -1;
    case "played":
      return winRate(card.played) ?? -1;
    case "notPlayed":
      return winRate(card.notPlayed) ?? -1;
    case "stranded":
      return card.endedInHandGames;
    case "mulled":
      return card.mulliganCopies;
    case "avgPlayedTurn":
      return card.avgFirstPlayedTurn ?? Number.POSITIVE_INFINITY;
    default:
      return 0;
  }
}

function StatCell({
  record,
  baseline,
  onDrill,
  drillLabel,
}: {
  record: RecordAgg;
  baseline: number | null;
  onDrill?: () => void;
  drillLabel?: string;
}) {
  const rate = winRate(record);
  const tone = rateTone(record, baseline);
  const body = (
    <>
      <strong>{rate == null ? "—" : pct(rate)}</strong>
      <span>
        {record.wins}/{record.games}
      </span>
    </>
  );
  if (!onDrill || record.games === 0) {
    return <span className={`deck-analytics-stat ${tone}`}>{body}</span>;
  }
  return (
    <button
      type="button"
      className={`deck-analytics-stat is-drillable ${tone}`}
      onClick={onDrill}
      title={drillLabel ? `Show games: ${drillLabel}` : undefined}
    >
      {body}
    </button>
  );
}

function RecordTile({
  label,
  record,
  detail,
  onDrill,
}: {
  label: string;
  record: RecordAgg;
  detail?: string;
  onDrill?: () => void;
}) {
  const rate = winRate(record);
  const inner = (
    <>
      <dt>{label}</dt>
      <dd>
        <strong>{recordLabel(record)}</strong>
        <span>{rate == null ? "no games" : pct(rate)}</span>
        {detail ? <small>{detail}</small> : null}
      </dd>
    </>
  );
  if (!onDrill || record.games === 0) {
    return <div className="deck-analytics-tile">{inner}</div>;
  }
  return (
    <div className="deck-analytics-tile is-drillable" role="button" tabIndex={0}
      onClick={onDrill}
      onKeyDown={(event) => {
        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          onDrill();
        }
      }}
      title={`Show the games behind ${label.toLowerCase()}`}
    >
      {inner}
    </div>
  );
}

function BucketTable({
  title,
  buckets,
  formatKey,
  baseline,
  emptyMessage,
  footnote,
  onDrill,
}: {
  title: string;
  buckets: AnalyticsBucket[];
  formatKey: (key: number) => string;
  baseline: number | null;
  emptyMessage: string;
  footnote?: string;
  onDrill?: (bucket: AnalyticsBucket) => void;
}) {
  return (
    <article className="deck-analytics-bucket-card">
      <h4>{title}</h4>
      {buckets.length === 0 ? (
        <p className="deck-analytics-empty">{emptyMessage}</p>
      ) : (
        <table className="deck-analytics-bucket-table">
          <thead>
            <tr>
              <th scope="col">{title === "Mulligans" ? "Taken" : "Count"}</th>
              <th scope="col">Games</th>
              <th scope="col">Record</th>
              <th scope="col">Win rate</th>
            </tr>
          </thead>
          <tbody>
            {buckets.map((bucket) => {
              return (
                <tr key={bucket.key}>
                  <th scope="row">{formatKey(bucket.key)}</th>
                  <td>
                    {bucket.record.games + bucket.unknownResults}
                    {bucket.unknownResults > 0 ? <small> ({bucket.unknownResults} unknown)</small> : null}
                  </td>
                  <td>{recordLabel(bucket.record)}</td>
                  <td>
                    <StatCell
                      record={bucket.record}
                      baseline={baseline}
                      onDrill={onDrill ? () => onDrill(bucket) : undefined}
                      drillLabel={`${title.toLowerCase()} ${formatKey(bucket.key)}`}
                    />
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
      {footnote ? <p className="deck-analytics-footnote">{footnote}</p> : null}
    </article>
  );
}

function DrillDownSection({
  deckId,
  versionId,
  drillDown,
  onClose,
}: {
  deckId: number;
  versionId: number;
  drillDown: DrillDown;
  onClose: () => void;
}) {
  const params = useMemo<DeckAnalyticsGamesParams>(() => {
    return versionId > 0 ? { ...drillDown.params, version: versionId } : drillDown.params;
  }, [drillDown.params, versionId]);

  const query = useQuery({
    queryKey: ["deck-analytics-games", deckId, params],
    queryFn: () => api.deckAnalyticsGames(deckId, params),
    placeholderData: keepPreviousData,
  });
  const rows = query.data ?? [];
  const { lookup: setLookup } = useEventSets(rows.map((row) => row.eventName));

  return (
    <div className="deck-analytics-drilldown" aria-live="polite">
      <div className="deck-analytics-drilldown-head">
        <h4>{drillDown.label}</h4>
        <button type="button" className="text-link" onClick={onClose}>
          Close
        </button>
      </div>
      {query.isLoading ? (
        <StatusMessage>Loading games…</StatusMessage>
      ) : query.error ? (
        <StatusMessage tone="error">{(query.error as Error).message}</StatusMessage>
      ) : rows.length === 0 ? (
        <StatusMessage>No games match this statistic.</StatusMessage>
      ) : (
        <div className="table-wrap">
          <table className="data-table deck-analytics-drilldown-table">
            <thead>
              <tr>
                <th>Started</th>
                <th>Event</th>
                <th>Opponent</th>
                <th>Game</th>
                <th>Result</th>
                <th>Initiative</th>
                <th>Hand</th>
                {params.card ? <th>Copies</th> : null}
                <th>Details</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <tr key={`${row.matchId}-${row.gameNumber}`}>
                  <td>{row.startedAt ? formatDateTime(row.startedAt) : "—"}</td>
                  <td>
                    <EventLabel eventName={row.eventName ?? ""} lookup={setLookup} />
                  </td>
                  <td>{row.opponent || "—"}</td>
                  <td>Game {row.gameNumber}</td>
                  <td>
                    <ResultPill result={row.result} />
                  </td>
                  <td>{row.playDraw ? `On the ${row.playDraw}` : "—"}</td>
                  <td>
                    {row.keptHandSize != null
                      ? `${row.keptHandSize} kept${row.mulliganCount ? ` · ${row.mulliganCount} mull` : ""}`
                      : "—"}
                  </td>
                  {params.card ? (
                    <td>
                      {[
                        row.openingKeptCopies ? `${row.openingKeptCopies} kept` : "",
                        row.drawnCopies ? `${row.drawnCopies} drawn` : "",
                        row.playedCopies
                          ? `${row.playedCopies} played${row.firstPlayedTurn != null ? ` (t${row.firstPlayedTurn})` : ""}`
                          : "",
                        row.endInHandCopies ? `${row.endInHandCopies} stranded` : "",
                      ]
                        .filter(Boolean)
                        .join(" · ") || "—"}
                    </td>
                  ) : null}
                  <td>
                    <Link className="text-link" to={`/matches/${row.matchId}`}>
                      View match
                    </Link>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

export function DeckAnalyticsPanel({ deckId, versions }: { deckId: number; versions: DeckVersion[] }) {
  const [versionId, setVersionId] = useState(0);
  const [sortKey, setSortKey] = useState<CardSortKey>("seen");
  const [sortAscending, setSortAscending] = useState(false);
  const [drillDown, setDrillDown] = useState<DrillDown | null>(null);

  const query = useQuery({
    queryKey: ["deck-analytics", deckId, versionId],
    queryFn: () => api.deckAnalytics(deckId, versionId || undefined),
    enabled: Number.isFinite(deckId),
    placeholderData: keepPreviousData,
  });
  const data = query.data;
  const baseline = data ? winRate(data.gameRecord) : null;

  const sortedCards = useMemo(() => {
    const cards = [...(data?.cards ?? [])];
    cards.sort((a, b) => {
      if (sortKey === "name") {
        const byName = (a.cardName ?? "").localeCompare(b.cardName ?? "", undefined, { sensitivity: "base" });
        return sortAscending ? byName : -byName;
      }
      const delta = cardSortValue(a, sortKey) - cardSortValue(b, sortKey);
      if (delta !== 0) {
        return sortAscending ? delta : -delta;
      }
      return b.gamesSeen - a.gamesSeen || a.cardId - b.cardId;
    });
    return cards;
  }, [data?.cards, sortKey, sortAscending]);

  const toggleSort = (key: CardSortKey) => {
    if (key === sortKey) {
      setSortAscending((current) => !current);
      return;
    }
    setSortKey(key);
    setSortAscending(key === "name" || key === "avgPlayedTurn");
  };

  const drillCard = (card: DeckCardPerformance, facet: DeckAnalyticsCardFacet, facetLabel: string) => {
    const name = card.cardName?.trim() || `Card ${card.cardId}`;
    setDrillDown({
      label: `${name} — ${facetLabel}`,
      params: { card: card.cardId, facet },
    });
  };

  if (query.isLoading) {
    return (
      <section className="panel deck-analytics-panel">
        <div className="panel-head">
          <h3>Performance Analytics</h3>
        </div>
        <StatusMessage>Deriving deck analytics…</StatusMessage>
      </section>
    );
  }
  if (query.error) {
    return (
      <section className="panel deck-analytics-panel">
        <div className="panel-head">
          <h3>Performance Analytics</h3>
        </div>
        <StatusMessage tone="error">{(query.error as Error).message}</StatusMessage>
      </section>
    );
  }
  if (!data) {
    return null;
  }

  const coverage = data.coverage;
  const coverageState =
    coverage.gameCount > 0 && coverage.gamesWithCardStats === coverage.gameCount && coverage.gamesWithResult === coverage.gameCount
      ? "complete"
      : coverage.gamesWithCardStats > 0
        ? "partial"
        : "unknown";

  const sortIndicator = (key: CardSortKey) => (sortKey === key ? (sortAscending ? " ↑" : " ↓") : "");
  const headerSort = (key: CardSortKey): "ascending" | "descending" | "none" =>
    sortKey === key ? (sortAscending ? "ascending" : "descending") : "none";

  return (
    <section className="panel deck-analytics-panel">
      <div className="panel-head deck-analytics-head">
        <div>
          <h3>Performance Analytics</h3>
          <p>
            {coverage.gameCount > 0
              ? `${coverage.gameCount.toLocaleString()} games across ${coverage.matches.toLocaleString()} matches, from local replay data`
              : "No games recorded with this deck yet"}
          </p>
        </div>
        <div className="deck-analytics-controls">
          {versions.length > 1 ? (
            <label className="deck-analytics-version-select">
              <span>Version</span>
              <select
                value={versionId}
                onChange={(event) => {
                  setVersionId(Number(event.target.value));
                  setDrillDown(null);
                }}
              >
                <option value={0}>All versions</option>
                {versions.map((version, index) => (
                  <option key={version.id} value={version.id}>
                    Version {version.versionNumber}
                    {index === 0 ? " (current)" : ""}
                  </option>
                ))}
              </select>
            </label>
          ) : null}
          <span className={`analytics-coverage-badge is-${coverageState}`}>
            {coverageState === "complete"
              ? "Complete coverage"
              : coverageState === "partial"
                ? "Partial coverage"
                : "No card data"}
          </span>
        </div>
      </div>

      <dl className="analytics-coverage-grid" aria-label="Deck analytics data coverage">
        <div>
          <dt>Matches</dt>
          <dd>
            {coverage.matches.toLocaleString()}
            {coverage.matchesWithVersion < coverage.matches
              ? ` (${coverage.matchesWithVersion.toLocaleString()} versioned)`
              : ""}
          </dd>
        </div>
        <div>
          <dt>Game results</dt>
          <dd>{coverage.gameCount > 0 ? `${coverage.gamesWithResult} of ${coverage.gameCount}` : "—"}</dd>
        </div>
        <div>
          <dt>Opening hands</dt>
          <dd>{coverage.gameCount > 0 ? `${coverage.gamesWithOpeningHand} of ${coverage.gameCount}` : "—"}</dd>
        </div>
        <div>
          <dt>Play / draw</dt>
          <dd>{coverage.gameCount > 0 ? `${coverage.gamesWithPlayDraw} of ${coverage.gameCount}` : "—"}</dd>
        </div>
        <div>
          <dt>Card data</dt>
          <dd>{coverage.gameCount > 0 ? `${coverage.gamesWithCardStats} of ${coverage.gameCount}` : "—"}</dd>
        </div>
      </dl>

      {coverage.gameCount === 0 ? (
        <StatusMessage>
          Play matches with this deck (or run an import) and per-card analytics will appear here.
        </StatusMessage>
      ) : (
        <>
          <dl className="deck-analytics-tiles" aria-label="Deck record summary">
            <RecordTile
              label="Games"
              record={data.gameRecord}
              detail={data.unknownResultGames > 0 ? `${data.unknownResultGames} unknown excluded` : undefined}
              onDrill={() => setDrillDown({ label: "All games", params: {} })}
            />
            <RecordTile label="Matches" record={data.matchRecord} />
            <RecordTile
              label="Game 1"
              record={data.gameOne}
              onDrill={() => setDrillDown({ label: "Game one", params: { game: "one" } })}
            />
            <RecordTile
              label="Post-board"
              record={data.postBoard}
              onDrill={() => setDrillDown({ label: "Post-board games", params: { game: "post" } })}
            />
            <RecordTile
              label="On the play"
              record={data.onPlay}
              onDrill={() => setDrillDown({ label: "Games on the play", params: { playDraw: "play" } })}
            />
            <RecordTile
              label="On the draw"
              record={data.onDraw}
              onDrill={() => setDrillDown({ label: "Games on the draw", params: { playDraw: "draw" } })}
            />
            <div className="deck-analytics-tile">
              <dt>Avg mulligans</dt>
              <dd>
                <strong>{data.averageMulligans != null ? data.averageMulligans.toFixed(2) : "—"}</strong>
                <span>per game</span>
              </dd>
            </div>
          </dl>

          <div className="deck-analytics-buckets">
            <BucketTable
              title="Kept hand size"
              buckets={data.handSizes}
              formatKey={(key) => `${key} cards`}
              baseline={baseline}
              emptyMessage="No opening hands captured yet."
              onDrill={(bucket) =>
                setDrillDown({
                  label: `${bucket.key}-card keeps`,
                  params: { keptSize: bucket.key },
                })
              }
            />
            <BucketTable
              title="Mulligans"
              buckets={[...data.mulliganCounts].sort((a, b) => a.key - b.key)}
              formatKey={(key) => (key === 0 ? "None" : `${key}`)}
              baseline={baseline}
              emptyMessage="No mulligan data captured yet."
              onDrill={(bucket) =>
                setDrillDown({
                  label: bucket.key === 0 ? "No-mulligan games" : `Games with ${bucket.key} mulligan${bucket.key === 1 ? "" : "s"}`,
                  params: { mulligans: bucket.key },
                })
              }
            />
            <BucketTable
              title="Lands in kept hand"
              buckets={data.landCounts}
              formatKey={(key) => `${key} lands`}
              baseline={baseline}
              emptyMessage="No kept hands with resolved card types yet."
              footnote={
                data.landCountUnknownHands > 0
                  ? `${data.landCountUnknownHands} hand${data.landCountUnknownHands === 1 ? "" : "s"} excluded: card types not resolved yet.`
                  : undefined
              }
            />
          </div>

          <div className="deck-analytics-cards-head">
            <h4>Card performance</h4>
            <p>
              Win rates are game-scoped and compared against the deck baseline of{" "}
              {baseline == null ? "—" : pct(baseline)}. Rates under {MIN_SAMPLE_GAMES} games are dimmed. Click a value to
              see the games behind it.
            </p>
          </div>
          {sortedCards.length === 0 ? (
            <StatusMessage>
              No per-card data yet. It is derived from match replays during maintenance, shortly after launch.
            </StatusMessage>
          ) : (
            <div className="table-wrap">
              <table className="data-table deck-analytics-card-table">
                <thead>
                  <tr>
                    <th aria-sort={headerSort("name")}>
                      <button type="button" onClick={() => toggleSort("name")}>Card{sortIndicator("name")}</button>
                    </th>
                    <th aria-sort={headerSort("seen")} title="Games where the card was kept, drawn, or played">
                      <button type="button" onClick={() => toggleSort("seen")}>Games{sortIndicator("seen")}</button>
                    </th>
                    <th aria-sort={headerSort("inHand")} title="Win rate in games where the card was in hand (kept or drawn)">
                      <button type="button" onClick={() => toggleSort("inHand")}>In hand{sortIndicator("inHand")}</button>
                    </th>
                    <th aria-sort={headerSort("opening")} title="Win rate when in the kept opening hand">
                      <button type="button" onClick={() => toggleSort("opening")}>Opening{sortIndicator("opening")}</button>
                    </th>
                    <th aria-sort={headerSort("drawn")} title="Win rate when drawn after the opening hand">
                      <button type="button" onClick={() => toggleSort("drawn")}>Drawn{sortIndicator("drawn")}</button>
                    </th>
                    <th aria-sort={headerSort("played")} title="Win rate when cast or played">
                      <button type="button" onClick={() => toggleSort("played")}>Played{sortIndicator("played")}</button>
                    </th>
                    <th aria-sort={headerSort("notPlayed")} title="Win rate when in hand but never played">
                      <button type="button" onClick={() => toggleSort("notPlayed")}>Unplayed{sortIndicator("notPlayed")}</button>
                    </th>
                    <th aria-sort={headerSort("stranded")} title="Games that ended with the card still in hand">
                      <button type="button" onClick={() => toggleSort("stranded")}>Stranded{sortIndicator("stranded")}</button>
                    </th>
                    <th aria-sort={headerSort("mulled")} title="Copies shuffled back by mulligans">
                      <button type="button" onClick={() => toggleSort("mulled")}>Mulled{sortIndicator("mulled")}</button>
                    </th>
                    <th aria-sort={headerSort("avgPlayedTurn")} title="Average turn the first copy was played">
                      <button type="button" onClick={() => toggleSort("avgPlayedTurn")}>Avg turn{sortIndicator("avgPlayedTurn")}</button>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {sortedCards.map((card) => (
                    <tr key={card.cardId}>
                      <td className="deck-analytics-card-name">
                        <CardPreviewName cardId={card.cardId} cardName={card.cardName} />
                      </td>
                      <td>
                        <button
                          type="button"
                          className="deck-analytics-stat is-drillable is-neutral"
                          onClick={() => drillCard(card, "any", "all games")}
                          title="Show every game where this card was kept, drawn, or played"
                        >
                          <strong>{card.gamesSeen}</strong>
                          {card.unknownResultGames > 0 ? <span>{card.unknownResultGames} unk.</span> : null}
                        </button>
                      </td>
                      <td>
                        <StatCell record={card.inHand} baseline={baseline} />
                      </td>
                      <td>
                        <StatCell
                          record={card.openingHand}
                          baseline={baseline}
                          onDrill={() => drillCard(card, "opening", "kept in opening hand")}
                          drillLabel="kept in opening hand"
                        />
                      </td>
                      <td>
                        <StatCell
                          record={card.drawn}
                          baseline={baseline}
                          onDrill={() => drillCard(card, "drawn", "drawn during play")}
                          drillLabel="drawn during play"
                        />
                      </td>
                      <td>
                        <StatCell
                          record={card.played}
                          baseline={baseline}
                          onDrill={() => drillCard(card, "played", "cast or played")}
                          drillLabel="cast or played"
                        />
                      </td>
                      <td>
                        <StatCell
                          record={card.notPlayed}
                          baseline={baseline}
                          onDrill={() => drillCard(card, "notplayed", "in hand but never played")}
                          drillLabel="in hand but never played"
                        />
                      </td>
                      <td>
                        {card.endedInHandGames > 0 ? (
                          <button
                            type="button"
                            className="deck-analytics-stat is-drillable is-neutral"
                            onClick={() => drillCard(card, "stranded", "stranded at game end")}
                            title="Show games that ended with this card in hand"
                          >
                            <strong>{card.endedInHandGames}</strong>
                          </button>
                        ) : (
                          <span className="deck-analytics-stat is-empty">
                            <strong>—</strong>
                          </span>
                        )}
                      </td>
                      <td>
                        {card.mulliganCopies > 0 ? (
                          <button
                            type="button"
                            className="deck-analytics-stat is-drillable is-neutral"
                            onClick={() => drillCard(card, "mulled", "shuffled back by a mulligan")}
                            title="Show games where a copy was shuffled back by a mulligan"
                          >
                            <strong>{card.mulliganCopies}</strong>
                          </button>
                        ) : (
                          <span className="deck-analytics-stat is-empty">
                            <strong>—</strong>
                          </span>
                        )}
                      </td>
                      <td>
                        <span className="deck-analytics-stat is-neutral">
                          <strong>{card.avgFirstPlayedTurn != null ? card.avgFirstPlayedTurn.toFixed(1) : "—"}</strong>
                          {card.avgFirstSeenTurn != null ? <span>seen t{card.avgFirstSeenTurn.toFixed(1)}</span> : null}
                        </span>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          {drillDown ? (
            <DrillDownSection
              deckId={deckId}
              versionId={versionId}
              drillDown={drillDown}
              onClose={() => setDrillDown(null)}
            />
          ) : null}

          <p className="analytics-method-note">
            Results come from GRE game state; hand and card facts are inferred from replay snapshots and labeled
            derived. Correlation here is a prompt to review replays, not proof a card caused the result.
          </p>
        </>
      )}
    </section>
  );
}
