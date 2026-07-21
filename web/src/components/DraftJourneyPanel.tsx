import { useMemo, useState } from "react";
import { useQueries } from "@tanstack/react-query";

import { CardPreviewName } from "./CardPreviewName";
import { ManaSymbol } from "./ManaSymbol";
import { StatusMessage } from "./StatusMessage";
import { fetchCardPreview, type CardPreview } from "../lib/scryfall";
import type { DraftPick, DraftPickCard } from "../lib/types";

const COLOR_ORDER = ["W", "U", "B", "R", "G"] as const;

// Line colors chosen to stay readable on both themes; white is gold-tinted so
// it never disappears against light backgrounds.
const COLOR_STROKES: Record<string, string> = {
  W: "#c9b458",
  U: "#4a90d9",
  B: "#8d6ba8",
  R: "#d05038",
  G: "#3f9a5f",
};

type OrderedPick = {
  packNumber: number;
  displayPack: number;
  pickNumber: number;
  displayPick: number;
  overallIndex: number;
  pickedCards: DraftPickCard[];
  packCards: DraftPickCard[];
};

type WheeledCard = {
  cardId: number;
  cardName?: string;
  packNumber: number;
  displayPack: number;
  seenAtPick: number;
  displaySeenAtPick: number;
  wheeledAtPick: number;
  displayWheeledAtPick: number;
  taken: boolean;
};

function orderedPicks(picks: DraftPick[]): OrderedPick[] {
  const sorted = [...picks].sort((a, b) => a.packNumber - b.packNumber || a.pickNumber - b.pickNumber);
  // Some Arena events report zero-based pack/pick numbers; show them one-based
  // either way while keeping the raw numbers for positional logic.
  const packOffset = sorted.some((pick) => pick.packNumber === 0) ? 1 : 0;
  const pickOffset = sorted.some((pick) => pick.pickNumber === 0) ? 1 : 0;
  return sorted.map((pick, index) => ({
    packNumber: pick.packNumber,
    displayPack: pick.packNumber + packOffset,
    pickNumber: pick.pickNumber,
    displayPick: pick.pickNumber + pickOffset,
    overallIndex: index,
    pickedCards: pick.pickedCards ?? [],
    packCards: pick.packCards ?? [],
  }));
}

/** Colors whose cumulative count ties or beats the second-highest count. */
function leadingColors(counts: Map<string, number>): Set<string> {
  const values = COLOR_ORDER.map((color) => counts.get(color) ?? 0);
  const sorted = [...values].sort((a, b) => b - a);
  const threshold = sorted[1] ?? 0;
  const out = new Set<string>();
  for (const color of COLOR_ORDER) {
    const count = counts.get(color) ?? 0;
    if (count > 0 && count >= threshold) {
      out.add(color);
    }
  }
  return out;
}

