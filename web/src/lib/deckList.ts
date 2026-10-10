import { eventCategory } from "./events";
import { formatGameFormat } from "./format";
import type { DeckMatchResult, DeckSummary } from "./types";

/**
 * Filtering, scoping, and sorting for the Decks table. The state round-trips
 * through the URL so back-navigation from a deck restores the same view.
 *
 * Event and period filters scope the record, not just the row set: filtering
 * to "Traditional Ladder" shows each deck's ladder record only.
 */

export const DECK_FILTER_COLORS = ["W", "U", "B", "R", "G"] as const;

/** Below this many decided matches a win rate is flagged as a small sample. */
export const SMALL_SAMPLE_MATCHES = 10;

export type DeckSortKey = "name" | "colors" | "format" | "lastPlayed" | "matches" | "record" | "winRate";
export type DeckPeriod = "" | "7d" | "30d" | "90d";

export const DECK_PERIODS: Array<{ value: DeckPeriod; label: string; days: number }> = [
  { value: "7d", label: "Last 7 days", days: 7 },
  { value: "30d", label: "Last 30 days", days: 30 },
  { value: "90d", label: "Last 90 days", days: 90 },
];

export type DeckListFilters = {
  q: string;
  format: string;
  event: string;
  colors: string[];
  period: DeckPeriod;
  hideUnplayed: boolean;
};

export type DeckListSort = { key: DeckSortKey; desc: boolean };

export const EMPTY_DECK_FILTERS: DeckListFilters = {
  q: "",
  format: "",
  event: "",
  colors: [],
  period: "",
  hideUnplayed: false,
};

export const DEFAULT_DECK_SORT: DeckListSort = { key: "lastPlayed", desc: true };

const SORT_KEYS: DeckSortKey[] = ["name", "colors", "format", "lastPlayed", "matches", "record", "winRate"];

/** Text-like columns start ascending; numeric and date columns start descending. */
export function defaultSortDesc(key: DeckSortKey): boolean {
  return key !== "name" && key !== "colors" && key !== "format";
}

export type DeckEventCount = { category: string; matches: number };

export type DeckListRow = {
  deck: DeckSummary;
  formatLabel: string;
  colors: string[];
  /** Event categories across all of the deck's matches, most played first. */
  events: DeckEventCount[];
  /** Stats over the matches inside the active event/period scope. */
  matches: number;
  wins: number;
  losses: number;
  winRate: number | null;
  lastPlayedAt: string;
};

export function parseDeckListParams(params: URLSearchParams): { filters: DeckListFilters; sort: DeckListSort } {
  const period = params.get("period") ?? "";
  const colors = (params.get("colors") ?? "")
    .toUpperCase()
    .split("")
    .filter((color): color is (typeof DECK_FILTER_COLORS)[number] =>
      (DECK_FILTER_COLORS as readonly string[]).includes(color),
    );

  const sortParam = params.get("sort") as DeckSortKey | null;
  const key = sortParam && SORT_KEYS.includes(sortParam) ? sortParam : DEFAULT_DECK_SORT.key;
  const dir = params.get("dir");
  const desc = dir === "asc" ? false : dir === "desc" ? true : defaultSortDesc(key);

  return {
    filters: {
      q: params.get("q") ?? "",
      format: params.get("format") ?? "",
      event: params.get("event") ?? "",
      colors: [...new Set(colors)],
      period: DECK_PERIODS.some((option) => option.value === period) ? (period as DeckPeriod) : "",
      hideUnplayed: params.get("unplayed") === "hide",
    },
    sort: { key, desc },
  };
}

/** Serializes state, omitting defaults so an untouched view has a bare URL. */
export function deckListParams(filters: DeckListFilters, sort: DeckListSort): URLSearchParams {
  const params = new URLSearchParams();
  if (filters.q) params.set("q", filters.q);
  if (filters.format) params.set("format", filters.format);
  if (filters.event) params.set("event", filters.event);
  if (filters.colors.length > 0) {
    params.set("colors", DECK_FILTER_COLORS.filter((color) => filters.colors.includes(color)).join(""));
  }
  if (filters.period) params.set("period", filters.period);
  if (filters.hideUnplayed) params.set("unplayed", "hide");
  if (sort.key !== DEFAULT_DECK_SORT.key) params.set("sort", sort.key);
  if (sort.desc !== defaultSortDesc(sort.key)) params.set("dir", sort.desc ? "desc" : "asc");
  return params;
}

export function hasActiveDeckFilters(filters: DeckListFilters): boolean {
  return (
    filters.q.trim() !== "" ||
    filters.format !== "" ||
    filters.event !== "" ||
    filters.colors.length > 0 ||
    filters.period !== "" ||
    filters.hideUnplayed
  );
}

/** Event/period filters narrow which matches count toward each deck's record. */
export function deckFiltersScopeRecord(filters: DeckListFilters): boolean {
  return filters.event !== "" || filters.period !== "";
}

