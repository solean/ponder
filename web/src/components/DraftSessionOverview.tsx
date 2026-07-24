import { EventLabel } from "./EventLabel";
import {
  draftReplayCoverage,
  draftSessionDurationSeconds,
  draftSessionRecord,
  draftSessionStatus,
  draftSessionStatusLabel,
} from "../lib/draftReport";
import { formatDateTime, formatDuration, pct } from "../lib/format";
import type { DraftPick, DraftSession } from "../lib/types";
import { useEventSets } from "../lib/useEventSets";

function SummaryItem({
  label,
  value,
  detail,
  mono = false,
}: {
  label: string;
  value: string | number;
  detail?: string;
  mono?: boolean;
}) {
  return (
    <div className={mono ? "is-mono" : undefined}>
      <dt>{label}</dt>
      <dd>
        <strong>{value}</strong>
        {detail ? <span>{detail}</span> : null}
      </dd>
    </div>
  );
}

export function DraftSessionOverview({
  session,
  picks,
}: {
  session: DraftSession;
  picks: DraftPick[];
}) {
  const { lookup: setLookup } = useEventSets([session.eventName]);
  const status = draftSessionStatus(session);
  const record = draftSessionRecord(session);
  const duration = draftSessionDurationSeconds(session);
  const replayCoverage = draftReplayCoverage(picks);
  const loadedPickMismatch = session.picks !== picks.length;
  const incompleteReplay = replayCoverage.covered < replayCoverage.total;

  return (
    <section className="panel draft-report-overview">
      <div className="panel-head">
        <div className="draft-report-heading">
          <p className="draft-report-kicker">Draft session #{session.id}</p>
          <h2>
            <EventLabel eventName={session.eventName} lookup={setLookup} />
          </h2>
        </div>
      </div>

      <dl className="draft-report-summary" aria-label="Draft session overview">
        <SummaryItem
          label="Record"
          value={record?.label ?? "—"}
          detail={record?.winRate == null ? "Result unavailable" : pct(record.winRate)}
          mono
        />
        <SummaryItem
          label="Picks"
          value={session.picks}
          detail={`${picks.length} loaded`}
          mono
        />
        <SummaryItem
          label="Started"
          value={session.startedAt ? formatDateTime(session.startedAt) : "—"}
          mono
        />
        <SummaryItem
          label="Draft duration"
          value={duration == null ? "—" : formatDuration(duration)}
          mono
        />
        <SummaryItem
          label="Draft source"
          value={session.isBotDraft ? "Bot draft" : "Player draft"}
          detail={session.isBotDraft ? "Arena bots" : "Human pod"}
        />
        <div>
          <dt>Status</dt>
          <dd>
            <span className={`draft-report-status is-${status}`}>
              {draftSessionStatusLabel(status)}
            </span>
          </dd>
        </div>
      </dl>

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
