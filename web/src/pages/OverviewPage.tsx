import { useQuery } from "@tanstack/react-query";
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type CSSProperties } from "react";
import { createPortal } from "react-dom";
import { Link } from "react-router-dom";

import { EventLabel } from "../components/EventLabel";
import { MatchDeckColors } from "../components/MatchDeckColors";
import { RankProgressPanel } from "../components/RankProgressPanel";
import { ResultPill } from "../components/ResultPill";
import { StatusMessage } from "../components/StatusMessage";
import { api } from "../lib/api";
import { eventDisplayName, parseEventName } from "../lib/events";
import {
  formatDateTime,
  formatDuration,
  formatRelativeTime,
  pct,
} from "../lib/format";
import {
  currentStreak,
  dailyActivity,
  matchAverages,
  recentForm,
  recordWinRate,
  sortMatchesDesc,
  splitRecords,
  type DailyActivity,
  type WinLossRecord,
} from "../lib/overviewStats";
import type { DraftSession, RuntimeStatus } from "../lib/types";
import { useEventSets } from "../lib/useEventSets";

const RECENT_MATCH_COUNT = 8;
const FORM_WINDOW = 10;
const ACTIVITY_DAYS = 365;

type Tone = "positive" | "negative" | "neutral";

function toneFor(rate: number | null): Tone {
  if (rate == null) return "neutral";
  const displayed = Number((rate * 100).toFixed(1));
  return displayed > 50 ? "positive" : displayed < 50 ? "negative" : "neutral";
}

function SplitRow({ label, record }: { label: string; record: WinLossRecord }) {
  const rate = recordWinRate(record);
  return (
    <div className="split-row">
      <div className="split-row-top">
        <span className="split-row-label">{label}</span>
        <span className="split-row-stat">
          {rate == null ? (
            <span className="split-row-empty">no matches</span>
          ) : (
            <>
              {record.wins}W–{record.losses}L
              <strong> {pct(rate)}</strong>
            </>
          )}
        </span>
      </div>
      <div className="split-bar" aria-hidden="true">
        {rate != null ? (
          <span className="split-bar-fill" style={{ width: `${Math.max(rate * 100, 2)}%` }} />
        ) : null}
      </div>
    </div>
  );
}

const ACTIVITY_HOVER_DELAY_MS = 120;
const ACTIVITY_POPOVER_GAP = 10;
const ACTIVITY_POPOVER_PADDING = 8;
const activityDateFormatter = new Intl.DateTimeFormat(undefined, {
  weekday: "long",
  month: "long",
  day: "numeric",
  year: "numeric",
});
const activityNumberFormatter = new Intl.NumberFormat();
const activityRateFormatter = new Intl.NumberFormat(undefined, {
  style: "percent",
  minimumFractionDigits: 1,
  maximumFractionDigits: 1,
});

type ActivityPopoverPosition = {
  top: number;
  left: number;
  arrowLeft: number;
  placement: "above" | "below";
};

function activityFullDate(dateKey: string): string {
  const date = new Date(`${dateKey}T00:00:00`);
  return Number.isNaN(date.getTime()) ? dateKey : activityDateFormatter.format(date);
}

function activityDuration(day: DailyActivity): string | null {
  if (day.timedMatches === 0 || day.trackedSeconds <= 0) return null;

  const totalMinutes = Math.max(1, Math.round(day.trackedSeconds / 60));
  const hours = Math.floor(totalMinutes / 60);
  const minutes = totalMinutes % 60;
  const duration = [
    hours > 0 ? `${activityNumberFormatter.format(hours)}h` : null,
    minutes > 0 ? `${activityNumberFormatter.format(minutes)}m` : null,
  ]
    .filter(Boolean)
    .join(" ");
  return `${duration} ${day.timedMatches === day.count ? "played" : "tracked"}`;
}

function activityFormatMix(day: DailyActivity): string {
  return [
    day.limited > 0 ? `${activityNumberFormatter.format(day.limited)} Limited` : null,
    day.constructed > 0 ? `${activityNumberFormatter.format(day.constructed)} Constructed` : null,
  ]
    .filter(Boolean)
    .join(" · ");
}

