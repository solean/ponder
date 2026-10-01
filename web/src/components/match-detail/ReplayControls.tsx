import { Fragment, useEffect, useId, useMemo, useRef, useState } from "react";
import {
  boardTurnLabel,
  buildReplayBeat,
  replayFrameMomentLabel,
  replayLifeDelta,
  replayLifeSeriesDomain,
  replayTurnLabel,
  timelinePlayerLabel,
  type ReplayBeat,
  type ReplayBoardCensus,
  type ReplayKeyMoment,
  type ReplayLifePoint,
  type ReplayRelationshipIndex,
  type ReplayTickKind,
  type ReplayTurnBoundary,
} from "../../lib/replay";
import { REPLAY_SPEED_OPTIONS } from "../../lib/replay/useReplayPlayer";
import type { MatchReplayFrame } from "../../lib/types";

const SCRUBBER_VIEW_W = 1000;

const SCRUBBER_VIEW_H = 64;

const SCRUBBER_LIFE_TOP = 7;

const SCRUBBER_LIFE_H = 31;

const SCRUBBER_TICK_TOP = 43;

const SCRUBBER_TICK_BOTTOM = 51;

/** Game-state summary shown while hovering the scrubber. */
export type ReplayScrubberSnapshot = {
  momentLabel: string;
  stepLabel: string;
  selfLife: number | null;
  opponentLife: number | null;
  selfLifeDelta: number | null;
  opponentLifeDelta: number | null;
  census: ReplayBoardCensus;
  beat: ReplayBeat;
};

function scrubberSnapshotAriaText(snapshot: ReplayScrubberSnapshot): string {
  const life = (value: number | null) =>
    value == null ? "unknown" : `${value}`;
  return `${snapshot.momentLabel} — you ${life(snapshot.selfLife)} life, opponent ${life(snapshot.opponentLife)} life`;
}

function MatchReplayScrubberLife({
  side,
  life,
  delta,
}: {
  side: "self" | "opponent";
  life: number | null;
  delta: number | null;
}) {
  return (
    <span className={`match-replay-scrubber-tooltip-life is-${side}`}>
      <span className="match-replay-scrubber-tooltip-life-label">
        {side === "self" ? "You" : "Opp"}
      </span>
      <strong>{life ?? "—"}</strong>
      {delta !== null ? (
        <span
          className={`match-replay-scrubber-tooltip-delta ${delta > 0 ? "is-up" : "is-down"}`}
        >
          {delta > 0 ? `+${delta}` : delta}
        </span>
      ) : null}
    </span>
  );
}

const SCRUBBER_CENSUS_COLUMNS = [
  { key: "creatures", label: "Crea" },
  { key: "power", label: "Pwr" },
  { key: "lands", label: "Land" },
  { key: "hand", label: "Hand" },
  { key: "graveyard", label: "Grave" },
] as const;

function MatchReplayScrubberTooltip({
  snapshot,
}: {
  snapshot: ReplayScrubberSnapshot;
}) {
  return (
    <>
      <p className="match-replay-scrubber-tooltip-head">
        <strong>{snapshot.momentLabel}</strong>
        <span>{snapshot.stepLabel}</span>
      </p>
      <div className="match-replay-scrubber-tooltip-lifes">
        <MatchReplayScrubberLife
          side="self"
          life={snapshot.selfLife}
          delta={snapshot.selfLifeDelta}
        />
        <MatchReplayScrubberLife
          side="opponent"
          life={snapshot.opponentLife}
          delta={snapshot.opponentLifeDelta}
        />
      </div>
      <div className="match-replay-scrubber-tooltip-census" role="presentation">
        <span className="match-replay-scrubber-tooltip-cell is-head" />
        {SCRUBBER_CENSUS_COLUMNS.map((column) => (
          <span
            key={`head-${column.key}`}
            className="match-replay-scrubber-tooltip-cell is-head"
          >
            {column.label}
          </span>
        ))}
        {(["self", "opponent"] as const).map((side) => (
          <Fragment key={side}>
            <span
              className={`match-replay-scrubber-tooltip-cell is-side is-${side}`}
            >
              {side === "self" ? "You" : "Opp"}
            </span>
            {SCRUBBER_CENSUS_COLUMNS.map((column) => (
              <span
                key={`${side}-${column.key}`}
                className="match-replay-scrubber-tooltip-cell"
              >
                {snapshot.census[side][column.key] ?? "—"}
              </span>
            ))}
          </Fragment>
        ))}
      </div>
      <p className="match-replay-scrubber-tooltip-beat">
        {snapshot.beat.text}
        {snapshot.beat.note ? ` · ${snapshot.beat.note}` : ""}
      </p>
    </>
  );
}

