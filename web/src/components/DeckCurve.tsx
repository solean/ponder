import { useId, useState, type CSSProperties } from "react";

import { CardPreviewName } from "./CardPreviewName";
import { ManaSymbol } from "./ManaSymbol";
import {
  curveBucketLabel,
  deckCurve,
  type CurveCard,
  type CurveColorKey,
  type CurveEntry,
} from "../lib/deckCurve";

type CurveMode = "type" | "color";

const COLOR_KEY_ORDER: CurveColorKey[] = ["W", "U", "B", "R", "G", "M", "C"];
const COLOR_KEY_NAMES: Record<CurveColorKey, string> = {
  W: "White",
  U: "Blue",
  B: "Black",
  R: "Red",
  G: "Green",
  M: "Multicolor",
  C: "Colorless",
};
const COLOR_NAMES: Record<string, string> = { W: "White", U: "Blue", B: "Black", R: "Red", G: "Green" };
/** The chart is at least this many blocks tall so short curves keep their shape. */
const MIN_CHART_ROWS = 6;

function cardsLabel(count: number): string {
  return `${count} ${count === 1 ? "card" : "cards"}`;
}

function blockClass(entry: CurveEntry, mode: CurveMode): string {
  return mode === "type"
    ? `deck-curve-block deck-curve-block--${entry.creature ? "creature" : "spell"}`
    : `deck-curve-block deck-curve-block--color-${entry.colorKey.toLowerCase()}`;
}

function orderedForMode(cards: CurveEntry[], mode: CurveMode): CurveEntry[] {
  if (mode === "type") return cards;
  return [...cards].sort((left, right) => COLOR_KEY_ORDER.indexOf(left.colorKey) - COLOR_KEY_ORDER.indexOf(right.colorKey));
}

/**
 * A deck's nonland mana curve with one block per card, colored by card type
 * or by card color, beside its headline numbers. Selecting a mana value
 * lists the cards at that cost.
 */
