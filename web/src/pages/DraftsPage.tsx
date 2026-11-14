import { useId } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { useQuery, type UseQueryResult } from "@tanstack/react-query";

import { ContextualLink } from "../components/Breadcrumbs";
import { DraftAnalyticsPanel } from "../components/DraftAnalyticsPanel";
import { DraftPerformancePanel } from "../components/DraftPerformancePanel";
import { DraftWins } from "../components/DraftWins";
import { LimitedMatchupsPanel } from "../components/MatchupPanels";
import { SetSymbol } from "../components/SetSymbol";
import { SectionTabs, sectionPanelID, sectionTabID } from "../components/SectionTabs";
import { StatusMessage } from "../components/StatusMessage";
import { api } from "../lib/api";
import {
  draftPerformanceOptions,
  draftSessionDateValue,
  matchesDraftFilter,
  type DraftPerformanceFilter,
} from "../lib/draftPerformance";
import { draftSessionType } from "../lib/draftReport";
import { parseEventName } from "../lib/events";
import { pct, winRateTone } from "../lib/format";
import type { DraftSession } from "../lib/types";
import { sortSetCodesByRelease, useEventSets, type SetLookup } from "../lib/useEventSets";
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

/** Net entry-vs-prize currency for a finished run, e.g. "+700 gems". */
function draftNetLabel(draft: DraftSession): string {
  const economy = draft.economy;
  if (!economy) return "-";
  const parts: string[] = [];
  const signed = (value: number) => `${value > 0 ? "+" : "−"}${Math.abs(value).toLocaleString()}`;
  if (economy.netGems !== 0) parts.push(`${signed(economy.netGems)} gems`);
  if (economy.netGold !== 0) parts.push(`${signed(economy.netGold)} gold`);
  return parts.length > 0 ? parts.join(" · ") : "even";
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
        <span className="draft-result-pips" aria-hidden="true">
          {Array.from({ length: draft.wins ?? 0 }, (_, index) => (
            <span key={`w${index}`} className="match-group-pip is-win" />
          ))}
          {Array.from({ length: draft.losses ?? 0 }, (_, index) => (
            <span key={`l${index}`} className="match-group-pip is-loss" />
          ))}
        </span>
      </td>
      <td className="draft-session-record">
        {draft.wins == null ? (
          "-"
        ) : (
          <DraftWins wins={draft.wins} label={`${draft.wins}–${draft.losses ?? 0}`} />
        )}
      </td>
      <td>
        {winRate == null ? (
          "-"
        ) : (
          <strong className={`win-rate win-rate--${winRateTone(winRate)}`}>{pct(winRate)}</strong>
        )}
      </td>
      <td className="draft-session-net">{draftNetLabel(draft)}</td>
      <td>
        {draft.deckId != null ? (
          <ContextualLink to={`/decks/${draft.deckId}`} className="text-link">
            Deck
          </ContextualLink>
        ) : (
          "-"
        )}
      </td>
    </tr>
  );
}

const DRAFT_SECTIONS = [
  { id: "overview", label: "Overview" },
  { id: "performance", label: "Performance" },
  { id: "matchups", label: "Matchups" },
] as const;

type DraftSectionId = (typeof DRAFT_SECTIONS)[number]["id"];

function isDraftSection(value: string | null): value is DraftSectionId {
  return DRAFT_SECTIONS.some((section) => section.id === value);
}

