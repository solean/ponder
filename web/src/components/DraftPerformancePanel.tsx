import ReactECharts from "echarts-for-react";
import { useId, useMemo, useState } from "react";

import { CHART_THEME_TOKENS } from "../lib/chartTheme";
import {
  buildDraftPerformance,
  draftPerformanceOptions,
  type DraftPerformancePoint,
} from "../lib/draftPerformance";
import { eventDisplayName, parseEventName } from "../lib/events";
import { pct } from "../lib/format";
import { useTheme } from "../lib/theme";
import type { DraftSession } from "../lib/types";
import type { SetLookup } from "../lib/useEventSets";

const MONO_FONT = "IBM Plex Mono, Menlo, monospace";
const MIN_CHART_EVENTS = 3;

const shortDate = new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric" });
const fullDate = new Intl.DateTimeFormat(undefined, { dateStyle: "medium" });

function recordLabel(wins: number, losses: number): string {
  return `${wins}–${losses}`;
}

type SeriesSpec = {
  name: string;
  value: (point: DraftPerformancePoint) => number | null;
  color: string;
  dashed: boolean;
  width: number;
};

/**
 * Rolling event win rate across draft sessions: every finished event adds a
 * point, and each line pools match results over a trailing window so a streak
 * shows up in "Last 10" long before it moves the all-time rate.
 */