export function DeckCurve({ title, cards }: { title: string; cards: CurveCard[] }) {
  const headingID = useId();
  const curve = deckCurve(cards);
  const [mode, setMode] = useState<CurveMode>("type");
  const [selectedBucket, setSelectedBucket] = useState<number | null>(null);
  const selected = selectedBucket ?? curve.peakBucket ?? 2;
  const selectedEntry = curve.buckets[selected];
  const rows = Math.max(MIN_CHART_ROWS, ...curve.buckets.map((bucket) => bucket.count));
  const usedColorKeys = COLOR_KEY_ORDER.filter((key) =>
    curve.buckets.some((bucket) => bucket.cards.some((card) => card.colorKey === key)),
  );
  const nonCreatureCount = curve.total - curve.creatureCount;
  const creatureShare = curve.total > 0 ? Math.round((curve.creatureCount / curve.total) * 100) : 0;

  if (curve.total === 0) {
    return null;
  }

  return (
    <section className="deck-curve" aria-labelledby={headingID} style={{ "--deck-curve-rows": rows } as CSSProperties}>
      <div className="deck-curve-head">
        <div>
          <h4 id={headingID}>{title}</h4>
          <p>
            {curve.total} nonland {curve.total === 1 ? "card" : "cards"}
            {curve.unknownCount > 0 ? ` · ${cardsLabel(curve.unknownCount)} still loading` : ""} · lands not counted
          </p>
        </div>
        <div className="tabs tabs--sm" role="group" aria-label="Color the curve by">
          {(["type", "color"] as const).map((option) => (
            <button
              key={option}
              type="button"
              className={`tab ${mode === option ? "is-active" : ""}`}
              aria-pressed={mode === option}
              onClick={() => setMode(option)}
            >
              {option === "type" ? "Type" : "Color"}
            </button>
          ))}
        </div>
      </div>

      <dl className="deck-curve-stats">
        <div className="deck-curve-stat">
          <dt>Average mana value</dt>
          <dd>{curve.averageManaValue == null ? "—" : curve.averageManaValue.toFixed(1)}</dd>
        </div>
        {curve.colorCounts.length > 0 ? (
          <div className="deck-curve-stat">
            <dt>Colors</dt>
            <dd className="deck-curve-colors">
              {curve.colorCounts.map(({ color, count }) => (
                <span key={color} className="deck-curve-color" role="img" aria-label={`${COLOR_NAMES[color]}: ${count}`}>
                  <ManaSymbol token={color} />
                  <span aria-hidden="true">{count}</span>
                </span>
              ))}
            </dd>
          </div>
        ) : null}
        <div className="deck-curve-stat deck-curve-stat--split">
          <dt>Creatures / non-creature</dt>
          <dd>
            <span>
              {curve.creatureCount} <span className="deck-curve-stat-divider">/</span> {nonCreatureCount}
            </span>
            <span className="deck-curve-split" aria-hidden="true">
              <span style={{ width: `${creatureShare}%` }} />
            </span>
          </dd>
        </div>
        <div className="deck-curve-stat">
          <dt>Most common cost</dt>
          <dd>
            {curve.peakBucket == null
              ? "—"
              : `${curveBucketLabel(curve.peakBucket)} mana · ${curve.buckets[curve.peakBucket].count}`}
          </dd>
        </div>
        <div className="deck-curve-stat">
          <dt>Top end (5+)</dt>
          <dd>{cardsLabel(curve.topEndCount)}</dd>
        </div>
      </dl>

      <div className="deck-curve-body">
        <div className="deck-curve-chart">
          <div className="deck-curve-columns" role="group" aria-label="Mana values">
            {curve.buckets.map((bucket) => {
              const label = curveBucketLabel(bucket.bucket);
              const isSelected = bucket.bucket === selected;
              return (
                <button
                  key={bucket.bucket}
                  type="button"
                  className={`deck-curve-column${isSelected ? " is-selected" : ""}`}
                  aria-pressed={isSelected}
                  aria-label={`${label} mana: ${cardsLabel(bucket.count)}, ${bucket.creatureCount} creatures, ${bucket.count - bucket.creatureCount} non-creature`}
                  onClick={() => setSelectedBucket(bucket.bucket)}
                >
                  <span className="deck-curve-count">{bucket.count > 0 ? bucket.count : ""}</span>
                  <span className="deck-curve-stack" aria-hidden="true">
                    {orderedForMode(bucket.cards, mode).flatMap((card) =>
                      Array.from({ length: card.copies }, (_, copy) => (
                        <span key={`${card.cardId}-${copy}`} className={blockClass(card, mode)} />
                      )),
                    )}
                  </span>
                  <span className="deck-curve-label">{label}</span>
                </button>
              );
            })}
          </div>
          <ul className="deck-curve-legend" aria-label="Legend">
            {mode === "type" ? (
              <>
                <li><span className="deck-curve-block deck-curve-block--creature" />Creature</li>
                <li><span className="deck-curve-block deck-curve-block--spell" />Non-creature</li>
              </>
            ) : (
              usedColorKeys.map((key) => (
                <li key={key}>
                  <span className={`deck-curve-block deck-curve-block--color-${key.toLowerCase()}`} />
                  {COLOR_KEY_NAMES[key]}
                </li>
              ))
            )}
          </ul>
        </div>

        <div className="deck-curve-detail" aria-live="polite">
          <div className="deck-curve-detail-head">
            <h5>
              {curveBucketLabel(selected)} mana <span aria-hidden="true">·</span> {cardsLabel(selectedEntry.count)}
            </h5>
            {selectedEntry.count > 0 ? (
              <span>
                {selectedEntry.creatureCount} creatures <span aria-hidden="true">·</span>{" "}
                {selectedEntry.count - selectedEntry.creatureCount} non-creature
              </span>
            ) : null}
          </div>
          {selectedEntry.cards.length === 0 ? (
            <p className="deck-curve-empty">No cards at this cost.</p>
          ) : (
            <ul className="deck-curve-cards">
              {selectedEntry.cards.map((card) => {
                const colors = (card.colors ?? []).filter((color) => COLOR_NAMES[color]);
                return (
                  <li key={card.cardId}>
                    {colors.length > 0 ? (
                      <span className="deck-curve-pips" role="img" aria-label={colors.map((color) => COLOR_NAMES[color]).join(" and ")}>
                        {colors.map((color) => <ManaSymbol key={color} token={color} />)}
                      </span>
                    ) : null}
                    <CardPreviewName
                      cardId={card.cardId}
                      cardName={card.name}
                      inline
                      label={<span className="deck-curve-card-name">{card.name}</span>}
                    />
                    {card.copies > 1 ? <span className="deck-curve-copies">×{card.copies}</span> : null}
                    <span className={`deck-curve-type${card.creature ? " is-creature" : ""}`}>{card.primaryType}</span>
                  </li>
                );
              })}
            </ul>
          )}
        </div>
      </div>
    </section>
  );
}