export function MatchReplayScrubber({
  length,
  index,
  onSeek,
  turnBoundaries,
  itemLabel,
  lifeSeries,
  tickKinds,
  keyMoments,
  snapshotForIndex,
}: {
  length: number;
  index: number;
  onSeek: (index: number) => void;
  turnBoundaries: ReplayTurnBoundary[];
  itemLabel: "step" | "action";
  lifeSeries?: ReplayLifePoint[];
  tickKinds?: ReplayTickKind[];
  keyMoments?: ReplayKeyMoment[];
  snapshotForIndex?: (index: number) => ReplayScrubberSnapshot | null;
}) {
  const trackRef = useRef<HTMLDivElement | null>(null);
  const [isScrubbing, setIsScrubbing] = useState(false);
  const [hoverIndex, setHoverIndex] = useState<number | null>(null);
  const lastIndex = length > 0 ? length - 1 : 0;

  const xOf = (i: number) =>
    lastIndex > 0 ? (i / lastIndex) * SCRUBBER_VIEW_W : 0;
  const pctOf = (i: number) => (lastIndex > 0 ? (i / lastIndex) * 100 : 0);

  const domain =
    lifeSeries && lifeSeries.length > 0
      ? replayLifeSeriesDomain(lifeSeries)
      : null;
  const yOf = (value: number) => {
    if (!domain) {
      return SCRUBBER_LIFE_TOP + SCRUBBER_LIFE_H;
    }
    const span = domain.max - domain.min || 1;
    return (
      SCRUBBER_LIFE_TOP + (1 - (value - domain.min) / span) * SCRUBBER_LIFE_H
    );
  };
  const lifePath = (side: "self" | "opponent") => {
    if (!lifeSeries) {
      return "";
    }
    const points: string[] = [];
    lifeSeries.forEach((point, i) => {
      const value = point[side];
      if (typeof value === "number") {
        points.push(`${xOf(i).toFixed(1)},${yOf(value).toFixed(2)}`);
      }
    });
    return points.join(" ");
  };

  const indexFromClientX = (clientX: number): number | null => {
    const element = trackRef.current;
    if (!element) {
      return null;
    }
    const rect = element.getBoundingClientRect();
    if (rect.width <= 0) {
      return null;
    }
    const fraction = Math.max(
      0,
      Math.min(1, (clientX - rect.left) / rect.width),
    );
    return Math.round(fraction * lastIndex);
  };

  const seekFromClientX = (clientX: number) => {
    const nextIndex = indexFromClientX(clientX);
    if (nextIndex !== null) {
      onSeek(nextIndex);
    }
  };

  // Hover previews without seeking; hidden mid-drag because the real board
  // below is already following the pointer.
  const hoverSnapshot =
    snapshotForIndex && hoverIndex !== null && !isScrubbing
      ? snapshotForIndex(hoverIndex)
      : null;
  const hoverPct = hoverIndex !== null ? pctOf(hoverIndex) : 0;
  const hoverAlign =
    hoverPct < 14 ? "is-align-start" : hoverPct > 86 ? "is-align-end" : "";
  const currentSnapshot = snapshotForIndex ? snapshotForIndex(index) : null;

  return (
    <div className="match-replay-scrubber">
      <div
        ref={trackRef}
        className={`match-replay-scrubber-track ${isScrubbing ? "is-scrubbing" : ""}`}
        role="slider"
        tabIndex={0}
        aria-label={`Replay ${itemLabel} position`}
        aria-valuemin={1}
        aria-valuemax={Math.max(length, 1)}
        aria-valuenow={index + 1}
        aria-valuetext={`${itemLabel} ${index + 1} of ${length}${
          currentSnapshot
            ? ` — ${scrubberSnapshotAriaText(currentSnapshot)}`
            : ""
        }`}
        onPointerDown={(event) => {
          event.preventDefault();
          try {
            event.currentTarget.setPointerCapture(event.pointerId);
          } catch {
            // pointer capture is best-effort
          }
          setIsScrubbing(true);
          seekFromClientX(event.clientX);
        }}
        onPointerMove={(event) => {
          if (isScrubbing) {
            seekFromClientX(event.clientX);
          }
          if (snapshotForIndex) {
            setHoverIndex(indexFromClientX(event.clientX));
          }
        }}
        onPointerUp={(event) => {
          setIsScrubbing(false);
          try {
            event.currentTarget.releasePointerCapture(event.pointerId);
          } catch {
            // pointer capture may already be released
          }
        }}
        onPointerCancel={() => setIsScrubbing(false)}
        onPointerLeave={() => setHoverIndex(null)}
      >
        <svg
          className="match-replay-scrubber-svg"
          viewBox={`0 0 ${SCRUBBER_VIEW_W} ${SCRUBBER_VIEW_H}`}
          preserveAspectRatio="none"
          aria-hidden="true"
        >
          {turnBoundaries.map((boundary) => (
            <line
              key={`turn-${boundary.turnKey}-${boundary.firstIndex}`}
              className="match-replay-scrubber-turn-line"
              x1={xOf(boundary.firstIndex)}
              x2={xOf(boundary.firstIndex)}
              y1={0}
              y2={SCRUBBER_VIEW_H}
              vectorEffect="non-scaling-stroke"
            />
          ))}
          {tickKinds?.map((kind, i) =>
            kind === "other" ? null : (
              <line
                key={`tick-${i}`}
                className={`match-replay-scrubber-tick is-${kind}`}
                x1={xOf(i)}
                x2={xOf(i)}
                y1={SCRUBBER_TICK_TOP}
                y2={SCRUBBER_TICK_BOTTOM}
                vectorEffect="non-scaling-stroke"
              />
            ),
          )}
          {lifeSeries ? (
            <>
              <polyline
                className="match-replay-scrubber-life is-opponent"
                points={lifePath("opponent")}
                vectorEffect="non-scaling-stroke"
              />
              <polyline
                className="match-replay-scrubber-life is-self"
                points={lifePath("self")}
                vectorEffect="non-scaling-stroke"
              />
            </>
          ) : null}
        </svg>

        <div className="match-replay-scrubber-turn-labels" aria-hidden="true">
          {turnBoundaries.map((boundary) => (
            <span
              key={`turn-label-${boundary.turnKey}-${boundary.firstIndex}`}
              className="match-replay-scrubber-turn-label"
              style={{ left: `${pctOf(boundary.firstIndex)}%` }}
            >
              {boardTurnLabel(boundary.turnKey)}
            </span>
          ))}
        </div>

        {keyMoments?.map((moment) => (
          <button
            key={`moment-${moment.index}`}
            type="button"
            className={`match-replay-scrubber-pin is-${moment.kind}`}
            style={{ left: `${pctOf(moment.index)}%` }}
            title={moment.label}
            aria-label={`Jump to ${moment.label}`}
            onPointerDown={(event) => event.stopPropagation()}
            onClick={() => onSeek(moment.index)}
          />
        ))}

        <div
          className="match-replay-scrubber-head"
          style={{ left: `${pctOf(index)}%` }}
          aria-hidden="true"
        />

        {hoverSnapshot ? (
          <>
            <div
              className="match-replay-scrubber-ghost"
              style={{ left: `${hoverPct}%` }}
              aria-hidden="true"
            />
            <div
              className={`match-replay-scrubber-tooltip ${hoverAlign}`}
              style={{ left: `${hoverPct}%` }}
              aria-hidden="true"
            >
              <MatchReplayScrubberTooltip snapshot={hoverSnapshot} />
            </div>
          </>
        ) : null}
      </div>

      {lifeSeries ? (
        <div className="match-replay-scrubber-legend" aria-hidden="true">
          <span className="match-replay-scrubber-legend-item is-self">
            <span className="match-replay-scrubber-legend-swatch" /> You
          </span>
          <span className="match-replay-scrubber-legend-item is-opponent">
            <span className="match-replay-scrubber-legend-swatch" /> Opponent
          </span>
        </div>
      ) : null}
    </div>
  );
}