function periodStart(period: DeckPeriod, now: number): number {
  const option = DECK_PERIODS.find((candidate) => candidate.value === period);
  return option ? now - option.days * 24 * 60 * 60 * 1000 : Number.NEGATIVE_INFINITY;
}

function resultInScope(result: DeckMatchResult, filters: DeckListFilters, since: number): boolean {
  if (filters.event && eventCategory(result.eventName) !== filters.event) return false;
  if (since !== Number.NEGATIVE_INFINITY) {
    const playedAt = Date.parse(result.playedAt);
    if (!Number.isFinite(playedAt) || playedAt < since) return false;
  }
  return true;
}

function deckEvents(results: DeckMatchResult[]): DeckEventCount[] {
  const counts = new Map<string, number>();
  for (const result of results) {
    const category = eventCategory(result.eventName);
    counts.set(category, (counts.get(category) ?? 0) + 1);
  }
  return [...counts.entries()]
    .map(([category, matches]) => ({ category, matches }))
    .sort((a, b) => b.matches - a.matches || a.category.localeCompare(b.category));
}

export function buildDeckRow(deck: DeckSummary, filters: DeckListFilters, now: number): DeckListRow {
  const results = deck.results ?? [];
  const since = periodStart(filters.period, now);
  const scoped = deckFiltersScopeRecord(filters)
    ? results.filter((result) => resultInScope(result, filters, since))
    : results;

  let wins = 0;
  let losses = 0;
  for (const result of scoped) {
    if (result.result === "win") wins += 1;
    else if (result.result === "loss") losses += 1;
  }
  const decided = wins + losses;

  return {
    deck,
    formatLabel: formatGameFormat(deck.format),
    colors: deck.colorsKnown ? (deck.colors ?? []).map((color) => color.toUpperCase()) : [],
    events: deckEvents(results),
    matches: scoped.length,
    wins,
    losses,
    winRate: decided > 0 ? wins / decided : null,
    // Results arrive newest first.
    lastPlayedAt: scoped[0]?.playedAt ?? "",
  };
}

export function filterDeckRows(rows: DeckListRow[], filters: DeckListFilters): DeckListRow[] {
  const query = filters.q.trim().toLowerCase();
  const scoped = deckFiltersScopeRecord(filters);
  return rows.filter((row) => {
    if (query && !deckDisplayName(row.deck).toLowerCase().includes(query)) return false;
    if (filters.format && row.formatLabel !== filters.format) return false;
    if (filters.colors.length > 0) {
      if (!row.deck.colorsKnown) return false;
      if (!filters.colors.every((color) => row.colors.includes(color))) return false;
    }
    // A scoped view only lists decks that actually played in that scope.
    if ((scoped || filters.hideUnplayed) && row.matches === 0) return false;
    return true;
  });
}

export function deckDisplayName(deck: DeckSummary): string {
  return deck.deckName || `Deck ${deck.deckId}`;
}

function colorSortValue(row: DeckListRow): string | null {
  // Fewer colors first, then WUBRG order within the same count.
  if (!row.deck.colorsKnown) return null;
  const indexes = row.colors.map((color) => DECK_FILTER_COLORS.indexOf(color as never)).join("");
  return `${row.colors.length}${indexes}`;
}

function compareText(a: string, b: string): number {
  return a.localeCompare(b, undefined, { sensitivity: "base", numeric: true });
}

/** Returns null when the row has no value for the key; those always sort last. */
function sortValue(row: DeckListRow, key: DeckSortKey): number | string | null {
  switch (key) {
    case "name":
      return deckDisplayName(row.deck);
    case "colors":
      return colorSortValue(row);
    case "format":
      return row.formatLabel === "-" ? null : row.formatLabel;
    case "lastPlayed": {
      const time = Date.parse(row.lastPlayedAt);
      return Number.isFinite(time) ? time : null;
    }
    case "matches":
      return row.matches;
    case "record":
      return row.matches > 0 ? row.wins - row.losses : null;
    case "winRate":
      return row.winRate;
  }
}

export function sortDeckRows(rows: DeckListRow[], sort: DeckListSort): DeckListRow[] {
  const direction = sort.desc ? -1 : 1;
  return [...rows].sort((a, b) => {
    const left = sortValue(a, sort.key);
    const right = sortValue(b, sort.key);
    if (left == null || right == null) {
      if (left == null && right != null) return 1;
      if (right == null && left != null) return -1;
    } else {
      const delta =
        typeof left === "string" && typeof right === "string"
          ? compareText(left, right)
          : (left as number) - (right as number);
      if (delta !== 0) return delta * direction;
    }
    // Stable, meaningful tie-breaks: more matches, then name.
    return b.matches - a.matches || compareText(deckDisplayName(a.deck), deckDisplayName(b.deck));
  });
}
