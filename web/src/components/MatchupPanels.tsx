import { useState } from "react";
import { Link } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { CardPreviewName } from "./CardPreviewName";
import { DeckColorIdentity } from "./MatchDeckColors";
import { EventLabel } from "./EventLabel";
import { ResultPill } from "./ResultPill";
import { SetSymbol } from "./SetSymbol";
import { StatusMessage } from "./StatusMessage";
import { api } from "../lib/api";
import { formatDateTime, pct } from "../lib/format";
import { useEventSets } from "../lib/useEventSets";
import type { MatchupRow, RecordAgg } from "../lib/types";

const ARCHETYPE_LABELS: Record<string, string> = {
  aggro: "Aggro",
  midrange: "Midrange",
  control: "Control",
  combo: "Combo",
  ramp: "Ramp",
  unknown: "Unknown",
};

function archetypeLabel(key: string): string {
  return ARCHETYPE_LABELS[key] ?? key;
}

function winRate(record: RecordAgg): number | null {
  return record.games > 0 ? record.wins / record.games : null;
}

function recordLabel(record: RecordAgg): string {
  const base = `${record.wins}–${record.losses}`;
  return record.draws > 0 ? `${base}–${record.draws}` : base;
}

function recordCellTone(record: RecordAgg): string {
  const rate = winRate(record);
  if (rate == null) {
    return "is-empty";
  }
  if (record.games < 3) {
    return "is-small-sample";
  }
  if (rate >= 0.55) {
    return "is-above";
  }
  if (rate <= 0.45) {
    return "is-below";
  }
  return "is-neutral";
}

function sumRecords(records: RecordAgg[]): RecordAgg {
  const out = { games: 0, wins: 0, losses: 0, draws: 0 };
  for (const record of records) {
    out.games += record.games;
    out.wins += record.wins;
    out.losses += record.losses;
    out.draws += record.draws;
  }
  return out;
}

function matchupRowTitle(row: MatchupRow): string {
  const colors = row.colors.length > 0 ? row.colors.join("") : "Colorless/Unknown";
  return row.archetype ? `${colors} ${archetypeLabel(row.archetype)}` : colors;
}

function SplitStat({ label, record }: { label: string; record: RecordAgg }) {
  const rate = winRate(record);
  return (
    <div>
      <dt>{label}</dt>
      <dd>
        <strong>{recordLabel(record)}</strong>
        <span>{rate == null ? "—" : pct(rate)}</span>
      </dd>
    </div>
  );
}

function ObservedCardChips({
  cards,
  emptyMessage,
  showLossSplit,
}: {
  cards: MatchupRow["topObservedCards"];
  emptyMessage: string;
  showLossSplit?: boolean;
}) {
  if (cards.length === 0) {
    return <p className="matchup-empty">{emptyMessage}</p>;
  }
  return (
    <ul className="matchup-card-chips">
      {cards.map((card) => (
        <li key={card.cardId}>
          <CardPreviewName cardId={card.cardId} cardName={card.cardName} />
          <small>
            {showLossSplit
              ? `${card.lossMatches}L / ${card.winMatches}W`
              : `${card.matches} match${card.matches === 1 ? "" : "es"}`}
          </small>
        </li>
      ))}
    </ul>
  );
}

