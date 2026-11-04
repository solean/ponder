import { useId, useMemo, useState, type CSSProperties } from "react";
import { useQueries } from "@tanstack/react-query";

import { CardPreviewName } from "./CardPreviewName";
import { CardStatsSource, gihTone, statTooltip, type DraftCardStats } from "./CardStats";
import { DraftPackDialog } from "./DraftPackDialog";
import { ManaSymbol } from "./ManaSymbol";
import { StatusMessage } from "./StatusMessage";
import {
  draftPickLogPacks,
  draftPickVerdict,
  summarizeDraftPickVerdicts,
  DRAFT_PICK_TIMING_THRESHOLD,
  type DraftPickLogPack,
  type DraftPickLogPick,
  type DraftPickVerdict,
  type DraftPickVerdictSummary,
} from "../lib/draftReport";
import { pct } from "../lib/format";
import { fetchCardPreview, type CardPreview } from "../lib/scryfall";
import type { DraftPick, DraftPickCard } from "../lib/types";

type PickLogView = "rows" | "tiles";

const VIEW_STORAGE_KEY = "ponder.draftPickLog.view";
/** The rows view folds picks past this many per pack; late picks rarely decide a draft. */
const UNFOLDED_PICKS = 10;
const COLOR_ORDER = ["W", "U", "B", "R", "G"];
const COLOR_NAMES: Record<string, string> = { W: "White", U: "Blue", B: "Black", R: "Red", G: "Green" };

function readStoredView(): PickLogView {
  try {
    return window.localStorage.getItem(VIEW_STORAGE_KEY) === "tiles" ? "tiles" : "rows";
  } catch {
    return "rows";
  }
}

function storeView(view: PickLogView) {
  try {
    window.localStorage.setItem(VIEW_STORAGE_KEY, view);
  } catch {
    // Storage can be blocked; the toggle still works for this visit.
  }
}

/** Percentage points with a typographic minus, e.g. "+1.5" / "−0.3". */
function signedDelta(delta: number): string {
  const points = Math.round(delta * 1000) / 10;
  return `${points < 0 ? "−" : "+"}${Math.abs(points).toFixed(1)}`;
}

function cardDisplayName(card: DraftPickCard, preview?: CardPreview): string {
  return card.cardName?.trim() || preview?.name?.trim() || `Card ${card.cardId}`;
}

function cardsLabel(count: number): string {
  return `${count} ${count === 1 ? "card" : "cards"}`;
}

function deltaTone(verdict: DraftPickVerdict, mean: number | null) {
  return gihTone(verdict.gihWinRate, mean);
}

function isRanked(verdict: DraftPickVerdict): verdict is DraftPickVerdict & { rank: number } {
  return verdict.rank != null && verdict.ratedCount > 1;
}

function ColorPips({ colors }: { colors?: string[] }) {
  const ordered = COLOR_ORDER.filter((color) => colors?.includes(color));
  if (ordered.length === 0) return null;
  return (
    <span className="draft-pick-pips" role="img" aria-label={ordered.map((color) => COLOR_NAMES[color]).join(" and ")}>
      {ordered.map((color) => (
        <ManaSymbol key={color} token={color} />
      ))}
    </span>
  );
}

function ChevronIcon({ className, direction = "right" }: { className: string; direction?: "right" | "down" }) {
  return (
    <svg className={className} aria-hidden="true" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
      <path d={direction === "right" ? "M6 3.5 10.5 8 6 12.5" : "M3.5 6 8 10.5 12.5 6"} />
    </svg>
  );
}