function activityDayLabel(day: DailyActivity): string {
  const count = `${activityNumberFormatter.format(day.count)} match${day.count === 1 ? "" : "es"}`;
  if (day.count === 0) return `${activityFullDate(day.date)}: no matches played`;

  const record = `${activityNumberFormatter.format(day.wins)} wins, ${activityNumberFormatter.format(day.losses)} losses`;
  const unresolved = day.unknown > 0
    ? `, ${activityNumberFormatter.format(day.unknown)} unresolved`
    : "";
  return `${activityFullDate(day.date)}: ${count}, ${record}${unresolved}`;
}

function ActivityGraph({
  activity,
  total,
}: {
  activity: DailyActivity[];
  total: number;
}) {
  const firstDate = new Date(`${activity[0]?.date}T00:00:00`);
  const firstWeekday = Number.isNaN(firstDate.getTime()) ? 0 : firstDate.getDay();
  const weekCount = Math.ceil((firstWeekday + activity.length) / 7);
  const maxCount = Math.max(...activity.map((day) => day.count), 1);
  const gridColumns = `repeat(${weekCount}, minmax(11px, 1fr))`;
  const monthFormatter = new Intl.DateTimeFormat(undefined, { month: "short" });
  const monthLabels = activity.flatMap((day, index) => {
    const date = new Date(`${day.date}T00:00:00`);
    if (Number.isNaN(date.getTime()) || date.getDate() !== 1) return [];
    return [{
      column: Math.floor((firstWeekday + index) / 7) + 1,
      label: monthFormatter.format(date),
    }];
  });
  const [focusedIndex, setFocusedIndex] = useState(Math.max(activity.length - 1, 0));
  const [visibleIndex, setVisibleIndex] = useState<number | null>(null);
  const [position, setPosition] = useState<ActivityPopoverPosition | null>(null);
  const gridRef = useRef<HTMLDivElement | null>(null);
  const anchorRef = useRef<HTMLButtonElement | null>(null);
  const popoverRef = useRef<HTMLDivElement | null>(null);
  const hoverTimerRef = useRef<number | null>(null);
  const tooltipId = "activity-day-tooltip";
  const visibleDay = visibleIndex == null ? null : activity[visibleIndex];
  const visibleDuration = visibleDay ? activityDuration(visibleDay) : null;

  const clearHoverTimer = useCallback(() => {
    if (hoverTimerRef.current != null) {
      window.clearTimeout(hoverTimerRef.current);
      hoverTimerRef.current = null;
    }
  }, []);

  const updatePopoverPosition = useCallback(() => {
    const anchor = anchorRef.current;
    const popover = popoverRef.current;
    if (!anchor || !popover) return;

    const anchorRect = anchor.getBoundingClientRect();
    const popoverRect = popover.getBoundingClientRect();
    const viewportWidth = window.innerWidth || document.documentElement.clientWidth;
    const viewportHeight = window.innerHeight || document.documentElement.clientHeight;
    const maxLeft = Math.max(
      ACTIVITY_POPOVER_PADDING,
      viewportWidth - popoverRect.width - ACTIVITY_POPOVER_PADDING,
    );
    const left = Math.max(
      ACTIVITY_POPOVER_PADDING,
      Math.min(
        anchorRect.left + anchorRect.width / 2 - popoverRect.width / 2,
        maxLeft,
      ),
    );
    const fitsAbove =
      anchorRect.top - popoverRect.height - ACTIVITY_POPOVER_GAP >= ACTIVITY_POPOVER_PADDING;
    const placement = fitsAbove ? "above" : "below";
    const rawTop = fitsAbove
      ? anchorRect.top - popoverRect.height - ACTIVITY_POPOVER_GAP
      : anchorRect.bottom + ACTIVITY_POPOVER_GAP;
    const top = Math.max(
      ACTIVITY_POPOVER_PADDING,
      Math.min(
        rawTop,
        viewportHeight - popoverRect.height - ACTIVITY_POPOVER_PADDING,
      ),
    );
    const arrowLeft = Math.max(
      12,
      Math.min(anchorRect.left + anchorRect.width / 2 - left, popoverRect.width - 12),
    );

    setPosition({ top, left, arrowLeft, placement });
  }, []);

  const showDay = useCallback((index: number, anchor: HTMLButtonElement) => {
    clearHoverTimer();
    anchorRef.current = anchor;
    setVisibleIndex(index);
  }, [clearHoverTimer]);

  const showHoveredDay = useCallback((index: number, anchor: HTMLButtonElement) => {
    clearHoverTimer();
    anchorRef.current = anchor;
    if (visibleIndex != null) {
      setVisibleIndex(index);
      return;
    }
    hoverTimerRef.current = window.setTimeout(() => {
      hoverTimerRef.current = null;
      setVisibleIndex(index);
    }, ACTIVITY_HOVER_DELAY_MS);
  }, [clearHoverTimer, visibleIndex]);

  const hideHoveredDay = useCallback(() => {
    clearHoverTimer();
    const activeElement = document.activeElement;
    if (
      activeElement instanceof HTMLButtonElement
      && gridRef.current?.contains(activeElement)
    ) {
      const activeIndex = Number(activeElement.dataset.activityIndex);
      if (Number.isInteger(activeIndex)) {
        showDay(activeIndex, activeElement);
        return;
      }
    }
    anchorRef.current = null;
    setVisibleIndex(null);
    setPosition(null);
  }, [clearHoverTimer, showDay]);

  const focusDay = useCallback((index: number) => {
    const target = gridRef.current?.querySelector<HTMLButtonElement>(
      `[data-activity-index="${index}"]`,
    );
    target?.focus();
  }, []);

  useLayoutEffect(() => {
    if (visibleDay) updatePopoverPosition();
  }, [updatePopoverPosition, visibleDay]);

  useEffect(() => {
    if (!visibleDay) return undefined;
    const update = () => updatePopoverPosition();
    window.addEventListener("resize", update);
    window.addEventListener("scroll", update, true);
    return () => {
      window.removeEventListener("resize", update);
      window.removeEventListener("scroll", update, true);
    };
  }, [updatePopoverPosition, visibleDay]);

  useEffect(() => () => clearHoverTimer(), [clearHoverTimer]);

  return (
    <div className="activity-frame">
      <div className="activity-heatmap">
        <div className="activity-months" style={{ gridTemplateColumns: gridColumns }} aria-hidden="true">
          {monthLabels.map((month) => (
            <span key={`${month.column}-${month.label}`} style={{ gridColumn: month.column }}>
              {month.label}
            </span>
          ))}
        </div>
        <div className="activity-chart">
          <div className="activity-weekdays" aria-hidden="true">
            <span style={{ gridRow: 2 }}>Mon</span>
            <span style={{ gridRow: 4 }}>Wed</span>
            <span style={{ gridRow: 6 }}>Fri</span>
          </div>
          <div
            ref={gridRef}
            className="activity-grid"
            role="grid"
            aria-label={`${activityNumberFormatter.format(total)} match${total === 1 ? "" : "es"} played over the last ${ACTIVITY_DAYS} days. Use arrow keys to inspect days.`}
            aria-rowcount={7}
            aria-colcount={weekCount}
            style={{ gridTemplateColumns: gridColumns, aspectRatio: `${weekCount} / 7` }}
            onMouseLeave={hideHoveredDay}
            onBlur={(event) => {
              if (!event.currentTarget.contains(event.relatedTarget)) {
                anchorRef.current = null;
                setVisibleIndex(null);
                setPosition(null);
              }
            }}
          >
            {activity.map((day, index) => {
              const level = day.count === 0 ? 0 : Math.max(1, Math.ceil((day.count / maxCount) * 4));
              const positionInGrid = firstWeekday + index;
              const row = positionInGrid % 7;
              const column = Math.floor(positionInGrid / 7);
              return (
                <button
                  type="button"
                  role="gridcell"
                  key={day.date}
                  data-activity-index={index}
                  className={`activity-cell activity-cell--${level}`}
                  style={{
                    gridColumn: column + 1,
                    gridRow: row + 1,
                  }}
                  tabIndex={focusedIndex === index ? 0 : -1}
                  aria-label={activityDayLabel(day)}
                  aria-describedby={visibleIndex === index ? tooltipId : undefined}
                  onMouseEnter={(event) => showHoveredDay(index, event.currentTarget)}
                  onFocus={(event) => {
                    setFocusedIndex(index);
                    showDay(index, event.currentTarget);
                  }}
                  onKeyDown={(event) => {
                    let nextIndex = index;
                    if (event.key === "ArrowLeft" && index >= 7) nextIndex = index - 7;
                    else if (event.key === "ArrowRight" && index + 7 < activity.length) nextIndex = index + 7;
                    else if (event.key === "ArrowUp" && row > 0 && index > 0) nextIndex = index - 1;
                    else if (event.key === "ArrowDown" && row < 6 && index + 1 < activity.length) nextIndex = index + 1;
                    else return;

                    event.preventDefault();
                    focusDay(nextIndex);
                  }}
                />
              );
            })}
          </div>
        </div>
      </div>
      <div className="activity-footer">
        <span>{activity[0]?.label}</span>
        <div className="activity-legend" aria-label="Activity intensity from less to more">
          <span>Less</span>
          {[0, 1, 2, 3, 4].map((level) => (
            <span key={level} className={`activity-cell activity-cell--${level}`} aria-hidden="true" />
          ))}
          <span>More</span>
        </div>
        <span>Today</span>
      </div>
      {visibleDay && typeof document !== "undefined"
        ? createPortal(
            <div
              ref={popoverRef}
              id={tooltipId}
              className="activity-popover"
              role="tooltip"
              data-placement={position?.placement ?? "above"}
              style={{
                top: position?.top ?? 0,
                left: position?.left ?? 0,
                visibility: position ? "visible" : "hidden",
                "--activity-popover-arrow-left": `${position?.arrowLeft ?? 20}px`,
              } as CSSProperties}
            >
              <p className="activity-popover-date">{activityFullDate(visibleDay.date)}</p>
              {visibleDay.count === 0 ? (
                <p className="activity-popover-empty">No matches played</p>
              ) : (
                <>
                  <div className="activity-popover-count">
                    <strong>{activityNumberFormatter.format(visibleDay.count)}</strong>
                    <span>match{visibleDay.count === 1 ? "" : "es"}</span>
                  </div>
                  <div className="activity-popover-record">
                    <strong>
                      {activityNumberFormatter.format(visibleDay.wins)}W –{" "}
                      {activityNumberFormatter.format(visibleDay.losses)}L
                    </strong>
                    {visibleDay.wins + visibleDay.losses > 0 ? (
                      <span>
                        {activityRateFormatter.format(
                          visibleDay.wins / (visibleDay.wins + visibleDay.losses),
                        )}
                      </span>
                    ) : null}
                  </div>
                  {visibleDay.unknown > 0 ? (
                    <p className="activity-popover-unresolved">
                      {activityNumberFormatter.format(visibleDay.unknown)} unresolved
                    </p>
                  ) : null}
                  <div className="activity-popover-details">
                    {visibleDuration ? <span>{visibleDuration}</span> : null}
                    <span>{activityFormatMix(visibleDay)}</span>
                  </div>
                </>
              )}
            </div>,
            document.body,
          )
        : null}
    </div>
  );
}

