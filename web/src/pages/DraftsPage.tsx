import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";

import { StatusMessage } from "../components/StatusMessage";
import { api } from "../lib/api";
import { pct } from "../lib/format";
import type { DeckSummary, DraftSession } from "../lib/types";

const DRAFT_EVENT_DATE_PATTERN = /_(\d{4})(\d{2})(\d{2})$/;
const DRAFT_EVENT_PATTERN = /^(QuickDraft|PremierDraft|TraditionalDraft|BotDraft|Sealed|PremierSealed|TraditionalSealed|PlayerDraft)_([A-Z0-9]+)(?:_(\d{8}))?$/;

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

function parseEventDateValue(eventName?: string | null): number | null {
  const match = eventName?.match(DRAFT_EVENT_DATE_PATTERN);
  if (!match) {
    return null;
  }

  const [, year, month, day] = match;
  return Date.UTC(Number(year), Number(month) - 1, Number(day));
}

function parseDraftEvent(eventName?: string | null): { typeLabel: string; setCode: string } | null {
  const match = eventName?.match(DRAFT_EVENT_PATTERN);
  if (!match) {
    return null;
  }

  const [, rawType, setCode] = match;
  const typeLabel =
    {
      QuickDraft: "Quick",
      PremierDraft: "Premier",
      TraditionalDraft: "Traditional",
      BotDraft: "Bot",
      Sealed: "Sealed",
      PremierSealed: "Premier Sealed",
      TraditionalSealed: "Traditional Sealed",
      PlayerDraft: "Player",
    }[rawType] ?? rawType;

  return {
    typeLabel,
    setCode,
  };
}

function getDraftSessionDateValue(draft: DraftSession): number | null {
  return parseDateValue(draft.startedAt) ?? parseDateValue(draft.completedAt) ?? parseEventDateValue(draft.eventName);
}

function formatDraftSessionDate(draft: DraftSession): string {
  const timestamp = draft.startedAt || draft.completedAt;
  const parsedTimestamp = parseDateValue(timestamp);
  if (parsedTimestamp != null && timestamp) {
    return new Intl.DateTimeFormat(undefined, { dateStyle: "medium" }).format(new Date(timestamp));
  }

  const eventDateValue = parseEventDateValue(draft.eventName);
  if (eventDateValue != null) {
    return new Intl.DateTimeFormat(undefined, {
      dateStyle: "medium",
      timeZone: "UTC",
    }).format(new Date(eventDateValue));
  }

  return "-";
}

function formatDraftSessionType(draft: DraftSession): string {
  return parseDraftEvent(draft.eventName)?.typeLabel ?? (draft.isBotDraft ? "Bot" : "Player");
}

function formatDraftSessionSet(draft: DraftSession): string {
  const parsed = parseDraftEvent(draft.eventName);
  if (!parsed) {
    return "-";
  }

  return parsed.setCode;
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

export function DraftsPage() {
  const draftsQuery = useQuery({
    queryKey: ["drafts"],
    queryFn: api.drafts,
  });
  const draftDecksQuery = useQuery({
    queryKey: ["decks", "draft"],
    queryFn: () => api.decks("draft"),
  });

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
                <tr key={draft.id}>
                  <td>
                    <Link to={`/drafts/${draft.id}`} className="text-link">
                      {draft.id}
                    </Link>
                  </td>
                  <td>{formatDraftSessionDate(draft)}</td>
                  <td>{formatDraftSessionType(draft)}</td>
                  <td>{formatDraftSessionSet(draft)}</td>
                  <td>{draft.wins ?? "-"}</td>
                  <td>{draft.losses ?? "-"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

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
                <tr key={deck.deckId}>
                  <td>{formatDraftDeckDate(deck)}</td>
                  <td>
                    <Link to={`/decks/${deck.deckId}`} className="text-link">
                      {deck.deckName || `Deck ${deck.deckId}`}
                    </Link>
                  </td>
                  <td>{deck.format || "-"}</td>
                  <td>{deck.eventName || "-"}</td>
                  <td>{deck.matches}</td>
                  <td>{deck.wins}</td>
                  <td>{deck.losses}</td>
                  <td>{pct(deck.winRate)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  );
}
