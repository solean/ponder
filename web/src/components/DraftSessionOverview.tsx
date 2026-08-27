import { EventLabel } from "./EventLabel";
import {
  draftReplayCoverage,
  draftSessionDurationSeconds,
  draftSessionRecord,
  draftSessionStatus,
  draftSessionStatusLabel,
} from "../lib/draftReport";
import { eventEntryLabel, eventRewardLabel, formatEconomyDelta } from "../lib/economy";
import { formatDateTime, formatDuration, pct } from "../lib/format";
import type { DraftPick, DraftSession } from "../lib/types";
import { useEventSets } from "../lib/useEventSets";

function SummaryItem({
  label,
  value,
  detail,
  mono = false,
  valueClassName,
}: {
  label: string;
  value: string | number;
  detail?: string;
  mono?: boolean;
  valueClassName?: string;
}) {
  return (
    <div className={mono ? "is-mono" : undefined}>
      <dt>{label}</dt>
      <dd>
        <strong className={valueClassName}>{value}</strong>
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
  const entry = session.economy ? eventEntryLabel(session.economy) : "—";
  const winnings = session.economy ? eventRewardLabel(session.economy) : "—";
  const gemsDelta = session.economy?.netGems;
  const gemsDeltaLabel = gemsDelta == null ? "—" : `${formatEconomyDelta(gemsDelta)} gems`;
  const gemsDeltaClass =
    gemsDelta == null
      ? undefined
      : `economy-delta ${
          gemsDelta > 0
            ? "economy-delta--positive"
            : gemsDelta < 0
              ? "economy-delta--negative"
              : ""
        }`;

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
          label="Cost"
          value={entry}
          detail={session.economy ? "Event entry" : "No economy data"}
          mono
        />
        <SummaryItem
          label="Winnings"
          value={winnings}
          detail={
            session.economy
              ? session.economy.linkConfidence === "none"
                ? "No reward data"
                : "Event rewards"
              : "No economy data"
          }
          mono
        />
        <SummaryItem
          label="Gems delta"
          value={gemsDeltaLabel}
          detail={session.economy ? "Winnings minus cost" : "No economy data"}
          valueClassName={gemsDeltaClass}
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
