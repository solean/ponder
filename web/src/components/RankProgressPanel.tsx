import ReactECharts from "echarts-for-react";
import { useQuery } from "@tanstack/react-query";
import { useEffect, useId, useMemo, useState, type KeyboardEvent } from "react";

import { api } from "../lib/api";
import { eventDisplayName, parseEventName } from "../lib/events";
import { formatDateTime } from "../lib/format";
import { rankBadgeUrl } from "../lib/rankBadgeAssets";
import { useEventSets } from "../lib/useEventSets";
import { RankSymbol } from "./RankSymbol";
import {
  buildGraphPoints,
  fillMissingRankClasses,
  LADDER_CONFIG,
  rankPromotionsFor,
  seasonOrdinalsFor,
  tierLabelAt,
  type Ladder,
  type RankProgressSeries,
  type SeasonView,
} from "../lib/rankProgress";
import { CHART_THEME_TOKENS } from "../lib/chartTheme";
import { useTheme } from "../lib/theme";

function handleSegmentedKeyDown<T extends string>(
  event: KeyboardEvent<HTMLButtonElement>,
  value: T,
  options: readonly T[],
  onChange: (next: T) => void,
) {
  const currentIndex = options.indexOf(value);
  if (currentIndex === -1) return;

  switch (event.key) {
    case "ArrowLeft":
    case "ArrowUp":
      event.preventDefault();
      onChange(options[(currentIndex + options.length - 1) % options.length]);
      break;
    case "ArrowRight":
    case "ArrowDown":
      event.preventDefault();
      onChange(options[(currentIndex + 1) % options.length]);
      break;
    case "Home":
      event.preventDefault();
      onChange(options[0]);
      break;
    case "End":
      event.preventDefault();
      onChange(options[options.length - 1]);
      break;
    default:
      break;
  }
}

function formatSnapshotLabel(count: number): string {
  return `${count} ranked snapshot${count === 1 ? "" : "s"}`;
}

function describeSeries(series: RankProgressSeries): string {
  if (series.seasonView === "all") {
    return `All seasons • ${formatSnapshotLabel(series.points.length)} across ${series.seasonOrdinals.length} season${series.seasonOrdinals.length === 1 ? "" : "s"}`;
  }

  return `Season ${series.seasonOrdinal} • ${formatSnapshotLabel(series.points.length)}`;
}

function describeSelection(seasonView: SeasonView): string {
  switch (seasonView) {
    case "previous":
      return "Review the most recent completed season for this ladder";
    case "all":
      return "View every recorded season on one timeline";
    default:
      return "Track how your ladder standing moves over time";
  }
}

function emptyStateMessage(seasonView: SeasonView): string {
  switch (seasonView) {
    case "previous":
      return "No previous season snapshots available for this ladder yet.";
    case "all":
      return "No rank snapshots available for this ladder yet.";
    default:
      return "No rank snapshots available for the current season yet.";
  }
}

type RankProgressPanelProps =
  | { ladder?: never; seasonView?: never }
  | { ladder: Ladder; seasonView: SeasonView };