function syncSummary(
  status: RuntimeStatus | undefined,
  lastMatchAt: string | undefined,
): { head: string; sub: string; live: boolean } {
  if (!status) return { head: "…", sub: "checking status", live: false };

  const importedAt = status.lastImport?.completedAt ?? "";
  const tickedAt = status.liveLastTickAt ?? "";
  const lastSyncedAt = [importedAt, tickedAt]
    .filter(Boolean)
    .sort((a, b) => new Date(a).getTime() - new Date(b).getTime())
    .pop();

  if (status.liveRunning) {
    return {
      head: "Live tracker on",
      sub: lastSyncedAt ? `last activity ${formatRelativeTime(lastSyncedAt)}` : "waiting for activity",
      live: true,
    };
  }
  if (lastSyncedAt) {
    return { head: `Synced ${formatRelativeTime(lastSyncedAt)}`, sub: "live tracker off", live: false };
  }
  // No sync this session; fall back to how fresh the stored data is.
  if (lastMatchAt) {
    return { head: `Data through ${formatRelativeTime(lastMatchAt)}`, sub: "live tracker off", live: false };
  }
  return { head: "Never synced", sub: "live tracker off", live: false };
}

function draftRecordLabel(draft: DraftSession): string | null {
  if (draft.wins == null || draft.losses == null) return null;
  return `${draft.wins}W–${draft.losses}L`;
}