function VerdictChips({ verdict }: { verdict: DraftPickVerdict }) {
  return (
    <>
      {isRanked(verdict) ? (
        verdict.rank === 1 ? (
          <span className="draft-pick-chip draft-pick-chip--best">Best in pack</span>
        ) : (
          <span className="draft-pick-chip draft-pick-chip--muted">
            #{verdict.rank} of {verdict.ratedCount}
          </span>
        )
      ) : null}
      {verdict.timing != null && verdict.ata != null ? (
        <span className={`draft-pick-chip draft-pick-chip--${verdict.timing === "late" ? "late" : "muted"}`}>
          {verdict.timing === "late" ? "Late" : "Early"} <span aria-hidden="true">·</span> ATA {verdict.ata.toFixed(1)}
        </span>
      ) : null}
    </>
  );
}

function DraftPickSummaryStrip({ summary, mean }: { summary: DraftPickVerdictSummary; mean: number | null }) {
  const averageTone = summary.averageDelta != null && mean != null ? gihTone(mean + summary.averageDelta, mean) : "neutral";
  return (
    <div className="draft-pick-summary">
      {summary.averageDelta != null ? (
        <p className="draft-pick-summary-stat">
          <strong className={`win-rate win-rate--${averageTone}`}>{signedDelta(summary.averageDelta)}</strong> average pick vs format
        </p>
      ) : null}
      {summary.rankedCount > 0 ? (
        <p className="draft-pick-summary-stat">
          <strong>
            {summary.bestCount}/{summary.rankedCount}
          </strong>{" "}
          best in pack
        </p>
      ) : null}
      <p className="draft-pick-summary-stat draft-pick-summary-stat--late">
        <strong>{summary.lateCount}</strong> taken late
      </p>
      <ul className="draft-pick-legend" aria-label="Legend">
        <li>
          <span className="draft-pick-chip draft-pick-chip--best">Best in pack</span>highest GIH WR in the pack
        </li>
        <li>
          <span className="draft-pick-chip draft-pick-chip--late">Late</span>taken {DRAFT_PICK_TIMING_THRESHOLD}+ picks after its ATA
        </li>
        {mean != null ? <li>± = GIH WR minus the format average ({pct(mean)})</li> : null}
      </ul>
    </div>
  );
}

function DraftPickRow({
  pick,
  displayPack,
  verdict,
  previews,
  cardStats,
  onOpen,
}: {
  pick: DraftPickLogPick;
  displayPack: number;
  verdict: DraftPickVerdict;
  previews: Map<number, CardPreview>;
  cardStats: DraftCardStats;
  onOpen: () => void;
}) {
  const openable = pick.packCards.length > 0;
  const stat = verdict.cardId != null ? cardStats.byId.get(verdict.cardId) : undefined;
  const passed = verdict.bestPassed;
  const hasMeta = isRanked(verdict) || verdict.timing != null || passed != null;

  return (
    <li className={`draft-pick-row${openable ? " is-openable" : ""}`}>
      {openable ? (
        <button
          type="button"
          className="draft-pick-row-open"
          aria-haspopup="dialog"
          aria-label={`View pack ${displayPack}, pick ${pick.displayPick}: ${cardsLabel(pick.packCards.length)}`}
          title={stat ? statTooltip(stat, cardStats.mean) : undefined}
          onClick={onOpen}
        />
      ) : null}
      <span className="draft-pick-row-number">{pick.displayPick}</span>
      <div className="draft-pick-row-cards">
        {pick.pickedCards.length === 0 ? (
          <span className="draft-card-empty">Selection unavailable</span>
        ) : (
          pick.pickedCards.map((card, index) => {
            const preview = previews.get(card.cardId);
            const artURL = preview?.artCropUrl ?? preview?.imageUrl;
            return (
              <span className="draft-pick-row-card" key={`${card.cardId}-${index}`}>
                <CardPreviewName
                  cardId={card.cardId}
                  cardName={card.cardName}
                  inline
                  resolveName
                  label={
                    <>
                      <span className="draft-pick-thumb">
                        {artURL ? <img src={artURL} alt="" loading="lazy" decoding="async" /> : null}
                      </span>
                      <span className="draft-pick-row-name">{cardDisplayName(card, preview)}</span>
                    </>
                  }
                />
                <ColorPips colors={preview?.colors} />
              </span>
            );
          })
        )}
      </div>
      {verdict.gihWinRate != null ? (
        <span className="draft-pick-row-stat">
          <span className="draft-pick-row-gih">{pct(verdict.gihWinRate)}</span>
          {verdict.delta != null ? (
            <strong className={`draft-pick-row-delta win-rate win-rate--${deltaTone(verdict, cardStats.mean)}`}>
              {signedDelta(verdict.delta)}
            </strong>
          ) : null}
        </span>
      ) : null}
      {hasMeta ? (
        <span className="draft-pick-row-meta">
          <VerdictChips verdict={verdict} />
          {passed ? (
            <span className="draft-pick-row-passed">
              passed{" "}
              <CardPreviewName
                cardId={passed.card.cardId}
                cardName={passed.card.cardName}
                inline
                resolveName
                label={<strong>{cardDisplayName(passed.card, previews.get(passed.card.cardId))}</strong>}
              />{" "}
              <span className="draft-pick-row-passed-gih">{pct(passed.gihWinRate)}</span>
            </span>
          ) : null}
        </span>
      ) : null}
      {openable ? <ChevronIcon className="draft-pick-row-chevron" /> : null}
    </li>
  );
}

