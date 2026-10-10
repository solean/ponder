import { useMemo } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";

import { ManaSymbol } from "../components/ManaSymbol";
import { DeckColorIdentity } from "../components/MatchDeckColors";
import { StatusMessage } from "../components/StatusMessage";
import { api } from "../lib/api";
import {
  DECK_FILTER_COLORS,
  DECK_PERIODS,
  EMPTY_DECK_FILTERS,
  SMALL_SAMPLE_MATCHES,
  buildDeckRow,
  deckDisplayName,
  deckFiltersScopeRecord,
  deckListParams,
  defaultSortDesc,
  filterDeckRows,
  hasActiveDeckFilters,
  parseDeckListParams,
  sortDeckRows,
  type DeckListFilters,
  type DeckListRow,
  type DeckSortKey,
} from "../lib/deckList";
import { eventCategory } from "../lib/events";
import { formatDateTime, formatRelativeTime, pct, winRateTone } from "../lib/format";
import { useRowLink } from "../lib/useRowLink";

const MAX_EVENT_CHIPS = 2;

const COLUMNS: Array<{ key: DeckSortKey | null; label: string; title?: string; className?: string }> = [
  { key: "name", label: "Deck" },
  { key: "colors", label: "Colors" },
  { key: "format", label: "Format" },
  { key: null, label: "Events", title: "Event types this deck has been played in" },
  { key: "lastPlayed", label: "Last Played" },
  { key: "matches", label: "Matches", className: "deck-list-num" },
  { key: "record", label: "Record", title: "Wins–losses; sorts by net wins", className: "deck-list-num" },
  { key: "winRate", label: "Win Rate", className: "deck-list-num" },
];

function DeckEvents({ row }: { row: DeckListRow }) {
  if (row.events.length === 0) return <span className="deck-list-muted">—</span>;
  const shown = row.events.slice(0, MAX_EVENT_CHIPS);
  const hidden = row.events.slice(MAX_EVENT_CHIPS);
  const describe = (events: DeckListRow["events"]) =>
    events.map((event) => `${event.category}: ${event.matches}`).join("\n");

  return (
    <span className="deck-list-events" title={describe(row.events)}>
      {shown.map((event) => (
        <span key={event.category} className="deck-list-event">
          {event.category}
        </span>
      ))}
      {hidden.length > 0 ? <span className="deck-list-event deck-list-event--more">+{hidden.length}</span> : null}
    </span>
  );
}

function DeckWinRate({ row }: { row: DeckListRow }) {
  if (row.winRate == null) return <span className="deck-list-muted">—</span>;
  const decided = row.wins + row.losses;
  const small = decided < SMALL_SAMPLE_MATCHES;
  return (
    <strong
      className={`win-rate win-rate--${winRateTone(row.winRate)}${small ? " is-small-sample" : ""}`}
      title={small ? `Small sample: only ${decided} decided ${decided === 1 ? "match" : "matches"}` : undefined}
    >
      {pct(row.winRate)}
    </strong>
  );
}

function DeckRow({ row }: { row: DeckListRow }) {
  const href = `/decks/${row.deck.deckId}`;
  const rowLink = useRowLink(href);
  return (
    <tr {...rowLink}>
      <td>
        <Link to={href} className="text-link">
          {deckDisplayName(row.deck)}
        </Link>
      </td>
      <td>
        <DeckColorIdentity colors={row.colors} known={row.deck.colorsKnown} />
      </td>
      <td>{row.formatLabel}</td>
      <td>
        <DeckEvents row={row} />
      </td>
      <td>
        {row.lastPlayedAt ? (
          <span className="deck-list-date" title={formatDateTime(row.lastPlayedAt)}>
            {formatRelativeTime(row.lastPlayedAt)}
          </span>
        ) : (
          <span className="deck-list-muted">Never</span>
        )}
      </td>
      <td className="deck-list-num">{row.matches}</td>
      <td className="deck-list-num">
        {row.matches > 0 ? `${row.wins}–${row.losses}` : <span className="deck-list-muted">—</span>}
      </td>
      <td className="deck-list-num">
        <DeckWinRate row={row} />
      </td>
    </tr>
  );
}