export function OverviewPage() {
  const { data, isLoading, error } = useQuery({
    queryKey: ["overview"],
    queryFn: api.overview,
  });
  const matchesQuery = useQuery({
    queryKey: ["matches", 500],
    queryFn: () => api.matches(500),
  });
  const decksQuery = useQuery({
    queryKey: ["decks", "all"],
    queryFn: () => api.decks("all"),
  });
  const draftsQuery = useQuery({
    queryKey: ["drafts"],
    queryFn: api.drafts,
  });
  const statusQuery = useQuery({
    queryKey: ["runtime-status"],
    queryFn: api.runtimeStatus,
    refetchInterval: 30_000,
  });

  const allMatches = useMemo(
    () => sortMatchesDesc(matchesQuery.data ?? data?.recent ?? []),
    [matchesQuery.data, data?.recent],
  );
  const drafts = useMemo(
    () =>
      [...(draftsQuery.data ?? [])].sort(
        (a, b) => new Date(b.startedAt).getTime() - new Date(a.startedAt).getTime(),
      ),
    [draftsQuery.data],
  );
  const { lookup: setLookup } = useEventSets([
    ...(data?.recent ?? []).map((match) => match.eventName),
    ...drafts.map((draft) => draft.eventName),
  ]);

  const splits = useMemo(() => splitRecords(allMatches), [allMatches]);
  const activity = useMemo(() => dailyActivity(allMatches, ACTIVITY_DAYS), [allMatches]);

  if (isLoading) return <StatusMessage>Loading overview…</StatusMessage>;
  if (error) return <StatusMessage tone="error">{(error as Error).message}</StatusMessage>;
  if (!data) return <StatusMessage>No data.</StatusMessage>;

  const sync = syncSummary(statusQuery.data, allMatches[0]?.startedAt);

  const header = (
    <section className="overview-header">
      {data.playerName ? (
        <div className="overview-identity" aria-label="Tracked username">
          <p>Username</p>
          <h2>{data.playerName}</h2>
        </div>
      ) : (
        <div />
      )}
      <div className="overview-sync" aria-label="Data status">
        <p>Data status</p>
        <div className="overview-sync-line">
          <span className={`sync-dot ${sync.live ? "is-live" : ""}`} aria-hidden="true" />
          <strong>{sync.head}</strong>
        </div>
        <small>{sync.sub}</small>
      </div>
    </section>
  );

  if (data.totalMatches === 0) {
    return (
      <div className="stack-lg">
        {header}
        <section className="panel empty-panel">
          <h3>No matches tracked yet</h3>
          <p>
            Point the tracker at your Arena log and run an import — matches, decks, drafts, and
            rank history will light this page up.
          </p>
          <Link to="/settings" className="control-button">
            Open Settings
          </Link>
        </section>
      </div>
    );
  }

  const unknownCount = data.totalMatches - data.wins - data.losses;
  const overallRate = recordWinRate({ wins: data.wins, losses: data.losses });
  const form = recentForm(allMatches, FORM_WINDOW);
  const formRate = recordWinRate(form);
  const streak = currentStreak(allMatches);
  const averages = matchAverages(allMatches);

  const activityTotal = activity.reduce((sum, day) => sum + day.count, 0);
  const lastPlayedAt = allMatches[0]?.startedAt;

  const topDecks = [...(decksQuery.data ?? [])]
    .filter((deck) => deck.matches > 0)
    .sort((a, b) => b.matches - a.matches)
    .slice(0, 4);

  const draftsWithRecord = drafts.filter((draft) => draft.wins != null && draft.losses != null);
  const avgDraftWins =
    draftsWithRecord.length > 0
      ? draftsWithRecord.reduce((sum, draft) => sum + (draft.wins ?? 0), 0) / draftsWithRecord.length
      : null;
  const recentDrafts = drafts.slice(0, 3);

  const shownMatches = data.recent.slice(0, RECENT_MATCH_COUNT);
  const averagesLabel = [
    averages.seconds != null ? formatDuration(averages.seconds) : null,
    averages.turns != null ? `${averages.turns} turns` : null,
  ]
    .filter(Boolean)
    .join(" · ");

  return (
    <div className="stack-lg">
      {header}

      <section className="metrics-grid">
        <article className="metric-card">
          <p>Record</p>
          <div className="metric-value">
            {data.wins}W – {data.losses}L
          </div>
          <small className="metric-sub">
            {data.totalMatches} matches{unknownCount > 0 ? ` · ${unknownCount} unresolved` : ""}
          </small>
        </article>
        <article className={`metric-card metric-card--toned metric-card--${toneFor(overallRate)}`}>
          <p>Win Rate</p>
          <div className="metric-value">{overallRate == null ? "—" : pct(overallRate)}</div>
          <small className="metric-sub">all decided matches</small>
        </article>
        <article className={`metric-card metric-card--toned metric-card--${toneFor(formRate)}`}>
          <p>Recent Form</p>
          <div className="metric-value">{formRate == null ? "—" : pct(formRate)}</div>
          <small className="metric-sub">
            last {form.wins + form.losses} · {form.wins}W – {form.losses}L
          </small>
        </article>
        <article
          className={`metric-card metric-card--toned metric-card--${
            streak ? (streak.result === "win" ? "positive" : "negative") : "neutral"
          }`}
        >
          <p>Streak</p>
          <div className="metric-value">{streak ? `${streak.result === "win" ? "W" : "L"}${streak.length}` : "—"}</div>
          <small className="metric-sub">
            {lastPlayedAt ? `last played ${formatRelativeTime(lastPlayedAt)}` : "current streak"}
          </small>
        </article>
      </section>

      <section className="panel activity-panel">
        <div className="panel-head">
          <h3>Activity</h3>
          <p>
            {activityTotal} match{activityTotal === 1 ? "" : "es"} · last {ACTIVITY_DAYS} days
          </p>
        </div>
        <ActivityGraph activity={activity} total={activityTotal} />
        {activityTotal === 0 ? (
          <p className="state activity-empty">
            No matches in the last {ACTIVITY_DAYS} days
            {lastPlayedAt ? ` — last played ${formatRelativeTime(lastPlayedAt)}` : ""}.
          </p>
        ) : null}
      </section>

      <RankProgressPanel />

      <section className="panel">
        <div className="panel-head">
          <h3>Performance Splits</h3>
          <p>{splits.constructed.wins + splits.constructed.losses + splits.limited.wins + splits.limited.losses} decided matches</p>
        </div>
        <div className="splits-grid">
          <div className="split-group">
            <span className="split-group-title">Format</span>
            <SplitRow label="Constructed" record={splits.constructed} />
            <SplitRow label="Limited" record={splits.limited} />
          </div>
          <div className="split-group">
            <span className="split-group-title">Initiative</span>
            <SplitRow label="On the play" record={splits.play} />
            <SplitRow label="On the draw" record={splits.draw} />
          </div>
          <div className="split-group">
            <span className="split-group-title">Match Type</span>
            <SplitRow label="Best of one" record={splits.bo1} />
            <SplitRow label="Best of three" record={splits.bo3} />
          </div>
        </div>
      </section>

      <div className="overview-columns">
        <section className="panel">
          <div className="panel-head">
            <h3>Top Decks</h3>
            <Link to="/decks" className="text-link">
              All decks
            </Link>
          </div>
          {topDecks.length > 0 ? (
            <div className="list">
              {topDecks.map((deck) => {
                const rate = recordWinRate({ wins: deck.wins, losses: deck.losses });
                return (
                  <Link
                    className="list-row list-row--compact"
                    key={deck.deckId}
                    to={`/decks/${deck.deckId}`}
                  >
                    <div className="list-main">
                      <p className="list-title">{deck.deckName}</p>
                      <p className="list-subtitle">
                        <EventLabel eventName={deck.eventName} lookup={setLookup} fallback={deck.format} />
                        {" · "}
                        {deck.matches} match{deck.matches === 1 ? "" : "es"}
                      </p>
                    </div>
                    <div className="list-stat">
                      <strong>{rate == null ? "—" : pct(rate)}</strong>
                      <span className="split-bar" aria-hidden="true">
                        {rate != null ? (
                          <span
                            className="split-bar-fill"
                            style={{ width: `${Math.max(rate * 100, 2)}%` }}
                          />
                        ) : null}
                      </span>
                      <small>
                        {deck.wins}W – {deck.losses}L
                      </small>
                    </div>
                  </Link>
                );
              })}
            </div>
          ) : (
            <p className="state">No decks tracked yet.</p>
          )}
        </section>

        <section className="panel">
          <div className="panel-head">
            <h3>Drafts</h3>
            <Link to="/drafts" className="text-link">
              All drafts
            </Link>
          </div>
          {recentDrafts.length > 0 ? (
            <>
              <div className="overview-chip-row">
                <div className="rank-chip">
                  <span>Tracked</span>
                  <strong>{drafts.length}</strong>
                </div>
                <div className="rank-chip">
                  <span>Avg Wins</span>
                  <strong>{avgDraftWins == null ? "—" : avgDraftWins.toFixed(1)}</strong>
                </div>
              </div>
              <div className="list">
                {recentDrafts.map((draft) => {
                  const record = draftRecordLabel(draft);
                  return (
                    <Link
                      className="list-row list-row--compact"
                      key={draft.id}
                      to={`/drafts/${draft.id}`}
                    >
                      <div className="list-main">
                        <p className="list-title">
                          <EventLabel eventName={draft.eventName} lookup={setLookup} />
                        </p>
                        <p className="list-subtitle">
                          {[
                            draft.startedAt ? formatRelativeTime(draft.startedAt) : null,
                            `${draft.picks} pick${draft.picks === 1 ? "" : "s"}`,
                          ]
                            .filter(Boolean)
                            .join(" · ")}
                        </p>
                      </div>
                      <div className="list-stat">
                        {record ? <strong>{record}</strong> : <small>no record</small>}
                      </div>
                    </Link>
                  );
                })}
              </div>
            </>
          ) : (
            <p className="state">No drafts tracked yet.</p>
          )}
        </section>
      </div>

      <section className="panel">
        <div className="panel-head">
          <h3>Recent Matches</h3>
          <Link to="/matches" className="text-link">
            Open full history
          </Link>
        </div>
        <div className="list">
          {shownMatches.map((match) => {
            const parsedEvent = parseEventName(match.eventName);
            const eventSet = setLookup(parsedEvent.setCode);
            const queueLabel = eventDisplayName(parsedEvent, eventSet);
            const title = match.deckName || queueLabel;
            const timingParts: string[] = [];
            const duration = formatDuration(match.secondsCount ?? undefined);

            if (duration !== "-") {
              timingParts.push(duration);
            }
            if (match.turnCount != null) {
              timingParts.push(`${match.turnCount} turn${match.turnCount === 1 ? "" : "s"}`);
            }

            return (
              <Link
                className={`list-row list-row--${match.result}`}
                key={match.id}
                to={`/matches/${match.id}`}
              >
                <div className="list-main">
                  <p className="list-title">{title}</p>
                  {match.deckName ? (
                    <p className="list-subtitle">
                      <EventLabel eventName={match.eventName} lookup={setLookup} />
                    </p>
                  ) : null}
                </div>

                <dl className="list-meta" aria-label="Recent match summary">
                  <div className="list-meta-item">
                    <dt>Opponent</dt>
                    <dd>{match.opponent || "Unknown"}</dd>
                  </div>
                  <div className="list-meta-item">
                    <dt>Started</dt>
                    <dd title={formatDateTime(match.startedAt)}>
                      {formatRelativeTime(match.startedAt)}
                    </dd>
                  </div>
                  <div className="list-meta-item list-meta-item--colors">
                    <dt>Colors</dt>
                    <dd>
                      <MatchDeckColors
                        className="match-deck-colors-list"
                        hideUnknown
                        deckColors={match.deckColors}
                        deckColorsKnown={match.deckColorsKnown}
                        opponentDeckColors={match.opponentDeckColors}
                        opponentDeckColorsKnown={match.opponentDeckColorsKnown}
                      />
                    </dd>
                  </div>
                </dl>

                <div className="list-right">
                  <ResultPill result={match.result} />
                  <small>{timingParts.join(" · ") || "Timing unavailable"}</small>
                </div>
              </Link>
            );
          })}
        </div>
        <div className="panel-foot">
          <span>
            Showing {shownMatches.length} of {data.totalMatches}
          </span>
          {averagesLabel ? <span>Avg match {averagesLabel}</span> : null}
        </div>
      </section>
    </div>
  );
}