function PackSummaryLine({ summary, mean }: { summary: DraftPickVerdictSummary; mean: number | null }) {
  if (summary.averageDelta == null && summary.rankedCount === 0) return null;
  const parts = [];
  if (summary.averageDelta != null) {
    const tone = mean != null ? gihTone(mean + summary.averageDelta, mean) : "neutral";
    parts.push(
      <span key="avg">
        avg <strong className={`win-rate win-rate--${tone}`}>{signedDelta(summary.averageDelta)}</strong>
      </span>,
    );
  }
  if (summary.rankedCount > 0) {
    parts.push(<span key="best">{summary.bestCount}/{summary.rankedCount} best</span>);
  }
  parts.push(<span key="late">{summary.lateCount} late</span>);
  return (
    <p className="draft-pack-column-summary">
      {parts.flatMap((part, index) => (index === 0 ? [part] : [<span key={`sep-${index}`} aria-hidden="true"> · </span>, part]))}
    </p>
  );
}

function DraftPackColumn({
  pack,
  verdicts,
  summary,
  previews,
  cardStats,
  expanded,
  onToggleExpanded,
  onOpen,
}: {
  pack: DraftPickLogPack;
  verdicts: Map<DraftPickLogPick, DraftPickVerdict>;
  summary: DraftPickVerdictSummary;
  previews: Map<number, CardPreview>;
  cardStats: DraftCardStats;
  expanded: boolean;
  onToggleExpanded: () => void;
  onOpen: (pick: DraftPickLogPick) => void;
}) {
  const headingID = useId();
  const folded = pack.picks.slice(UNFOLDED_PICKS);
  const visible = expanded ? pack.picks : pack.picks.slice(0, UNFOLDED_PICKS);
  const foldedRange = folded.length === 0
    ? ""
    : folded.length === 1
      ? `pick ${folded[0].displayPick}`
      : `picks ${folded[0].displayPick}–${folded[folded.length - 1].displayPick}`;

  return (
    <article className="draft-pack-column" aria-labelledby={headingID}>
      <header className="draft-pack-column-head">
        <h4 id={headingID}>Pack {pack.displayPack}</h4>
        <PackSummaryLine summary={summary} mean={cardStats.mean} />
      </header>
      <ol className="draft-pick-rows">
        {visible.map((pick) => (
          <DraftPickRow
            key={pick.pickNumber}
            pick={pick}
            displayPack={pack.displayPack}
            verdict={verdicts.get(pick)!}
            previews={previews}
            cardStats={cardStats}
            onOpen={() => onOpen(pick)}
          />
        ))}
      </ol>
      {folded.length > 0 ? (
        <button type="button" className="draft-pick-fold" aria-expanded={expanded} onClick={onToggleExpanded}>
          {expanded ? "Hide" : "Show"} {foldedRange}
          <ChevronIcon className="draft-pick-fold-icon" direction="down" />
        </button>
      ) : null}
    </article>
  );
}