/** Newest first, so a stable sort keeps same-day sessions in id order. */
function sortDraftsNewestFirst(drafts: DraftSession[]): DraftSession[] {
  return [...drafts].sort((a, b) => {
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
}

/**
 * Page-wide set and format filters. Set options come from every draft session
 * (newest release first); the selected values live in the URL so they survive
 * tab switches, reloads, and returning from a draft or match page.
 */
function DraftFilterControls({
  drafts,
  lookup,
  filter,
  onChange,
}: {
  drafts: DraftSession[];
  lookup: SetLookup;
  filter: DraftPerformanceFilter;
  onChange: (filter: DraftPerformanceFilter) => void;
}) {
  const sessionSets = drafts.flatMap((draft) => parseEventName(draft.eventName).setCode ?? []);
  if (filter.setCode) sessionSets.push(filter.setCode);
  const sets = sortSetCodesByRelease([...new Set(sessionSets)], lookup);
  const formats = draftPerformanceOptions(drafts).formats;
  if (filter.format && !formats.includes(filter.format)) formats.push(filter.format);

  return (
    <div className="rank-controls" role="group" aria-label="Draft filters">
      <label className="rank-season-select">
        <span>Set</span>
        <select value={filter.setCode ?? ""} onChange={(event) => onChange({ ...filter, setCode: event.target.value || null })}>
          <option value="">All sets</option>
          {sets.map((code) => (
            <option key={code} value={code}>
              {lookup(code)?.name ?? code}
            </option>
          ))}
        </select>
      </label>
      <label className="rank-season-select">
        <span>Format</span>
        <select value={filter.format ?? ""} onChange={(event) => onChange({ ...filter, format: event.target.value || null })}>
          <option value="">All formats</option>
          {formats.map((value) => (
            <option key={value} value={value}>
              {value}
            </option>
          ))}
        </select>
      </label>
    </div>
  );
}

export function DraftsPage() {
  const sectionTabBaseId = useId();
  const [searchParams, setSearchParams] = useSearchParams();
  const requestedSection = searchParams.get("tab");
  const activeSection: DraftSectionId = isDraftSection(requestedSection) ? requestedSection : "overview";
  const filter: DraftPerformanceFilter = {
    setCode: searchParams.get("set")?.toUpperCase() || null,
    format: searchParams.get("format") || null,
  };
  const draftsQuery = useQuery({
    queryKey: ["drafts"],
    queryFn: api.drafts,
  });
  const drafts = draftsQuery.data ?? [];
  const { lookup: setLookup } = useEventSets(drafts.map((draft) => draft.eventName));

  const updateParams = (changes: Record<string, string | null>) => {
    setSearchParams(
      (current) => {
        const next = new URLSearchParams(current);
        for (const [key, value] of Object.entries(changes)) {
          if (value == null) next.delete(key);
          else next.set(key, value);
        }
        return next;
      },
      { replace: true },
    );
  };

  const setName = filter.setCode ? (setLookup(filter.setCode)?.name ?? filter.setCode) : null;
  const matchupsEmptyMessage = `No ${filter.format ? `${filter.format} ` : ""}matchups recorded${setName ? ` for ${setName}` : ""} yet.`;

  return (
    <div className="stack-lg">
      <SectionTabs
        sections={DRAFT_SECTIONS}
        activeSection={activeSection}
        baseId={sectionTabBaseId}
        label="Draft sections"
        onSelect={(section) => updateParams({ tab: section === "overview" ? null : section })}
        actions={
          <DraftFilterControls
            drafts={drafts}
            lookup={setLookup}
            filter={filter}
            onChange={(next) => updateParams({ set: next.setCode, format: next.format })}
          />
        }
      />
      <div
        role="tabpanel"
        id={sectionPanelID(sectionTabBaseId, activeSection)}
        aria-labelledby={sectionTabID(sectionTabBaseId, activeSection)}
      >
        {activeSection === "overview" ? (
          <DraftsOverview query={draftsQuery} setLookup={setLookup} filter={filter} />
        ) : activeSection === "performance" ? (
          <DraftAnalyticsPanel set={filter.setCode} format={filter.format} />
        ) : (
          <LimitedMatchupsPanel
            setCode={filter.setCode}
            format={filter.format}
            emptyMessage={matchupsEmptyMessage}
          />
        )}
      </div>
    </div>
  );
}

function DraftsOverview({
  query: draftsQuery,
  setLookup,
  filter,
}: {
  query: UseQueryResult<DraftSession[]>;
  setLookup: SetLookup;
  filter: DraftPerformanceFilter;
}) {
  if (draftsQuery.isLoading) return <StatusMessage>Loading drafts…</StatusMessage>;
  if (draftsQuery.error) return <StatusMessage tone="error">{(draftsQuery.error as Error).message}</StatusMessage>;

  const drafts = sortDraftsNewestFirst(draftsQuery.data ?? []);

  const filteredDrafts = drafts.filter((draft) => matchesDraftFilter(draft, filter));
  const isFiltered = filter.setCode != null || filter.format != null;

  return (
    <div className="stack-lg">
      <DraftPerformancePanel drafts={drafts} setLookup={setLookup} filter={filter} />

      <section className="panel">
        <div className="panel-head">
          <h3>Draft Sessions</h3>
          <p>
            {isFiltered
              ? `${filteredDrafts.length} of ${drafts.length} sessions`
              : `${drafts.length} sessions`}
          </p>
        </div>
        <div className="table-wrap draft-sessions-scroll">
          <table className="data-table draft-sessions-table">
            <thead>
              <tr>
                <th>ID</th>
                <th>Date</th>
                <th>Type</th>
                <th>Set</th>
                <th>Result</th>
                <th>Record</th>
                <th>Win Rate</th>
                <th>Net</th>
                <th>Deck</th>
              </tr>
            </thead>
            <tbody>
              {filteredDrafts.map((draft) => (
                <DraftSessionRow key={draft.id} draft={draft} setLookup={setLookup} />
              ))}
            </tbody>
          </table>
          {filteredDrafts.length === 0 ? <p className="state">No draft sessions match these filters.</p> : null}
        </div>
      </section>
    </div>
  );
}