export function DraftPerformancePanel({
  drafts,
  setLookup,
}: {
  drafts: DraftSession[];
  setLookup: SetLookup;
}) {
  const headingId = useId();
  const { mode, scheme } = useTheme();
  const chartTheme = CHART_THEME_TOKENS[`${scheme}-${mode}`];
  const [setCode, setSetCode] = useState<string | null>(null);
  const [format, setFormat] = useState<string | null>(null);

  const options = useMemo(() => draftPerformanceOptions(drafts), [drafts]);
  const performance = useMemo(
    () => buildDraftPerformance(drafts, { setCode, format }),
    [drafts, setCode, format],
  );
  const { points } = performance;
  const latest = points[points.length - 1];

  const chartOption = useMemo(() => {
    if (points.length < MIN_CHART_EVENTS) return null;

    // Short windows are noisy, so the longest line gets the accent and the
    // shortest recedes; dashes keep them apart without relying on color.
    const allSpecs: SeriesSpec[] = [
      { name: "Last 10 Events", value: (p) => p.rolling[10], color: chartTheme.axisText, dashed: true, width: 1.6 },
      { name: "Last 50 Events", value: (p) => p.rolling[50], color: chartTheme.contrast, dashed: false, width: 2 },
      { name: "All Events", value: (p) => p.all, color: chartTheme.accent, dashed: false, width: 2.4 },
    ];
    const specs = allSpecs.filter((spec) => points.some((point) => spec.value(point) != null));

    return {
      backgroundColor: "transparent",
      animationDuration: 320,
      aria: {
        enabled: true,
        description: "Pooled match win rate over trailing windows of draft events.",
      },
      grid: { left: 56, right: 64, top: 44, bottom: 40 },
      legend: {
        // One row at any width; wrapping would push the legend into the plot.
        type: "scroll",
        top: 6,
        icon: "roundRect",
        itemWidth: 14,
        itemHeight: 4,
        data: specs.map((spec) => spec.name),
        textStyle: { color: chartTheme.axisText, fontFamily: MONO_FONT, fontSize: 11 },
      },
      tooltip: {
        trigger: "axis",
        backgroundColor: chartTheme.tooltipBackground,
        borderColor: chartTheme.tooltipBorder,
        textStyle: { color: chartTheme.tooltipText, fontFamily: MONO_FONT, fontSize: 12 },
        axisPointer: { type: "line", lineStyle: { color: chartTheme.accent, opacity: 0.26 } },
        formatter: (params: any) => {
          const entries = Array.isArray(params) ? params : [params];
          const point = points[entries[0]?.dataIndex ?? -1];
          if (!point) return "";
          const parsed = parseEventName(point.eventName);
          const rows = entries
            .filter((entry: any) => entry.value != null)
            .map((entry: any) => `<span>${entry.marker}${entry.seriesName}: ${pct(entry.value)}</span>`);
          return [
            `<div style="display:grid;gap:4px;">`,
            `<strong>Event ${point.eventNumber}${point.dateValue != null ? ` · ${fullDate.format(point.dateValue)}` : ""}</strong>`,
            `<span>${eventDisplayName(parsed, setLookup(parsed.setCode))} · ${recordLabel(point.wins, point.losses)}${point.trophy ? " · Trophy" : ""}</span>`,
            ...rows,
            `</div>`,
          ].join("");
        },
      },
      xAxis: {
        type: "category",
        boundaryGap: false,
        data: points.map((point) => String(point.eventNumber)),
        axisLine: { lineStyle: { color: chartTheme.axisLine } },
        axisTick: { show: false },
        axisLabel: {
          color: chartTheme.axisText,
          fontFamily: MONO_FONT,
          hideOverlap: true,
          formatter: (_value: string, index: number) => {
            const dateValue = points[index]?.dateValue;
            return dateValue == null ? "" : shortDate.format(dateValue);
          },
        },
        splitLine: { show: false },
      },
      yAxis: {
        type: "value",
        // Snap to 10% gridlines with a little headroom so lines never touch the frame.
        min: (extent: { min: number }) => Math.max(0, Math.floor((extent.min - 0.02) * 10) / 10),
        max: (extent: { max: number }) => Math.min(1, Math.ceil((extent.max + 0.02) * 10) / 10),
        interval: 0.1,
        axisLine: { show: false },
        axisTick: { show: false },
        axisLabel: {
          color: chartTheme.axisText,
          fontFamily: MONO_FONT,
          formatter: (value: number) => `${Math.round(value * 100)}%`,
        },
        splitLine: { lineStyle: { color: chartTheme.axisLine, type: "dashed" } },
      },
      series: specs.map((spec, index) => ({
        name: spec.name,
        type: "line",
        data: points.map((point) => spec.value(point)),
        smooth: false,
        showSymbol: false,
        symbol: "circle",
        symbolSize: 8,
        connectNulls: false,
        color: spec.color,
        lineStyle: { color: spec.color, width: spec.width, type: spec.dashed ? "dashed" : "solid" },
        emphasis: { focus: "series" },
        endLabel: {
          show: true,
          color: chartTheme.tooltipText,
          fontFamily: MONO_FONT,
          fontSize: 11,
          formatter: (entry: any) => (entry.value == null ? "" : pct(entry.value)),
        },
        labelLayout: { moveOverlap: "shiftY" },
        z: 2 + index,
        ...(index === specs.length - 1
          ? {
              markLine: {
                silent: true,
                symbol: "none",
                label: { show: false },
                lineStyle: { color: chartTheme.axisText, type: "solid", width: 1, opacity: 0.45 },
                data: [{ yAxis: 0.5 }],
              },
            }
          : {}),
      })),
    };
  }, [chartTheme, points, setLookup]);

  if (options.sets.length === 0 && options.formats.length === 0) {
    return null;
  }

  const winRate = performance.wins + performance.losses > 0
    ? performance.wins / (performance.wins + performance.losses)
    : null;
  const lastTen = latest?.rolling[10] ?? null;

  return (
    <section className="panel" aria-labelledby={headingId}>
      <div className="panel-head rank-toolbar">
        <div>
          <h3 id={headingId}>Event Win Rate</h3>
          <p>Match win rate pooled over your last 10, last 50, and all finished draft events.</p>
        </div>
        <div className="rank-controls" role="group" aria-label="Win rate filters">
          <label className="rank-season-select">
            <span>Set</span>
            <select value={setCode ?? ""} onChange={(event) => setSetCode(event.target.value || null)}>
              <option value="">All sets</option>
              {options.sets.map((code) => (
                <option key={code} value={code}>
                  {setLookup(code)?.name ?? code}
                </option>
              ))}
            </select>
          </label>
          <label className="rank-season-select">
            <span>Format</span>
            <select value={format ?? ""} onChange={(event) => setFormat(event.target.value || null)}>
              <option value="">All formats</option>
              {options.formats.map((value) => (
                <option key={value} value={value}>
                  {value}
                </option>
              ))}
            </select>
          </label>
        </div>
      </div>

      {points.length > 0 ? (
        <div className="rank-summary">
          <div className="rank-chip rank-chip--selected">
            <span>Match record</span>
            <strong>{recordLabel(performance.wins, performance.losses)}</strong>
          </div>
          <div className="rank-chip">
            <span>Win rate</span>
            <strong>{winRate == null ? "—" : pct(winRate)}</strong>
          </div>
          <div className="rank-chip">
            <span>Events · Trophies</span>
            <strong>
              {points.length} · {performance.trophies}
            </strong>
          </div>
          <div className="rank-chip">
            <span>Last 10 events</span>
            <strong>{lastTen == null ? "—" : pct(lastTen)}</strong>
          </div>
        </div>
      ) : null}

      <div className={`rank-chart-frame ${chartOption ? "" : "rank-chart-frame--sparse"}`}>
        {points.length === 0 ? (
          <p className="state">No finished draft events match these filters.</p>
        ) : chartOption ? (
          <ReactECharts
            key={`${scheme}-${mode}`}
            option={chartOption}
            notMerge
            style={{ height: 340 }}
          />
        ) : (
          <p className="state">
            {`Only ${points.length} finished event${points.length === 1 ? "" : "s"} here so far — the chart unlocks at ${MIN_CHART_EVENTS}.`}
          </p>
        )}
      </div>
    </section>
  );
}