function DraftPickTile({
  pick,
  displayPack,
  verdict,
  previews,
  cardStats,
  onOpen,
}: {
  pick: DraftPickLogPick;
  displayPack: number;
  verdict: DraftPickVerdict;
  previews: Map<number, CardPreview>;
  cardStats: DraftCardStats;
  onOpen: () => void;
}) {
  const card = pick.pickedCards.find((entry) => entry.cardId === verdict.cardId) ?? pick.pickedCards[0];
  const preview = card ? previews.get(card.cardId) : undefined;
  const artURL = preview?.artCropUrl ?? preview?.imageUrl;
  const name = card ? cardDisplayName(card, preview) : "Selection unavailable";
  const extraPicks = Math.max(0, pick.pickedCards.length - 1);
  const ranked = isRanked(verdict);
  const best = ranked && verdict.rank === 1;
  const stat = verdict.cardId != null ? cardStats.byId.get(verdict.cardId) : undefined;
  const openable = pick.packCards.length > 0;

  const description = [
    `Pack ${displayPack}, pick ${pick.displayPick}: ${name}${extraPicks > 0 ? ` and ${extraPicks} more` : ""}`,
    ranked ? (best ? "best in pack" : `number ${verdict.rank} of ${verdict.ratedCount} in pack`) : null,
    verdict.delta != null ? `${signedDelta(verdict.delta)} points versus format` : null,
    verdict.timing === "late" ? "taken late" : null,
    openable ? "View pack" : null,
  ].filter(Boolean).join(", ");

  const content = (
    <>
      <span className="draft-pick-tile-art">
        {artURL ? <img src={artURL} alt="" loading="lazy" decoding="async" /> : null}
        {ranked && !best ? <span className="draft-pick-tile-badge draft-pick-tile-badge--rank">#{verdict.rank}</span> : null}
        {verdict.timing === "late" ? <span className="draft-pick-tile-badge draft-pick-tile-badge--late">Late</span> : null}
      </span>
      <span className="draft-pick-tile-name">
        {name}
        {extraPicks > 0 ? ` +${extraPicks}` : ""}
      </span>
      <span className="draft-pick-tile-stat">
        {verdict.delta != null ? (
          <strong className={`win-rate win-rate--${deltaTone(verdict, cardStats.mean)}`}>{signedDelta(verdict.delta)}</strong>
        ) : (
          <span />
        )}
        {verdict.gihWinRate != null ? <span>{pct(verdict.gihWinRate)}</span> : null}
      </span>
    </>
  );
  const className = `draft-pick-tile${best ? " is-best" : ""}`;
  const style = { gridColumn: pick.displayPick + 1 } as CSSProperties;

  return openable ? (
    <button
      type="button"
      className={className}
      style={style}
      aria-haspopup="dialog"
      aria-label={description}
      title={stat ? statTooltip(stat, cardStats.mean) : undefined}
      onClick={onOpen}
    >
      {content}
    </button>
  ) : (
    <div className={className} style={style} role="group" aria-label={description}>
      {content}
    </div>
  );
}

function DraftPickTiles({
  packs,
  verdicts,
  summaries,
  previews,
  cardStats,
  onOpen,
}: {
  packs: DraftPickLogPack[];
  verdicts: Map<DraftPickLogPick, DraftPickVerdict>;
  summaries: Map<DraftPickLogPack, DraftPickVerdictSummary>;
  previews: Map<number, CardPreview>;
  cardStats: DraftCardStats;
  onOpen: (pick: DraftPickLogPick) => void;
}) {
  const columns = Math.max(1, ...packs.flatMap((pack) => pack.picks.map((pick) => pick.displayPick)));
  const style = { "--draft-pick-tile-columns": columns } as CSSProperties;

  return (
    <div className="draft-pick-tiles-scroll">
      <div className="draft-pick-tiles" style={style}>
        <div className="draft-pick-tiles-row draft-pick-tiles-head" aria-hidden="true">
          {Array.from({ length: columns }, (_, index) => (
            <span key={index} style={{ gridColumn: index + 2 }}>
              Pick {index + 1}
            </span>
          ))}
        </div>
        {packs.map((pack) => {
          const summary = summaries.get(pack);
          return (
            <section className="draft-pick-tiles-row" key={pack.packNumber} aria-label={`Pack ${pack.displayPack}`}>
              <div className="draft-pick-tiles-pack">
                <h4>Pack {pack.displayPack}</h4>
                {summary?.averageDelta != null ? (
                  <p>
                    <strong
                      className={`win-rate win-rate--${cardStats.mean != null ? gihTone(cardStats.mean + summary.averageDelta, cardStats.mean) : "neutral"}`}
                    >
                      {signedDelta(summary.averageDelta)}
                    </strong>{" "}
                    avg
                  </p>
                ) : null}
                {summary && summary.rankedCount > 0 ? (
                  <p>
                    {summary.bestCount}/{summary.rankedCount} best
                  </p>
                ) : null}
              </div>
              {pack.picks.map((pick) => (
                <DraftPickTile
                  key={pick.pickNumber}
                  pick={pick}
                  displayPack={pack.displayPack}
                  verdict={verdicts.get(pick)!}
                  previews={previews}
                  cardStats={cardStats}
                  onOpen={() => onOpen(pick)}
                />
              ))}
            </section>
          );
        })}
      </div>
    </div>
  );
}

export function DraftPickLog({ picks, cardStats }: { picks: DraftPick[]; cardStats: DraftCardStats }) {
  const headingID = useId();
  const packs = useMemo(() => draftPickLogPacks(picks), [picks]);
  // The dialog steps through every pick with recorded pack contents, across packs.
  const viewablePicks = useMemo(
    () => packs.flatMap((pack) =>
      pack.picks.filter((pick) => pick.packCards.length > 0).map((pick) => ({ pick, displayPack: pack.displayPack })),
    ),
    [packs],
  );
  const maxPackCards = Math.max(0, ...viewablePicks.map(({ pick }) => pick.packCards.length));
  const [openIndex, setOpenIndex] = useState<number | null>(null);
  const openPick = openIndex == null ? undefined : viewablePicks[openIndex];
  const [view, setView] = useState<PickLogView>(readStoredView);
  const [showLatePicks, setShowLatePicks] = useState(false);

  const verdicts = useMemo(() => {
    const ratingOf = (cardId: number) => cardStats.byId.get(cardId);
    const out = new Map<DraftPickLogPick, DraftPickVerdict>();
    for (const pack of packs) {
      for (const pick of pack.picks) {
        out.set(pick, draftPickVerdict(pick, ratingOf, cardStats.mean));
      }
    }
    return out;
  }, [packs, cardStats.byId, cardStats.mean]);

  const { packSummaries, overallSummary } = useMemo(() => {
    const packSummaries = new Map<DraftPickLogPack, DraftPickVerdictSummary>();
    for (const pack of packs) {
      packSummaries.set(pack, summarizeDraftPickVerdicts(pack.picks.map((pick) => verdicts.get(pick)!)));
    }
    return { packSummaries, overallSummary: summarizeDraftPickVerdicts([...verdicts.values()]) };
  }, [packs, verdicts]);
  const hasRatings = cardStats.byId.size > 0;

  // One deduped batch for the whole log: Scryfall lookups are collection-batched
  // and share React Query keys with the pool/journey panels on this page. Passed
  // cards join it only when the log has no name for them.
  const previewCards = useMemo(() => {
    const seen = new Map<number, DraftPickCard>();
    const add = (card: DraftPickCard) => {
      if (card.cardId > 0 && !seen.has(card.cardId)) {
        seen.set(card.cardId, card);
      }
    };
    for (const pack of packs) {
      for (const pick of pack.picks) {
        pick.pickedCards.forEach(add);
        const passed = verdicts.get(pick)?.bestPassed?.card;
        if (passed && !passed.cardName?.trim()) add(passed);
      }
    }
    return [...seen.values()].sort((left, right) => left.cardId - right.cardId);
  }, [packs, verdicts]);

  const previewQueries = useQueries({
    queries: previewCards.map((card) => ({
      // Key shape matches the other draft panels so previews are fetched once per page.
      queryKey: ["card-preview", card.cardId, card.cardName?.trim() || `Card ${card.cardId}`],
      queryFn: () => fetchCardPreview(card.cardId, card.cardName),
      staleTime: 1000 * 60 * 60 * 24,
      gcTime: 1000 * 60 * 60 * 24,
      retry: 1,
    })),
  });

  const previewByCardID = useMemo(() => {
    const out = new Map<number, CardPreview>();
    previewCards.forEach((card, index) => {
      const preview = previewQueries[index]?.data;
      if (preview) {
        out.set(card.cardId, preview);
      }
    });
    return out;
  }, [previewCards, previewQueries]);

  const openPack = (pick: DraftPickLogPick) => setOpenIndex(viewablePicks.findIndex((entry) => entry.pick === pick));
  const chooseView = (next: PickLogView) => {
    setView(next);
    storeView(next);
  };

  return (
    <section className="panel draft-pick-log" aria-labelledby={headingID}>
      <div className="panel-head">
        <div>
          <h3 id={headingID}>Pick Log</h3>
          <p>Each pick compared with the pack it came from</p>
        </div>
        {packs.length > 0 ? (
          <div className="tabs tabs--sm" role="group" aria-label="Pick log layout">
            {(["rows", "tiles"] as const).map((option) => (
              <button
                key={option}
                type="button"
                className={`tab ${view === option ? "is-active" : ""}`}
                aria-pressed={view === option}
                onClick={() => chooseView(option)}
              >
                {option === "rows" ? "Rows" : "Tiles"}
              </button>
            ))}
          </div>
        ) : null}
      </div>

      {packs.length === 0 ? (
        <StatusMessage>No picks were recorded for this draft.</StatusMessage>
      ) : (
        <>
          {hasRatings ? <DraftPickSummaryStrip summary={overallSummary} mean={cardStats.mean} /> : null}
          {view === "rows" ? (
            <div className="draft-pack-grid">
              {packs.map((pack) => (
                <DraftPackColumn
                  key={pack.packNumber}
                  pack={pack}
                  verdicts={verdicts}
                  summary={packSummaries.get(pack)!}
                  previews={previewByCardID}
                  cardStats={cardStats}
                  expanded={showLatePicks}
                  onToggleExpanded={() => setShowLatePicks((current) => !current)}
                  onOpen={openPack}
                />
              ))}
            </div>
          ) : (
            <DraftPickTiles
              packs={packs}
              verdicts={verdicts}
              summaries={packSummaries}
              previews={previewByCardID}
              cardStats={cardStats}
              onOpen={openPack}
            />
          )}
        </>
      )}
      <CardStatsSource stats={cardStats} />
      {openPick && openIndex != null ? (
        <DraftPackDialog
          pick={openPick.pick}
          displayPack={openPick.displayPack}
          cardStats={cardStats}
          position={openIndex}
          total={viewablePicks.length}
          maxCards={maxPackCards}
          onNavigate={(step) => setOpenIndex(openIndex + step)}
          onClose={() => setOpenIndex(null)}
        />
      ) : null}
    </section>
  );
}
