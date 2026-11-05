import { useId, type ReactNode } from "react";

import { EventLabel } from "./EventLabel";
import {
  draftReplayCoverage,
  draftSessionDurationSeconds,
  draftSessionRecord,
  draftSessionStatus,
  draftSessionStatusLabel,
} from "../lib/draftReport";
import { netGemsSummary } from "../lib/economy";
import { formatDuration, formatMediumDateTime, pct } from "../lib/format";
import type { DraftPick, DraftSession } from "../lib/types";
import { useEventSets } from "../lib/useEventSets";

function HeaderStat({
  label,
  value,
  detail,
  tone = "neutral",
}: {
  label: string;
  value: string;
  detail: string;
  tone?: "positive" | "negative" | "neutral" | "unknown";
}) {
  return (
    <div>
      <dt>{label}</dt>
      <dd>
        <strong className={`is-${tone}`}>{value}</strong>
        <span>{detail}</span>
      </dd>
    </div>
  );
}

/** Joins the meta line's parts with dot separators that screen readers skip. */
function MetaLine({ items }: { items: ReactNode[] }) {
  return (
    <p className="draft-report-meta">
      {items.flatMap((item, index) => [
        index > 0 ? (
          <span key={`sep-${index}`} className="draft-report-meta-sep" aria-hidden="true">
            ·
          </span>
        ) : null,
        <span key={index}>{item}</span>,
      ])}
    </p>
  );
}

export function DraftSessionOverview({
  session,
  picks,
}: {
  session: DraftSession;
  picks: DraftPick[];
}) {
  const headingID = useId();
  const { lookup: setLookup } = useEventSets([session.eventName]);
  const status = draftSessionStatus(session);
  const record = draftSessionRecord(session);
  const duration = draftSessionDurationSeconds(session);
  const replayCoverage = draftReplayCoverage(picks);
  const loadedPickMismatch = session.picks !== picks.length;
  const incompleteReplay = replayCoverage.covered < replayCoverage.total;
  const netGems = netGemsSummary(session.economy);
  const matchesPlayed = record ? record.label !== "0–0" : false;

  const recordStat = record && matchesPlayed
    ? { value: record.label, detail: record.winRate == null ? "Result unavailable" : pct(record.winRate) }
    : { value: "—", detail: status === "in-progress" || record ? "No matches yet" : "Result unavailable" };

  const meta: ReactNode[] = [
    <>
      {session.isBotDraft ? "Bot draft" : "Player draft"} ({session.isBotDraft ? "Arena bots" : "human pod"})
    </>,
  ];
  if (session.startedAt) {
    meta.push(
      <>
        Started <span className="draft-report-meta-value">{formatMediumDateTime(session.startedAt)}</span>
      </>,
    );
  }
  if (duration != null) {
    meta.push(
      <>
        Drafted in <span className="draft-report-meta-value">{formatDuration(duration)}</span>
      </>,
    );
  } else if (status === "in-progress") {
    meta.push(<>Drafting now</>);
  }

  return (
    <section className="panel draft-report-overview" aria-labelledby={headingID}>
      <div className="draft-report-header">
        <div className="draft-report-heading">
          <div className="draft-report-kicker-row">
            <p className="draft-report-kicker">Draft #{session.id}</p>
            <span className={`draft-report-status is-${status}`}>{draftSessionStatusLabel(status)}</span>
          </div>
          <h2 id={headingID}>
            <EventLabel eventName={session.eventName} lookup={setLookup} />
          </h2>
          <MetaLine items={meta} />
        </div>

        <dl className="draft-report-stats" aria-label="Draft results">
          <HeaderStat
            label="Record"
            value={recordStat.value}
            detail={recordStat.detail}
            tone={recordStat.value === "—" ? "unknown" : "neutral"}
          />
          <HeaderStat label="Net gems" value={netGems.value} detail={netGems.detail} tone={netGems.tone} />
        </dl>
      </div>

      {loadedPickMismatch || incompleteReplay ? (
        <div className="draft-report-coverage" role="status">
          {loadedPickMismatch ? (
            <span>
              {picks.length} pick row{picks.length === 1 ? "" : "s"} loaded; the session reports {session.picks}.
            </span>
          ) : null}
          {incompleteReplay ? (
            <span>
              Pack contents recorded for {replayCoverage.covered} of {replayCoverage.total} loaded picks.
            </span>
          ) : null}
        </div>
      ) : null}
    </section>
  );
}