export function MatchReplaySpeedControl({
  speed,
  onSelectSpeed,
}: {
  speed: number;
  onSelectSpeed: (speed: number) => void;
}) {
  return (
    <div
      className="match-replay-speed"
      role="group"
      aria-label="Playback speed"
    >
      <span className="match-replay-speed-label">Speed</span>
      {REPLAY_SPEED_OPTIONS.map((option) => (
        <button
          key={option}
          type="button"
          className={`match-replay-speed-button ${speed === option ? "is-active" : ""}`}
          aria-pressed={speed === option}
          onClick={() => onSelectSpeed(option)}
        >
          {option}×
        </button>
      ))}
    </div>
  );
}

function MatchReplayHudLife({
  side,
  life,
  delta,
  flashKey,
}: {
  side: "self" | "opponent";
  life?: number;
  delta: number | null;
  flashKey: number;
}) {
  return (
    <div className={`match-replay-hud-life is-${side}`}>
      <div className="match-replay-hud-life-body">
        <p className="match-replay-hud-life-label">
          {timelinePlayerLabel(side)}
        </p>
        <div className="match-replay-hud-life-readout">
          <span className="match-replay-hud-life-value">
            {typeof life === "number" ? life : "—"}
          </span>
          {delta !== null ? (
            <span
              key={flashKey}
              className={`match-replay-hud-delta ${delta > 0 ? "is-up" : "is-down"}`}
              aria-label={`${timelinePlayerLabel(side)} life ${delta > 0 ? "gained" : "lost"} ${Math.abs(delta)}`}
            >
              {delta > 0 ? `+${delta}` : delta}
            </span>
          ) : null}
        </div>
      </div>
    </div>
  );
}