function MatchupRowDetail({ contextLabel, row }: { contextLabel: string; row: MatchupRow }) {
  const queryClient = useQueryClient();
  const { lookup: setLookup } = useEventSets(row.matchRefs.map((ref) => ref.eventName));
  const overrideMutation = useMutation({
    mutationFn: ({ matchId, archetype }: { matchId: number; archetype: string }) =>
      api.setOpponentArchetype(matchId, archetype),
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: ["deck-matchups"] });
      void queryClient.invalidateQueries({ queryKey: ["limited-matchups"] });
    },
  });

  return (
    <article className="matchup-detail" aria-label={`${contextLabel} vs ${matchupRowTitle(row)}`}>
      <header className="matchup-detail-head">
        <div className="matchup-detail-title">
          <DeckColorIdentity colors={row.colors} known={row.colors.length > 0} />
          {row.archetype ? <h4>{archetypeLabel(row.archetype)}</h4> : null}
          <span className={`analytics-coverage-badge is-${row.confidence === "high" ? "complete" : row.confidence === "medium" ? "partial" : "unknown"}`}>
            {row.confidence} confidence
          </span>
        </div>
        <p>
          {row.matchRefs.length} match{row.matchRefs.length === 1 ? "" : "es"} · avg{" "}
          {pct(row.avgPctObserved)} of opponent deck observed
        </p>
      </header>

      <dl className="matchup-splits" aria-label="Matchup record splits">
        <SplitStat label="Matches" record={row.matches} />
        <SplitStat label="Games" record={row.games} />
        <SplitStat label="Game 1" record={row.gameOne} />
        <SplitStat label="Post-board" record={row.postBoard} />
        <SplitStat label="On the play" record={row.onPlay} />
        <SplitStat label="On the draw" record={row.onDraw} />
      </dl>
      {row.unknownResultGames > 0 ? (
        <p className="matchup-footnote">
          {row.unknownResultGames} game{row.unknownResultGames === 1 ? "" : "s"} with unknown result excluded from game
          records.
        </p>
      ) : null}

      <div className="matchup-observed">
        <div>
          <h5>Most observed cards</h5>
          <ObservedCardChips cards={row.topObservedCards} emptyMessage="No opposing cards recorded." />
        </div>
        <div>
          <h5>Skewed toward losses</h5>
          <ObservedCardChips
            cards={row.lossSkewedCards}
            emptyMessage="No card stands out in losses yet."
            showLossSplit
          />
        </div>
      </div>

      <div className="table-wrap">
        <table className="data-table matchup-matches-table">
          <thead>
            <tr>
              <th>Started</th>
              <th>Event</th>
              <th>Opponent</th>
              <th>Result</th>
              <th>Observed</th>
              <th>Archetype</th>
              <th>Details</th>
            </tr>
          </thead>
          <tbody>
            {row.matchRefs.map((ref) => (
              <tr key={ref.matchId}>
                <td>{ref.startedAt ? formatDateTime(ref.startedAt) : "—"}</td>
                <td>
                  <EventLabel eventName={ref.eventName ?? ""} lookup={setLookup} />
                </td>
                <td>{ref.opponent || "—"}</td>
                <td>
                  <ResultPill result={ref.result} />
                </td>
                <td>{pct(ref.pctObserved)}</td>
                <td>
                  <select
                    className="matchup-archetype-select"
                    value={ref.archetypeSource === "manual" ? ref.archetype : "auto"}
                    disabled={overrideMutation.isPending}
                    onChange={(event) => {
                      const value = event.target.value;
                      overrideMutation.mutate({
                        matchId: ref.matchId,
                        archetype: value === "auto" ? "" : value,
                      });
                    }}
                    aria-label={`Opponent archetype for match against ${ref.opponent || "unknown opponent"}`}
                  >
                    <option value="auto">Auto: {archetypeLabel(ref.archetype)}</option>
                    {Object.entries(ARCHETYPE_LABELS).map(([key, label]) => (
                      <option key={key} value={key}>
                        {label}
                      </option>
                    ))}
                  </select>
                </td>
                <td>
                  <Link className="text-link" to={`/matches/${ref.matchId}`}>
                    View
                  </Link>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </article>
  );
}

/**
 * One deck's record against classified opponent archetypes, with drill-downs.
 * Rendered on constructed deck detail pages; draft decks get the pooled
 * per-set view instead, where the sample is large enough to mean something.
 */
export function DeckMatchupsPanel({ deckId }: { deckId: number }) {
  const [selectedArchetype, setSelectedArchetype] = useState<string | null>(null);
  const query = useQuery({
    queryKey: ["deck-matchups", deckId],
    queryFn: () => api.deckMatchups(deckId),
  });

  if (query.isLoading) {
    return null;
  }
  if (query.error) {
    return (
      <section className="panel">
        <div className="panel-head">
          <h3>Matchups</h3>
        </div>
        <StatusMessage tone="error">{(query.error as Error).message}</StatusMessage>
      </section>
    );
  }

  const deck = query.data?.deck;
  if (!deck || deck.rows.length === 0) {
    return null;
  }

  const archetypes = (query.data?.archetypes ?? []).filter((key) =>
    deck.rows.some((row) => row.archetype === key),
  );
  const selectedRows = selectedArchetype
    ? deck.rows.filter((row) => row.archetype === selectedArchetype)
    : [];

  return (
    <section className="panel">
      <div className="panel-head">
        <div>
          <h3>Matchups</h3>
          <p>
            Opponents classified from locally observed cards — colors, curve, and card types. Click an archetype for
            splits, observed cards, and the underlying matches.
          </p>
        </div>
      </div>

      <div className="matchup-tiles" role="group" aria-label="Record by opponent archetype">
        <div className="matchup-tile">
          <span className="matchup-tile-label">Overall</span>
          <span className="deck-analytics-stat is-neutral">
            <strong>{recordLabel(deck.matches)}</strong>
            <span>{winRate(deck.matches) == null ? "—" : pct(winRate(deck.matches)!)}</span>
          </span>
        </div>
        {archetypes.map((key) => {
          const rows = deck.rows.filter((row) => row.archetype === key);
          const record = sumRecords(rows.map((row) => row.matches));
          const matchCount = rows.reduce((sum, row) => sum + row.matchRefs.length, 0);
          const isSelected = selectedArchetype === key;
          return (
            <button
              type="button"
              key={key}
              className={`matchup-tile is-drillable ${isSelected ? "is-selected" : ""}`}
              onClick={() => setSelectedArchetype(isSelected ? null : key)}
              title={`${deck.deckName} vs ${archetypeLabel(key)}: ${matchCount} matches`}
            >
              <span className="matchup-tile-label">{archetypeLabel(key)}</span>
              <span className={`deck-analytics-stat ${recordCellTone(record)}`}>
                <strong>{recordLabel(record)}</strong>
                <span>{winRate(record) == null ? `${matchCount} matches` : pct(winRate(record)!)}</span>
              </span>
            </button>
          );
        })}
      </div>

      {selectedArchetype && selectedRows.length > 0 ? (
        <div className="matchup-detail-list">
          {selectedRows.map((row) => (
            <MatchupRowDetail
              contextLabel={deck.deckName}
              row={row}
              key={`${row.colorsKey}-${row.archetype}`}
            />
          ))}
        </div>
      ) : null}

      <p className="analytics-method-note">
        Records count matches with known results; unknown results are excluded from every rate. Confidence reflects
        how much of each opponent's deck was actually observed.
      </p>
    </section>
  );
}

/**
 * Opponent color-pair records pooled across every draft/sealed run of a set.
 * In limited the color pair effectively is the archetype, and pooling a whole
 * set's matches is what makes the sample meaningful. Without a setCode it
 * shows every set (Drafts page); with one it scopes to that set (draft and
 * draft-deck detail pages).
 */
export function LimitedMatchupsPanel({ setCode }: { setCode?: string | null }) {
  const [selection, setSelection] = useState<{ setCode: string; colorsKey: string } | null>(null);
  // Per-set filter by the player's own deck colors; missing key = all decks.
  const [ownColorsFilter, setOwnColorsFilter] = useState<Record<string, string>>({});
  const query = useQuery({
    queryKey: ["limited-matchups"],
    queryFn: api.limitedMatchups,
  });
  const allSets = query.data?.sets ?? [];
  // null/undefined = every set (Drafts page); "" = the unknown-set group.
  const sets = setCode == null ? allSets : allSets.filter((set) => set.setCode === setCode);

  const setOwnColorsSelection = (forSet: string, groupKey: string | null) => {
    setOwnColorsFilter((current) => {
      const next = { ...current };
      if (groupKey == null) {
        delete next[forSet];
      } else {
        next[forSet] = groupKey;
      }
      return next;
    });
    // The opponent drill-down belongs to the previous group's rows.
    setSelection((current) => (current?.setCode === forSet ? null : current));
  };
  const { lookup: setLookup } = useEventSets(sets.map((set) => set.setCode));

  if (query.isLoading) {
    return null;
  }
  if (query.error) {
    return (
      <section className="panel">
        <div className="panel-head">
          <h3>Set Matchups</h3>
        </div>
        <StatusMessage tone="error">{(query.error as Error).message}</StatusMessage>
      </section>
    );
  }
  if (sets.length === 0) {
    return null;
  }

  return (
    <section className="panel">
      <div className="panel-head">
        <div>
          <h3>Set Matchups</h3>
          <p>
            Your record against opponent color pairs, pooled across every draft of a set — single draft decks are too
            short-lived for per-deck matchup data to mean much.
          </p>
        </div>
      </div>

      <div className="stack-lg">
        {sets.map((set) => {
          const setInfo = setLookup(set.setCode);
          const overallRate = winRate(set.matches);
          const activeGroupKey = ownColorsFilter[set.setCode];
          const activeGroup =
            activeGroupKey != null
              ? set.colorGroups.find((group) => group.colorsKey === activeGroupKey)
              : undefined;
          const visibleRows = activeGroup ? activeGroup.rows : set.rows;
          const selectedRow =
            selection && selection.setCode === set.setCode
              ? visibleRows.find((row) => row.colorsKey === selection.colorsKey)
              : undefined;
          return (
            <div className="limited-matchup-set" key={set.setCode || "unknown"}>
              <div className="limited-matchup-set-head">
                <span className="event-label">
                  {setInfo?.iconSvgUri ? <SetSymbol iconSvgUri={setInfo.iconSvgUri} name={setInfo.name} /> : null}
                  <span className="event-label-text">
                    {setInfo?.name ?? (set.setCode || "Unknown set")}
                  </span>
                </span>
                <p>
                  {recordLabel(set.matches)}
                  {overallRate == null ? "" : ` · ${pct(overallRate)}`} · {set.deckCount} deck
                  {set.deckCount === 1 ? "" : "s"}
                </p>
              </div>

              {set.colorGroups.length > 1 ? (
                <div className="matchup-tile-group">
                  <span className="matchup-tile-group-label">Your deck colors</span>
                  <div className="matchup-tiles" role="group" aria-label={`Record by your deck colors in ${set.setCode || "unknown set"}`}>
                    <button
                      type="button"
                      className={`matchup-tile is-drillable ${activeGroupKey == null ? "is-selected" : ""}`}
                      onClick={() => setOwnColorsSelection(set.setCode, null)}
                      title={`All decks: ${set.deckCount} decks`}
                    >
                      <span className="matchup-tile-label">All</span>
                      <span className={`deck-analytics-stat ${recordCellTone(set.matches)}`}>
                        <strong>{recordLabel(set.matches)}</strong>
                        <span>{overallRate == null ? "—" : pct(overallRate)}</span>
                      </span>
                    </button>
                    {set.colorGroups.map((group) => {
                      const isActive = activeGroupKey === group.colorsKey;
                      const groupRate = winRate(group.matches);
                      return (
                        <button
                          type="button"
                          key={group.colorsKey || "unknown"}
                          className={`matchup-tile is-drillable ${isActive ? "is-selected" : ""}`}
                          onClick={() => setOwnColorsSelection(set.setCode, isActive ? null : group.colorsKey)}
                          title={`Your ${group.colorsKey || "unknown-color"} decks: ${group.deckCount} deck${group.deckCount === 1 ? "" : "s"}`}
                        >
                          <span className="matchup-tile-label">
                            <DeckColorIdentity colors={group.colors} known={group.colorsKnown} />
                          </span>
                          <span className={`deck-analytics-stat ${recordCellTone(group.matches)}`}>
                            <strong>{recordLabel(group.matches)}</strong>
                            <span>
                              {groupRate == null ? "—" : pct(groupRate)} · {group.deckCount} deck
                              {group.deckCount === 1 ? "" : "s"}
                            </span>
                          </span>
                        </button>
                      );
                    })}
                  </div>
                </div>
              ) : null}

              {activeGroup ? (
                <span className="matchup-tile-group-label">
                  Opponent colors · your <DeckColorIdentity colors={activeGroup.colors} known={activeGroup.colorsKnown} /> decks
                </span>
              ) : set.colorGroups.length > 1 ? (
                <span className="matchup-tile-group-label">Opponent colors · all decks</span>
              ) : null}
              <div className="matchup-tiles" role="group" aria-label={`Record by opponent colors in ${set.setCode || "unknown set"}`}>
                {visibleRows.map((row) => {
                  const isSelected =
                    selection?.setCode === set.setCode && selection.colorsKey === row.colorsKey;
                  return (
                    <button
                      type="button"
                      key={row.colorsKey || "unknown"}
                      className={`matchup-tile is-drillable ${isSelected ? "is-selected" : ""}`}
                      onClick={() =>
                        setSelection(isSelected ? null : { setCode: set.setCode, colorsKey: row.colorsKey })
                      }
                      title={`vs ${matchupRowTitle(row)}: ${row.matchRefs.length} matches`}
                    >
                      <span className="matchup-tile-label">
                        <DeckColorIdentity colors={row.colors} known={row.colors.length > 0} />
                      </span>
                      <span className={`deck-analytics-stat ${recordCellTone(row.matches)}`}>
                        <strong>{recordLabel(row.matches)}</strong>
                        <span>
                          {winRate(row.matches) == null ? `${row.matchRefs.length} matches` : pct(winRate(row.matches)!)}
                        </span>
                      </span>
                    </button>
                  );
                })}
              </div>

              {selectedRow ? (
                <div className="matchup-detail-list">
                  <MatchupRowDetail
                    contextLabel={setInfo?.name ?? set.setCode ?? "Limited"}
                    row={selectedRow}
                  />
                </div>
              ) : null}
            </div>
          );
        })}
      </div>

      <p className="analytics-method-note">
        Records count matches with known results; unknown results are excluded from every rate. Speed labels
        (aggro/midrange/control) stay visible per match in the drill-down but are not a grouping axis in limited.
      </p>
    </section>
  );
}