export function DraftJourneyPanel({ picks }: { picks: DraftPick[] }) {
  const ordered = useMemo(() => orderedPicks(picks), [picks]);

  const pickedCardIDs = useMemo(() => {
    const seen = new Map<number, string | undefined>();
    for (const pick of ordered) {
      for (const card of pick.pickedCards) {
        if (!seen.has(card.cardId)) {
          seen.set(card.cardId, card.cardName);
        }
      }
    }
    return [...seen.entries()].map(([cardId, cardName]) => ({ cardId, cardName }));
  }, [ordered]);

  const previewQueries = useQueries({
    queries: pickedCardIDs.map((card) => ({
      queryKey: ["card-preview", card.cardId, card.cardName?.trim() || `Card ${card.cardId}`],
      queryFn: () => fetchCardPreview(card.cardId, card.cardName),
      staleTime: 1000 * 60 * 60 * 24,
      gcTime: 1000 * 60 * 60 * 24,
      retry: 1,
    })),
  });
  const colorsByCardID = useMemo(() => {
    const out = new Map<number, string[]>();
    pickedCardIDs.forEach((card, index) => {
      const preview = previewQueries[index]?.data;
      if (preview?.colors) {
        out.set(card.cardId, preview.colors);
      }
    });
    return out;
  }, [pickedCardIDs, previewQueries]);

  // Cumulative colored-pick counts after each pick; multicolor cards count
  // toward each of their colors.
  const series = useMemo(() => {
    const counts = new Map<string, number>();
    const points: Array<Map<string, number>> = [];
    for (const pick of ordered) {
      for (const card of pick.pickedCards) {
        for (const color of colorsByCardID.get(card.cardId) ?? []) {
          counts.set(color, (counts.get(color) ?? 0) + 1);
        }
      }
      points.push(new Map(counts));
    }
    return points;
  }, [colorsByCardID, ordered]);

  const finalCounts = series.length > 0 ? series[series.length - 1] : new Map<string, number>();
  const activeColors = COLOR_ORDER.filter((color) => (finalCounts.get(color) ?? 0) > 0);

  const finalTopColors = useMemo(() => {
    const sorted = [...activeColors].sort(
      (a, b) => (finalCounts.get(b) ?? 0) - (finalCounts.get(a) ?? 0),
    );
    return sorted.slice(0, 2);
  }, [activeColors, finalCounts]);

  const establishedAt = useMemo(() => {
    if (finalTopColors.length < 2 || series.length === 0) {
      return null;
    }
    for (let start = 0; start < series.length; start += 1) {
      let holds = true;
      for (let index = start; index < series.length; index += 1) {
        const leaders = leadingColors(series[index]);
        if (!finalTopColors.every((color) => leaders.has(color))) {
          holds = false;
          break;
        }
      }
      if (holds) {
        return start;
      }
    }
    return null;
  }, [finalTopColors, series]);

  const pivots = useMemo(() => {
    const out: number[] = [];
    for (let index = 7; index < series.length; index += 1) {
      const previous = leadingColors(series[index - 1]);
      const current = leadingColors(series[index]);
      const changed =
        [...current].some((color) => !previous.has(color)) && previous.size > 0 && current.size <= 3;
      if (changed && (establishedAt == null || index < establishedAt)) {
        out.push(index);
      }
    }
    return out.slice(0, 4);
  }, [establishedAt, series]);

  const wheeled = useMemo<WheeledCard[]>(() => {
    const byPosition = new Map<string, OrderedPick>();
    for (const pick of ordered) {
      byPosition.set(`${pick.packNumber}-${pick.pickNumber}`, pick);
    }
    // A card sits in the pack across several picks before it wheels; key by
    // pack+card and keep only the earliest sighting so each physical card
    // appears once.
    const byCard = new Map<string, WheeledCard>();
    for (const pick of ordered) {
      if (pick.packCards.length === 0) {
        continue;
      }
      const wheelPick = byPosition.get(`${pick.packNumber}-${pick.pickNumber + 8}`);
      if (!wheelPick || wheelPick.packCards.length === 0) {
        continue;
      }
      const pickedNow = new Set(pick.pickedCards.map((card) => card.cardId));
      const laterPack = new Map(wheelPick.packCards.map((card) => [card.cardId, card]));
      for (const card of pick.packCards) {
        if (pickedNow.has(card.cardId) || !laterPack.has(card.cardId)) {
          continue;
        }
        const key = `${pick.packNumber}-${card.cardId}`;
        const taken = wheelPick.pickedCards.some((picked) => picked.cardId === card.cardId);
        const existing = byCard.get(key);
        if (!existing) {
          byCard.set(key, {
            cardId: card.cardId,
            cardName: card.cardName ?? laterPack.get(card.cardId)?.cardName,
            packNumber: pick.packNumber,
            displayPack: pick.displayPack,
            seenAtPick: pick.pickNumber,
            displaySeenAtPick: pick.displayPick,
            wheeledAtPick: wheelPick.pickNumber,
            displayWheeledAtPick: wheelPick.displayPick,
            taken,
          });
        } else if (taken && !existing.taken) {
          existing.taken = true;
          existing.wheeledAtPick = wheelPick.pickNumber;
          existing.displayWheeledAtPick = wheelPick.displayPick;
        }
      }
    }
    return [...byCard.values()].sort((a, b) => {
      if (a.taken !== b.taken) {
        return a.taken ? -1 : 1;
      }
      return a.packNumber - b.packNumber || a.seenAtPick - b.seenAtPick;
    });
  }, [ordered]);

  // In an eight-player pod every pick past eight is technically a wheel, so
  // only surface the memorable ones: cards seen while the pack was still
  // fresh that came back around and were taken.
  const notableWheels = useMemo(
    () => wheeled.filter((card) => card.taken && card.seenAtPick <= 4),
    [wheeled],
  );

  const isColorDataLoading = previewQueries.some((query) => query.isPending);

  if (ordered.length === 0) {
    return null;
  }

  const chartWidth = 640;
  const chartHeight = 180;
  const padLeft = 30;
  const padBottom = 22;
  const padTop = 10;
  const plotWidth = chartWidth - padLeft - 8;
  const plotHeight = chartHeight - padTop - padBottom;
  const maxCount = Math.max(1, ...activeColors.map((color) => finalCounts.get(color) ?? 0));
  const xForIndex = (index: number) =>
    padLeft + (series.length <= 1 ? 0 : (index / (series.length - 1)) * plotWidth);
  const yForCount = (count: number) => padTop + plotHeight - (count / maxCount) * plotHeight;

  const packBoundaries: number[] = [];
  for (let index = 1; index < ordered.length; index += 1) {
    if (ordered[index].packNumber !== ordered[index - 1].packNumber) {
      packBoundaries.push(index);
    }
  }

  return (
    <section className="panel draft-journey-panel">
      <div className="panel-head">
        <div>
          <h3>Color Path</h3>
          <p>Cumulative colored picks across the draft, from Scryfall card colors</p>
        </div>
      </div>

      {isColorDataLoading ? (
        <StatusMessage>Resolving card colors…</StatusMessage>
      ) : activeColors.length === 0 ? (
        <StatusMessage>No colored picks resolved for this draft.</StatusMessage>
      ) : (
        <>
          <svg
            className="draft-color-chart"
            viewBox={`0 0 ${chartWidth} ${chartHeight}`}
            role="img"
            aria-label="Cumulative colored picks by pick number"
          >
            <line
              x1={padLeft}
              y1={padTop + plotHeight}
              x2={padLeft + plotWidth}
              y2={padTop + plotHeight}
              className="draft-chart-axis"
            />
            {packBoundaries.map((index) => (
              <g key={`boundary-${index}`}>
                <line
                  x1={xForIndex(index)}
                  y1={padTop}
                  x2={xForIndex(index)}
                  y2={padTop + plotHeight}
                  className="draft-chart-pack-boundary"
                />
                <text x={xForIndex(index) + 3} y={padTop + 10} className="draft-chart-label">
                  Pack {ordered[index].displayPack}
                </text>
              </g>
            ))}
            {establishedAt != null && establishedAt > 0 ? (
              <g>
                <line
                  x1={xForIndex(establishedAt)}
                  y1={padTop}
                  x2={xForIndex(establishedAt)}
                  y2={padTop + plotHeight}
                  className="draft-chart-established"
                />
                <text
                  x={xForIndex(establishedAt) + 3}
                  y={padTop + plotHeight - 6}
                  className="draft-chart-label is-established"
                >
                  locked
                </text>
              </g>
            ) : null}
            {activeColors.map((color) => {
              const path = series
                .map((counts, index) => {
                  const command = index === 0 ? "M" : "L";
                  return `${command}${xForIndex(index).toFixed(1)},${yForCount(counts.get(color) ?? 0).toFixed(1)}`;
                })
                .join(" ");
              return (
                <path
                  key={color}
                  d={path}
                  fill="none"
                  stroke={COLOR_STROKES[color]}
                  strokeWidth={2}
                  strokeLinejoin="round"
                />
              );
            })}
            <text x={padLeft - 4} y={yForCount(maxCount) + 4} className="draft-chart-label" textAnchor="end">
              {maxCount}
            </text>
            <text x={padLeft} y={chartHeight - 6} className="draft-chart-label">
              P1P1
            </text>
            <text x={padLeft + plotWidth} y={chartHeight - 6} className="draft-chart-label" textAnchor="end">
              {`P${ordered[ordered.length - 1].displayPack}P${ordered[ordered.length - 1].displayPick}`}
            </text>
          </svg>

          <div className="draft-journey-notes">
            <div className="draft-journey-legend" aria-label="Chart legend">
              {activeColors.map((color) => (
                <span className="draft-journey-legend-item" key={color}>
                  <span className="draft-journey-swatch" style={{ background: COLOR_STROKES[color] }} />
                  <ManaSymbol token={color} />
                  <span>{finalCounts.get(color) ?? 0}</span>
                </span>
              ))}
            </div>
            <p>
              {finalTopColors.length >= 2 ? (
                <>
                  Final colors {finalTopColors.join("")}
                  {establishedAt != null
                    ? `, locked in at pack ${ordered[establishedAt].displayPack} pick ${ordered[establishedAt].displayPick}`
                    : ", never firmly established"}
                  .
                </>
              ) : (
                "Not enough colored picks to name a color pair."
              )}
              {pivots.length > 0
                ? ` Leading colors shifted at ${pivots
                    .map((index) => `P${ordered[index].displayPack}P${ordered[index].displayPick}`)
                    .join(", ")}.`
                : ""}
            </p>
          </div>
        </>
      )}

      {wheeled.length > 0 ? (
        <div className="draft-wheeled">
          <h4>Wheeled into your pool</h4>
          <p>Seen in a fresh pack (first four picks), passed, and taken when it came back around.</p>
          {notableWheels.length === 0 ? (
            <p className="matchup-empty">None — nothing you passed early made it back into your pool.</p>
          ) : (
            <ul className="matchup-card-chips">
              {notableWheels.map((card) => (
                <li key={`${card.packNumber}-${card.cardId}`}>
                  <CardPreviewName cardId={card.cardId} cardName={card.cardName} />
                  <small>
                    P{card.displayPack}P{card.displaySeenAtPick} → P{card.displayPack}P{card.displayWheeledAtPick}
                  </small>
                </li>
              ))}
            </ul>
          )}
          <p className="matchup-footnote" style={{ margin: "0.45rem 0 0" }}>
            {wheeled.length.toLocaleString()} card{wheeled.length === 1 ? "" : "s"} wheeled in total across the draft.
          </p>
        </div>
      ) : null}
    </section>
  );
}