export function RankProgressPanel(props: RankProgressPanelProps = {}) {
  const tabBaseId = useId();
  const { mode, scheme } = useTheme();
  const [localLadder, setLocalLadder] = useState<Ladder>("constructed");
  const [localSeasonView, setLocalSeasonView] = useState<SeasonView>("current");
  const isControlled = props.ladder != null;
  const ladder = props.ladder ?? localLadder;
  const seasonView = props.seasonView ?? localSeasonView;
  const { data, isLoading, error } = useQuery({
    queryKey: ["rank-history"],
    queryFn: api.rankHistory,
  });
  const { lookup: setLookup } = useEventSets((data ?? []).map((point) => point.eventName));
  const panelId = `${tabBaseId}-panel`;
  const headingId = `${tabBaseId}-heading`;
  const ladderOptions = ["constructed", "limited"] as const satisfies readonly Ladder[];

  // Arena often omits the rank-class string; anchor missing classes to the
  // explicit ones within each season so tiers label correctly.
  const filledData = useMemo(() => (data ? fillMissingRankClasses(data) : null), [data]);

  const availableSeasons = useMemo(
    () => (filledData ? seasonOrdinalsFor(filledData, ladder) : []),
    [filledData, ladder],
  );
  const hasPreviousSeason = availableSeasons.length > 1;
  const currentSeasonOrdinal = availableSeasons[availableSeasons.length - 1];
  const previousSeasonOrdinal = availableSeasons[availableSeasons.length - 2];

  useEffect(() => {
    if (!isControlled && localSeasonView === "previous" && !hasPreviousSeason) {
      setLocalSeasonView("current");
    }
  }, [hasPreviousSeason, isControlled, localSeasonView]);

  const series = useMemo(
    () => (filledData ? buildGraphPoints(filledData, ladder, seasonView) : null),
    [filledData, ladder, seasonView],
  );
  const latestPoint = series ? series.points[series.points.length - 1] : null;
  const firstPoint = series?.points[0];
  const currentRank = latestPoint ? latestPoint.rankLabel : "Unranked";
  const currentRecord = series?.record ? `${series.record.wins}W-${series.record.losses}L` : null;
  const rankMoved = firstPoint != null && latestPoint != null && firstPoint.rankLabel !== latestPoint.rankLabel;
  const promotions = useMemo(
    () => (series ? rankPromotionsFor(series.points, ladder) : []),
    [ladder, series],
  );
  const chartTheme = CHART_THEME_TOKENS[`${scheme}-${mode}`];

  // A sparse line reads as noise unless it contains a rank-up worth showing.
  const hasChartableTrend = (series?.points.length ?? 0) >= 3 || promotions.length > 0;

  const chartOption = useMemo(
    () =>
      series && latestPoint
        ? {
          backgroundColor: "transparent",
          animationDuration: 320,
          grid: { left: 72, right: 28, top: 28, bottom: 46 },
          tooltip: {
            trigger: "axis",
            backgroundColor: chartTheme.tooltipBackground,
            borderColor: chartTheme.tooltipBorder,
            textStyle: {
              color: chartTheme.tooltipText,
              fontFamily: "IBM Plex Mono, Menlo, monospace",
              fontSize: 12,
            },
            axisPointer: {
              type: "line",
              lineStyle: { color: chartTheme.accent, opacity: 0.26 },
            },
            formatter: (params: any) => {
              const entries = Array.isArray(params) ? params : [params];
              const point =
                entries.find((entry: any) => entry?.seriesName === "Rank progress")?.data ??
                entries[0]?.data;
              if (!point) return "";
              const resultLabel =
                point.result === "win" ? "Win" : point.result === "loss" ? "Loss" : "Unknown";
              const timestamp = point.observedAt || point.endedAt;
              const parsedEvent = parseEventName(point.eventName);
              const eventLabel = eventDisplayName(parsedEvent, setLookup(parsedEvent.setCode));
              return [
                `<div style="display:grid;gap:4px;">`,
                `<strong>${point.rankLabel}</strong>`,
                point.isPromotion
                  ? `<span style="color:${chartTheme.promotion};font-weight:600;">Rank increased to ${point.rankClass}</span>`
                  : "",
                `<span>Season ${point.seasonOrdinal} • Match ${point.matchNumber} • ${resultLabel}</span>`,
                `<span>${eventLabel} vs ${point.opponent || "Unknown"}</span>`,
                `<span>${formatDateTime(timestamp)}</span>`,
                `</div>`,
              ].join("");
            },
          },
          xAxis: {
            type: "value",
            min: 1,
            max: Math.max(series.points.length, 1),
            splitNumber: Math.min(Math.max(Math.floor(series.points.length / 4), 4), 8),
            axisLine: { lineStyle: { color: chartTheme.axisLine } },
            axisTick: { show: false },
            axisLabel: {
              color: chartTheme.axisText,
              fontFamily: "IBM Plex Mono, Menlo, monospace",
              formatter: (value: number) => {
                if (value === 1 || value === series.points.length || value % 5 === 0) {
                  return `${Math.round(value)}`;
                }
                return "";
              },
            },
            splitLine: { show: false },
          },
          yAxis: {
            type: "value",
            min: Math.max(0, Math.floor(Math.min(...series.points.map((point) => point.score)))),
            max: Math.min(
              LADDER_CONFIG[ladder].tiers.length - 0.001,
              Math.ceil(Math.max(...series.points.map((point) => point.score))) + 0.25,
            ),
            interval: 1,
            axisLine: { show: false },
            axisTick: { show: false },
            axisLabel: {
              color: chartTheme.axisText,
              fontFamily: "IBM Plex Mono, Menlo, monospace",
              margin: 14,
              formatter: (value: number) => tierLabelAt(value, ladder),
            },
            splitLine: {
              lineStyle: {
                color: chartTheme.splitLine,
                type: "solid",
              },
            },
          },
          series: [
            {
              name: "Rank progress",
              type: "line",
              data: series.points.map((point) => ({
                ...point,
                isPromotion: promotions.includes(point),
                value: [point.matchNumber, point.score],
              })),
              smooth: false,
              showSymbol: true,
              symbol: "circle",
              symbolSize: 8,
              lineStyle: {
                color: chartTheme.accent,
                width: 2.4,
                shadowBlur: 10,
                shadowColor: chartTheme.accentGlow,
              },
              itemStyle: {
                color: chartTheme.accent,
                borderColor: chartTheme.pointBorder,
                borderWidth: 1.5,
                shadowBlur: 8,
                shadowColor: chartTheme.accentGlow,
              },
              areaStyle: {
                color: {
                  type: "linear",
                  x: 0,
                  y: 0,
                  x2: 0,
                  y2: 1,
                  colorStops: [
                    { offset: 0, color: chartTheme.accentSoft },
                    { offset: 1, color: chartTheme.accentFaint },
                  ],
                },
              },
            },
            {
              name: "Current rank",
              type: "scatter",
              data: [
                {
                  ...latestPoint,
                  value: [latestPoint.matchNumber, latestPoint.score],
                },
              ],
              symbolSize: 12,
              itemStyle: {
                color: chartTheme.accent,
                borderColor: chartTheme.hoverBorder,
                borderWidth: 2,
                shadowBlur: 18,
                shadowColor: chartTheme.accentGlow,
              },
              z: 5,
            },
            {
              name: "Rank increase",
              type: "scatter",
              data: promotions.map((point) => ({
                ...point,
                value: [point.matchNumber, point.score],
              })),
              symbol: "circle",
              symbolSize: 11,
              itemStyle: {
                color: chartTheme.promotion,
                borderColor: chartTheme.hoverBorder,
                borderWidth: 2,
                shadowBlur: 12,
                shadowColor: chartTheme.promotionGlow,
              },
              z: 7,
            },
            {
              name: "Rank badge",
              type: "scatter",
              data: promotions.flatMap((point) => {
                const badgeUrl = rankBadgeUrl(ladder, point.resolvedRankClass ?? "");
                return badgeUrl
                  ? [
                    {
                      ...point,
                      value: [point.matchNumber, point.score],
                      symbol: `image://${badgeUrl}`,
                    },
                  ]
                  : [];
              }),
              symbolSize: 48,
              symbolOffset: [0, 32],
              tooltip: { show: false },
              silent: true,
              z: 8,
            },
          ],
        }
        : null,
    [chartTheme, ladder, latestPoint, promotions, series, setLookup],
  );

  const readyState =
    series && latestPoint && chartOption
      ? {
          chartOption,
          latestPoint,
          firstPoint,
          series,
        }
      : null;

  return (
    <section className="panel rank-panel">
      <div className="panel-head rank-toolbar">
        <div>
          <h3 id={headingId}>Rank Progress</h3>
          <p>{series ? describeSeries(series) : describeSelection(seasonView)}</p>
        </div>
        {!isControlled ? (
          <div className="rank-controls" role="group" aria-label="Rank trend filters">
            <div className="tabs rank-toggle" role="group" aria-label="Trend ladder">
              {ladderOptions.map((value) => (
                <button
                  key={value}
                  type="button"
                  aria-pressed={ladder === value}
                  className={`tab rank-toggle-button ${ladder === value ? "is-active" : ""}`}
                  onClick={() => setLocalLadder(value)}
                  onKeyDown={(event) =>
                    handleSegmentedKeyDown(event, value, ladderOptions, setLocalLadder)
                  }
                >
                  {LADDER_CONFIG[value].label}
                </button>
              ))}
            </div>
            <label className="rank-season-select">
              <span>Season</span>
              <select
                value={seasonView}
                onChange={(event) => setLocalSeasonView(event.target.value as SeasonView)}
              >
                <option value="all">All seasons</option>
                <option value="current">
                  {currentSeasonOrdinal == null
                    ? "Current season"
                    : `Season ${currentSeasonOrdinal}`}
                </option>
                {hasPreviousSeason ? (
                  <option value="previous">Season {previousSeasonOrdinal}</option>
                ) : null}
              </select>
            </label>
          </div>
        ) : null}
      </div>

      {readyState ? (
        <div className="rank-summary">
          <div className="rank-chip rank-chip--selected">
            <span>{LADDER_CONFIG[ladder].label} · Trend</span>
            <div className="rank-chip-value">
              <RankSymbol ladder={ladder} rank={readyState.series.latestState} />
              <strong>{currentRank}</strong>
            </div>
          </div>
          {rankMoved ? (
            <div className="rank-chip">
              <span>{seasonView === "all" ? "Span" : "Path"}</span>
              <strong>
                {`${readyState.firstPoint?.rankLabel} to ${readyState.latestPoint.rankLabel}`}
              </strong>
            </div>
          ) : null}
          {currentRecord ? (
            <div className="rank-chip">
              <span>{seasonView === "all" ? "Total Record" : "Season Record"}</span>
              <strong>{currentRecord}</strong>
            </div>
          ) : null}
        </div>
      ) : null}

      <div
        className={`rank-chart-frame ${readyState && !hasChartableTrend ? "rank-chart-frame--sparse" : ""}`}
        id={panelId}
        role="region"
        aria-labelledby={headingId}
      >
        {isLoading ? <p className="state">Loading ladder data…</p> : null}
        {error ? <p className="state error">{(error as Error).message}</p> : null}
        {!isLoading && !error && !readyState ? (
          <p className="state">{emptyStateMessage(seasonView)}</p>
        ) : null}
        {readyState && !hasChartableTrend ? (
          <p className="state">
            {`Only ${formatSnapshotLabel(readyState.series.points.length)} so far — the trend chart unlocks at 3.`}
          </p>
        ) : null}
        {readyState && hasChartableTrend ? (
          <ReactECharts
            key={`${ladder}-${seasonView}-${scheme}-${mode}`}
            option={readyState.chartOption}
            style={{ height: 320 }}
          />
        ) : null}
      </div>
    </section>
  );
}