export function MatchReplayHud({
  currentFrame,
  previousFrame,
  stepNumber,
  stepCount,
  beat,
}: {
  currentFrame: MatchReplayFrame;
  previousFrame: MatchReplayFrame | null;
  stepNumber: number;
  stepCount: number;
  beat: ReplayBeat;
}) {
  return (
    <section className="match-replay-hud" aria-label="Replay status">
      <MatchReplayHudLife
        side="opponent"
        life={currentFrame.opponentLifeTotal}
        delta={replayLifeDelta(previousFrame, currentFrame, "opponent")}
        flashKey={currentFrame.id}
      />
      <div className="match-replay-hud-center">
        <div className="match-replay-hud-meta">
          <span className="match-replay-hud-moment">
            {replayFrameMomentLabel(currentFrame)}
          </span>
          <span className="match-replay-hud-step">
            Step {stepNumber} / {stepCount}
          </span>
        </div>
        <p className="match-replay-hud-headline">
          {beat.text}
          {beat.note ? (
            <span className="match-replay-hud-headline-note">
              {" "}
              — {beat.note}
            </span>
          ) : null}
        </p>
      </div>
      <MatchReplayHudLife
        side="self"
        life={currentFrame.selfLifeTotal}
        delta={replayLifeDelta(previousFrame, currentFrame, "self")}
        flashKey={currentFrame.id}
      />
    </section>
  );
}