export function DecksPage() {
  const { data, isLoading, error } = useQuery({
    queryKey: ["decks"],
    queryFn: () => api.decks(),
  });
  const [searchParams, setSearchParams] = useSearchParams();
  const { filters, sort } = useMemo(() => parseDeckListParams(searchParams), [searchParams]);

  const decks = useMemo(() => data ?? [], [data]);
  const allRows = useMemo(() => {
    const now = Date.now();
    return decks.map((deck) => buildDeckRow(deck, filters, now));
  }, [decks, filters]);
  const rows = useMemo(() => sortDeckRows(filterDeckRows(allRows, filters), sort), [allRows, filters, sort]);

  const formatOptions = useMemo(
    () => [...new Set(allRows.map((row) => row.formatLabel).filter((label) => label !== "-"))].sort(),
    [allRows],
  );
  const eventOptions = useMemo(
    () => [...new Set(decks.flatMap((deck) => (deck.results ?? []).map((result) => eventCategory(result.eventName))))].sort(),
    [decks],
  );

  // Replace rather than push so back-navigation skips individual keystrokes
  // and returns straight to the previous page with the last view intact.
  function updateFilters(patch: Partial<DeckListFilters>) {
    setSearchParams(deckListParams({ ...filters, ...patch }, sort), { replace: true });
  }

  function toggleColor(color: string) {
    updateFilters({
      colors: filters.colors.includes(color)
        ? filters.colors.filter((value) => value !== color)
        : [...filters.colors, color],
    });
  }

  function toggleSort(key: DeckSortKey) {
    const next = key === sort.key ? { key, desc: !sort.desc } : { key, desc: defaultSortDesc(key) };
    setSearchParams(deckListParams(filters, next), { replace: true });
  }

  if (isLoading) return <StatusMessage>Loading decks…</StatusMessage>;
  if (error) return <StatusMessage tone="error">{(error as Error).message}</StatusMessage>;

  const filtersActive = hasActiveDeckFilters(filters);
  const deckCount = `${decks.length} ${decks.length === 1 ? "deck" : "decks"}`;
  const summary = filtersActive ? `${rows.length} of ${deckCount}` : deckCount;
  const headerSort = (key: DeckSortKey): "ascending" | "descending" | "none" =>
    sort.key === key ? (sort.desc ? "descending" : "ascending") : "none";
  const sortIndicator = (key: DeckSortKey) => (sort.key === key ? (sort.desc ? " ↓" : " ↑") : "");

  return (
    <section className="panel decks-page">
      <div className="panel-head">
        <h3>Decks</h3>
        <p>{summary}</p>
      </div>

      <div className="match-filter-bar" aria-label="Deck filters">
        <label className="match-filter-field match-filter-search">
          <span>Deck</span>
          <input
            className="settings-input"
            type="search"
            value={filters.q}
            onChange={(event) => updateFilters({ q: event.target.value })}
            placeholder="Search decks…"
            spellCheck={false}
          />
        </label>

        <label className="match-filter-field">
          <span>Format</span>
          <select
            className="settings-input"
            value={filters.format}
            onChange={(event) => updateFilters({ format: event.target.value })}
          >
            <option value="">All formats</option>
            {formatOptions.map((format) => (
              <option key={format} value={format}>
                {format}
              </option>
            ))}
          </select>
        </label>

        <label className="match-filter-field">
          <span>Event</span>
          <select
            className="settings-input"
            value={filters.event}
            onChange={(event) => updateFilters({ event: event.target.value })}
          >
            <option value="">All events</option>
            {eventOptions.map((category) => (
              <option key={category} value={category}>
                {category}
              </option>
            ))}
          </select>
        </label>

        <label className="match-filter-field">
          <span>Played</span>
          <select
            className="settings-input"
            value={filters.period}
            onChange={(event) => updateFilters({ period: event.target.value as DeckListFilters["period"] })}
          >
            <option value="">All time</option>
            {DECK_PERIODS.map((period) => (
              <option key={period.value} value={period.value}>
                {period.label}
              </option>
            ))}
          </select>
        </label>

        <div className="match-filter-field">
          <span>Colors</span>
          <div className="match-filter-colors" role="group" aria-label="Filter by deck colors">
            {DECK_FILTER_COLORS.map((color) => (
              <button
                key={color}
                type="button"
                className={`match-filter-color-chip${filters.colors.includes(color) ? " is-active" : ""}`}
                aria-pressed={filters.colors.includes(color)}
                onClick={() => toggleColor(color)}
              >
                <ManaSymbol token={color} />
              </button>
            ))}
          </div>
        </div>

        <div className="match-filter-actions">
          <button
            type="button"
            className={`control-button match-filter-toggle${filters.hideUnplayed ? " is-active" : ""}`}
            aria-pressed={filters.hideUnplayed}
            onClick={() => updateFilters({ hideUnplayed: !filters.hideUnplayed })}
          >
            Hide Unplayed
          </button>
          <button
            type="button"
            className="control-button"
            onClick={() => updateFilters(EMPTY_DECK_FILTERS)}
            disabled={!filtersActive}
          >
            Clear Filters
          </button>
        </div>
      </div>

      {deckFiltersScopeRecord(filters) ? (
        <p className="deck-list-scope-note">Records and win rates count only matches in the selected event and period.</p>
      ) : null}

      {rows.length === 0 ? (
        <div className="table-wrap match-table-empty">
          <span className="match-filter-empty">No decks found for the current filters.</span>
        </div>
      ) : (
        <div className="table-wrap">
          <table className="data-table deck-list-table">
            <thead>
              <tr>
                {COLUMNS.map((column) =>
                  column.key ? (
                    <th
                      key={column.label}
                      className={column.className}
                      aria-sort={headerSort(column.key)}
                      title={column.title}
                    >
                      <button type="button" onClick={() => toggleSort(column.key as DeckSortKey)}>
                        {column.label}
                        {sortIndicator(column.key)}
                      </button>
                    </th>
                  ) : (
                    <th key={column.label} className={column.className} title={column.title}>
                      {column.label}
                    </th>
                  ),
                )}
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <DeckRow key={row.deck.deckId} row={row} />
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}
