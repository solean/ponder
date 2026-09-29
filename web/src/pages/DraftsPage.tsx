import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";

import { ContextualLink } from "../components/Breadcrumbs";
import { DraftPerformancePanel } from "../components/DraftPerformancePanel";
import { DraftWins } from "../components/DraftWins";
import { LimitedMatchupsPanel } from "../components/MatchupPanels";
import { SetSymbol } from "../components/SetSymbol";
import { StatusMessage } from "../components/StatusMessage";
import { api } from "../lib/api";
import { draftSessionDateValue } from "../lib/draftPerformance";
import { draftSessionType } from "../lib/draftReport";
import { parseEventName } from "../lib/events";
import { pct, winRateTone } from "../lib/format";
import type { DraftSession } from "../lib/types";
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

function DraftSessionRow({ draft, setLookup }: { draft: DraftSession; setLookup: SetLookup }) {
  const rowLink = useRowLink(`/drafts/${draft.id}`);
  const games = (draft.wins ?? 0) + (draft.losses ?? 0);
  const winRate = games > 0 ? (draft.wins ?? 0) / games : null;
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
      <td>
        <DraftWins wins={draft.wins} />
      </td>
      <td>{draft.losses ?? "-"}</td>
      <td>
        {winRate == null ? (
          "-"
        ) : (
          <strong className={`win-rate win-rate--${winRateTone(winRate)}`}>{pct(winRate)}</strong>
        )}
      </td>
      <td>
        {draft.deckId != null ? (
          <ContextualLink to={`/decks/${draft.deckId}`} className="text-link">
            View deck
          </ContextualLink>
        ) : (
          "-"
        )}
      </td>
    </tr>
  );
}

export function DraftsPage() {
  const draftsQuery = useQuery({
    queryKey: ["drafts"],
    queryFn: api.drafts,
  });
  const { lookup: setLookup } = useEventSets((draftsQuery.data ?? []).map((draft) => draft.eventName));

  if (draftsQuery.isLoading) return <StatusMessage>Loading drafts…</StatusMessage>;
  if (draftsQuery.error) return <StatusMessage tone="error">{(draftsQuery.error as Error).message}</StatusMessage>;

  const drafts = [...(draftsQuery.data ?? [])].sort((a, b) => {
    const aDate = draftSessionDateValue(a);
    const bDate = draftSessionDateValue(b);

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
      <DraftPerformancePanel drafts={drafts} setLookup={setLookup} />

      <section className="panel">
        <div className="panel-head">
          <h3>Draft Sessions</h3>
          <p>{drafts.length} sessions</p>
        </div>
        <div className="table-wrap draft-sessions-scroll">
          <table className="data-table draft-sessions-table">
            <thead>
              <tr>
                <th>ID</th>
                <th>Date</th>
                <th>Type</th>
                <th>Set</th>
                <th>Wins</th>
                <th>Losses</th>
                <th>Win Rate</th>
                <th>Deck</th>
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
    </div>
  );
}