export function MatchReplayMoveList({
  frames,
  relationships,
  turnBoundaries,
  currentIndex,
  collapsed,
  onToggleCollapsed,
  onSeek,
}: {
  frames: MatchReplayFrame[];
  relationships: ReplayRelationshipIndex;
  turnBoundaries: ReplayTurnBoundary[];
  currentIndex: number;
  collapsed: boolean;
  onToggleCollapsed: () => void;
  onSeek: (index: number) => void;
}) {
  const beats = useMemo(
    () =>
      frames.map((frame, index) =>
        buildReplayBeat(
          frame,
          index > 0 ? (frames[index - 1] ?? null) : null,
          relationships,
        ),
      ),
    [frames, relationships],
  );
  const scrollId = useId();
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const activeRef = useRef<HTMLButtonElement | null>(null);

  // Deferred a frame: expanding the panel (and the arena reflow that follows a
  // width change) settles after commit, so measuring immediately would align
  // against a stale box and leave the current beat out of view.
  useEffect(() => {
    if (collapsed) return;
    const frame = requestAnimationFrame(() => {
      const scrollElement = scrollRef.current;
      const activeElement = activeRef.current;
      if (!scrollElement || !activeElement) return;

      const scrollRect = scrollElement.getBoundingClientRect();
      const activeRect = activeElement.getBoundingClientRect();
      if (activeRect.top < scrollRect.top) {
        scrollElement.scrollTop -= scrollRect.top - activeRect.top;
      } else if (activeRect.bottom > scrollRect.bottom) {
        scrollElement.scrollTop += activeRect.bottom - scrollRect.bottom;
      }
    });
    return () => cancelAnimationFrame(frame);
  }, [currentIndex, collapsed]);

  return (
    <aside
      className={`match-replay-movelist ${collapsed ? "is-collapsed" : ""}`}
      aria-label="Play-by-play"
    >
      <button
        type="button"
        className="match-replay-movelist-title"
        aria-expanded={!collapsed}
        aria-controls={scrollId}
        title={collapsed ? "Expand play-by-play" : "Minimize play-by-play"}
        onClick={onToggleCollapsed}
      >
        <span className="match-replay-movelist-title-text">Play-by-play</span>
        <span className="match-replay-movelist-title-toggle" aria-hidden="true">
          {collapsed ? "+" : "–"}
        </span>
      </button>
      <div
        id={scrollId}
        ref={scrollRef}
        className="match-replay-movelist-scroll"
        hidden={collapsed}
      >
        {turnBoundaries.map((boundary) => (
          <div
            className="match-replay-movelist-turn"
            key={`${boundary.turnKey}-${boundary.firstIndex}`}
          >
            <p className="match-replay-movelist-turn-label">
              {replayTurnLabel(boundary.turnKey)}
            </p>
            {Array.from(
              { length: boundary.lastIndex - boundary.firstIndex + 1 },
              (_, offset) => boundary.firstIndex + offset,
            ).map((index) => {
              const isCurrent = index === currentIndex;
              const beat = beats[index];
              if (!beat) {
                return null;
              }
              return (
                <button
                  key={index}
                  ref={isCurrent ? activeRef : undefined}
                  type="button"
                  className={`match-replay-movelist-beat ${isCurrent ? "is-current" : ""}`}
                  aria-current={isCurrent ? "step" : undefined}
                  onClick={() => onSeek(index)}
                >
                  <span className="match-replay-movelist-beat-text">
                    {beat.text}
                  </span>
                  {beat.note ? (
                    <span className="match-replay-movelist-beat-note">
                      {" "}
                      · {beat.note}
                    </span>
                  ) : null}
                </button>
              );
            })}
          </div>
        ))}
      </div>
    </aside>
  );
}
