import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";

import { ContextualLink, useBreadcrumbNavigationState } from "../components/Breadcrumbs";
import { EventLabel } from "../components/EventLabel";
import { LimitedMatchupsPanel } from "../components/MatchupPanels";
import { SetSymbol } from "../components/SetSymbol";
import { StatusMessage } from "../components/StatusMessage";
import { api } from "../lib/api";
import { draftSessionType } from "../lib/draftReport";
import { parseEventName } from "../lib/events";
import { pct } from "../lib/format";
import type { DeckSummary, DraftSession } from "../lib/types";
import { useEventSets, type SetLookup } from "../lib/useEventSets";
import { useRowLink } from "../lib/useRowLink";

function parseDateValue(timestamp?: string | null): number | null {
  if (!timestamp) {
    return null;
  }

  const date = new Date(timestamp);
  if (Number.isNaN(date.getTime())) {
    return null;
  }

  return date.getTime();
}

function getDraftSessionDateValue(draft: DraftSession): number | null {
  return (
    parseDateValue(draft.startedAt) ??
    parseDateValue(draft.completedAt) ??
    parseEventName(draft.eventName).dateValue
  );
}

function formatDraftSessionDate(draft: DraftSession): string {
  const timestamp = draft.startedAt || draft.completedAt;
  const parsedTimestamp = parseDateValue(timestamp);
  if (parsedTimestamp != null && timestamp) {
    return new Intl.DateTimeFormat(undefined, { dateStyle: "medium" }).format(new Date(timestamp));
  }

  const eventDateValue = parseEventName(draft.eventName).dateValue;
  if (eventDateValue != null) {
    return new Intl.DateTimeFormat(undefined, {
      dateStyle: "medium",
      timeZone: "UTC",
    }).format(new Date(eventDateValue));
  }

  return "-";
}

function DraftSessionSet({ draft, lookup }: { draft: DraftSession; lookup: SetLookup }) {
  const parsed = parseEventName(draft.eventName);
  if (!parsed.setCode) {
    return <>-</>;
  }
  const setInfo = lookup(parsed.setCode);
  return (
    <span className="event-label">
      {setInfo?.iconSvgUri ? <SetSymbol iconSvgUri={setInfo.iconSvgUri} name={setInfo.name} /> : null}
      <span className="event-label-text">{setInfo?.name ?? parsed.setCode}</span>
    </span>
  );
}

function getDraftDeckTimestamp(deck: DeckSummary): number | null {
  return parseDateValue(deck.firstPlayedAt) ?? parseDateValue(deck.lastUpdatedAt);
}

function formatDraftDeckDate(deck: DeckSummary): string {
  const timestamp = deck.firstPlayedAt || deck.lastUpdatedAt;
  if (!timestamp) {
    return "-";
  }

  const date = new Date(timestamp);
  if (Number.isNaN(date.getTime())) {
    return "-";
  }

  return new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
  }).format(date);
}

function DraftSessionRow({ draft, setLookup }: { draft: DraftSession; setLookup: SetLookup }) {
  const rowLink = useRowLink(`/drafts/${draft.id}`);
  return (
    <tr {...rowLink}>
      <td>
        <Link to={`/drafts/${draft.id}`} className="text-link">
          {draft.id}
        </Link>
      </td>
      <td>{formatDraftSessionDate(draft)}</td>
      <td>{draftSessionType(draft)}</td>
      <td>
        <DraftSessionSet draft={draft} lookup={setLookup} />
      </td>
      <td>{draft.wins ?? "-"}</td>
      <td>{draft.losses ?? "-"}</td>
    </tr>
  );
}

function DraftDeckRow({ deck, setLookup }: { deck: DeckSummary; setLookup: SetLookup }) {
  const to = `/decks/${deck.deckId}`;
  const breadcrumbState = useBreadcrumbNavigationState(to);
  const rowLink = useRowLink(to, breadcrumbState);
  return (
    <tr {...rowLink}>
      <td>{formatDraftDeckDate(deck)}</td>
      <td>
        <ContextualLink to={to} className="text-link">
          {deck.deckName || `Deck ${deck.deckId}`}
        </ContextualLink>
      </td>
      <td>{deck.format || "-"}</td>
      <td>
        <EventLabel eventName={deck.eventName} lookup={setLookup} />
      </td>
      <td>{deck.matches}</td>
      <td>{deck.wins}</td>
      <td>{deck.losses}</td>
      <td>{pct(deck.winRate)}</td>
    </tr>
  );
}

export function DraftsPage() {
  const draftsQuery = useQuery({
    queryKey: ["drafts"],
    queryFn: api.drafts,
  });
  const draftDecksQuery = useQuery({
    queryKey: ["decks", "draft"],
    queryFn: () => api.decks("draft"),
  });

  const { lookup: setLookup } = useEventSets([
    ...(draftsQuery.data ?? []).map((draft) => draft.eventName),
    ...(draftDecksQuery.data ?? []).map((deck) => deck.eventName),
  ]);

  const draftDecks = [...(draftDecksQuery.data ?? [])].sort((a, b) => {
    const aDate = getDraftDeckTimestamp(a);
    const bDate = getDraftDeckTimestamp(b);

    if (aDate != null && bDate != null && aDate !== bDate) {
      return bDate - aDate;
    }
    if (aDate != null) {
      return -1;
    }
    if (bDate != null) {
      return 1;
    }

    return b.deckId - a.deckId;
  });

  if (draftsQuery.isLoading || draftDecksQuery.isLoading) return <StatusMessage>Loading drafts…</StatusMessage>;
  if (draftsQuery.error) return <StatusMessage tone="error">{(draftsQuery.error as Error).message}</StatusMessage>;
  if (draftDecksQuery.error) return <StatusMessage tone="error">{(draftDecksQuery.error as Error).message}</StatusMessage>;

  const drafts = [...(draftsQuery.data ?? [])].sort((a, b) => {
    const aDate = getDraftSessionDateValue(a);
    const bDate = getDraftSessionDateValue(b);

    if (aDate != null && bDate != null && aDate !== bDate) {
      return bDate - aDate;
    }
    if (aDate != null) {
      return -1;
    }
    if (bDate != null) {
      return 1;
    }

    return b.id - a.id;
  });

  return (
    <div className="stack-lg">
      <section className="panel">
        <div className="panel-head">
          <h3>Draft Sessions</h3>
          <p>{drafts.length} sessions</p>
        </div>
        <div className="table-wrap">
          <table className="data-table">
            <thead>
              <tr>
                <th>ID</th>
                <th>Date</th>
                <th>Type</th>
                <th>Set</th>
                <th>Wins</th>
                <th>Losses</th>
              </tr>
            </thead>
            <tbody>
              {drafts.map((draft) => (
                <DraftSessionRow key={draft.id} draft={draft} setLookup={setLookup} />
              ))}
            </tbody>
          </table>
        </div>
      </section>

      <LimitedMatchupsPanel />

      <section className="panel">
        <div className="panel-head">
          <h3>Draft Decks</h3>
          <p>{draftDecks.length} decks</p>
        </div>
        <div className="table-wrap">
          <table className="data-table">
            <thead>
              <tr>
                <th>Date</th>
                <th>Deck</th>
                <th>Format</th>
                <th>Event</th>
                <th>Matches</th>
                <th>Wins</th>
                <th>Losses</th>
                <th>Win Rate</th>
              </tr>
            </thead>
            <tbody>
              {draftDecks.map((deck) => (
                <DraftDeckRow key={deck.deckId} deck={deck} setLookup={setLookup} />
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  );
}