export function DraftPackReplayPanel({ picks }: { picks: DraftPick[] }) {
  const ordered = useMemo(() => orderedPicks(picks), [picks]);
  const replayablePicks = useMemo(
    () => ordered.filter((pick) => pick.packCards.length > 0),
    [ordered],
  );
  const [replayIndex, setReplayIndex] = useState(0);
  const current = replayablePicks[Math.min(replayIndex, Math.max(0, replayablePicks.length - 1))];

  const packCards = current?.packCards ?? [];
  const previewQueries = useQueries({
    queries: packCards.map((card) => ({
      queryKey: ["card-preview", card.cardId, card.cardName?.trim() || `Card ${card.cardId}`],
      queryFn: () => fetchCardPreview(card.cardId, card.cardName),
      staleTime: 1000 * 60 * 60 * 24,
      gcTime: 1000 * 60 * 60 * 24,
      retry: 1,
    })),
  });
  const previewByCardID = useMemo(() => {
    const out = new Map<number, CardPreview>();
    packCards.forEach((card, index) => {
      const preview = previewQueries[index]?.data;
      if (preview) {
        out.set(card.cardId, preview);
      }
    });
    return out;
  }, [packCards, previewQueries]);

  if (replayablePicks.length === 0) {
    return null;
  }

  const pickedIDs = new Set(current.pickedCards.map((card) => card.cardId));

  return (
    <section className="panel draft-replay-panel">
      <div className="panel-head">
        <div>
          <h3>Pack Replay</h3>
          <p>
            {replayablePicks.length} of {ordered.length} picks have recorded pack contents
          </p>
        </div>
        <div className="draft-replay-controls">
          <button
            type="button"
            className="tab"
            onClick={() => setReplayIndex((index) => Math.max(0, index - 1))}
            disabled={replayIndex === 0}
          >
            ← Prev
          </button>
          <select
            value={replayIndex}
            onChange={(event) => setReplayIndex(Number(event.target.value))}
            aria-label="Jump to pick"
          >
            {replayablePicks.map((pick, index) => (
              <option key={`${pick.packNumber}-${pick.pickNumber}`} value={index}>
                Pack {pick.displayPack} · Pick {pick.displayPick}
              </option>
            ))}
          </select>
          <button
            type="button"
            className="tab"
            onClick={() => setReplayIndex((index) => Math.min(replayablePicks.length - 1, index + 1))}
            disabled={replayIndex >= replayablePicks.length - 1}
          >
            Next →
          </button>
        </div>
      </div>

      <div className="draft-replay-grid" aria-label={`Pack ${current.displayPack} pick ${current.displayPick} contents`}>
        {packCards.map((card) => {
          const preview = previewByCardID.get(card.cardId);
          const isPicked = pickedIDs.has(card.cardId);
          return (
            <figure className={`draft-replay-card ${isPicked ? "is-picked" : ""}`} key={card.cardId}>
              {preview?.imageUrl ? (
                <img src={preview.imageUrl} alt={preview.name} loading="lazy" width={488} height={680} />
              ) : (
                <span className="draft-replay-card-fallback">{card.cardName ?? `Card ${card.cardId}`}</span>
              )}
              {isPicked ? <figcaption>Picked</figcaption> : null}
            </figure>
          );
        })}
      </div>
    </section>
  );
}
