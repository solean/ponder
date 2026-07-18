import { useMemo } from "react";
import { Link } from "react-router-dom";
import { useQueries, useQuery } from "@tanstack/react-query";

import { CardPreviewName } from "./CardPreviewName";
import { ManaSymbol } from "./ManaSymbol";
import { StatusMessage } from "./StatusMessage";
import { api } from "../lib/api";
import { pct } from "../lib/format";
import { fetchCardPreview, type CardPreview } from "../lib/scryfall";
import type { DeckSummary, DraftPick } from "../lib/types";

type PoolCardStatus = "main" | "side" | "unused";

type PoolCard = {
  cardId: number;
  cardName?: string;
  poolCopies: number;
  mainCopies: number;
  sideCopies: number;
  status: PoolCardStatus;
  manaValue: number | null;
  typeLine?: string;
  colors?: string[];
};

const STATUS_LABELS: Record<PoolCardStatus, string> = {
  main: "Main deck",
  side: "Sideboard",
  unused: "Not in deck",
};

const COLOR_ORDER = ["W", "U", "B", "R", "G"];

/** Decks submitted for this event, most recently updated first. */
export function draftDeckCandidates(decks: DeckSummary[], eventName: string): DeckSummary[] {
  return decks
    .filter((deck) => deck.eventName === eventName)
    .sort((a, b) => (b.lastUpdatedAt ?? "").localeCompare(a.lastUpdatedAt ?? ""));
}

function poolCounts(picks: DraftPick[]): Map<number, { copies: number; name?: string }> {
  const out = new Map<number, { copies: number; name?: string }>();
  for (const pick of picks) {
    for (const card of pick.pickedCards ?? []) {
      const entry = out.get(card.cardId) ?? { copies: 0, name: card.cardName };
      entry.copies += 1;
      if (!entry.name && card.cardName) {
        entry.name = card.cardName;
      }
      out.set(card.cardId, entry);
    }
  }
  return out;
}

function curveBuckets(cards: PoolCard[], counts: (card: PoolCard) => number): number[] {
  // Buckets 0..6 where 6 aggregates MV 6+; lands and unknown MV excluded.
  const buckets = Array.from({ length: 7 }, () => 0);
  for (const card of cards) {
    const quantity = counts(card);
    if (quantity <= 0 || card.manaValue == null) {
      continue;
    }
    if (card.typeLine?.toLowerCase().includes("land")) {
      continue;
    }
    const bucket = Math.min(6, Math.max(0, Math.round(card.manaValue)));
    buckets[bucket] += quantity;
  }
  return buckets;
}

function CurveBars({ title, buckets }: { title: string; buckets: number[] }) {
  const max = Math.max(1, ...buckets);
  return (
    <div className="draft-curve">
      <p className="draft-curve-title">{title}</p>
      <div className="draft-curve-bars" role="img" aria-label={`${title} mana curve`}>
        {buckets.map((count, manaValue) => (
          <div className="draft-curve-col" key={manaValue}>
            <span className="draft-curve-count">{count > 0 ? count : ""}</span>
            <span
              className="draft-curve-bar"
              style={{ height: `${Math.round((count / max) * 52) + 2}px` }}
            />
            <span className="draft-curve-label">{manaValue === 6 ? "6+" : manaValue}</span>
          </div>
        ))}
      </div>
    </div>
  );
}

export function DraftPoolPanel({ eventName, picks }: { eventName: string; picks: DraftPick[] }) {
  const decksQuery = useQuery({
    queryKey: ["decks", "draft"],
    queryFn: () => api.decks("draft"),
  });
  const candidates = useMemo(
    () => draftDeckCandidates(decksQuery.data ?? [], eventName),
    [decksQuery.data, eventName],
  );

  const pool = useMemo(() => poolCounts(picks), [picks]);
  const poolCardIDs = useMemo(() => [...pool.keys()].sort((a, b) => a - b), [pool]);

  // Several drafts can share one Arena event name (re-entries). The submitted
  // deck for THIS draft is the candidate whose cards overlap the pool most.
  const candidateQueries = useQueries({
    queries: candidates.map((candidate) => ({
      queryKey: ["deck", candidate.deckId],
      queryFn: () => api.deckDetail(candidate.deckId),
    })),
  });
  const { deckSummary, deckDetail } = useMemo(() => {
    let bestIndex = -1;
    let bestOverlap = -1;
    candidates.forEach((_, index) => {
      const detail = candidateQueries[index]?.data;
      if (!detail) {
        return;
      }
      const deckCounts = new Map<number, number>();
      for (const card of detail.cards ?? []) {
        deckCounts.set(card.cardId, (deckCounts.get(card.cardId) ?? 0) + card.quantity);
      }
      let overlap = 0;
      for (const [cardId, entry] of pool) {
        overlap += Math.min(entry.copies, deckCounts.get(cardId) ?? 0);
      }
      if (overlap > bestOverlap) {
        bestOverlap = overlap;
        bestIndex = index;
      }
    });
    if (bestIndex < 0) {
      return { deckSummary: null, deckDetail: null };
    }
    return { deckSummary: candidates[bestIndex], deckDetail: candidateQueries[bestIndex]?.data ?? null };
  }, [candidateQueries, candidates, pool]);

  const previewQueries = useQueries({
    queries: poolCardIDs.map((cardId) => ({
      queryKey: ["card-preview", cardId, pool.get(cardId)?.name ?? `Card ${cardId}`],
      queryFn: () => fetchCardPreview(cardId, pool.get(cardId)?.name),
      staleTime: 1000 * 60 * 60 * 24,
      gcTime: 1000 * 60 * 60 * 24,
      retry: 1,
    })),
  });
  const previewByCardID = useMemo(() => {
    const out = new Map<number, CardPreview>();
    poolCardIDs.forEach((cardId, index) => {
      const preview = previewQueries[index]?.data;
      if (preview) {
        out.set(cardId, preview);
      }
    });
    return out;
  }, [poolCardIDs, previewQueries]);

  const deckCards = useMemo(() => deckDetail?.cards ?? [], [deckDetail]);
  const poolCards = useMemo<PoolCard[]>(() => {
    const mainByCard = new Map<number, number>();
    const sideByCard = new Map<number, number>();
    for (const card of deckCards) {
      const target = card.section === "main" ? mainByCard : card.section === "sideboard" ? sideByCard : null;
      if (target) {
        target.set(card.cardId, (target.get(card.cardId) ?? 0) + card.quantity);
      }
    }
    return poolCardIDs.map((cardId) => {
      const entry = pool.get(cardId)!;
      const preview = previewByCardID.get(cardId);
      const mainCopies = mainByCard.get(cardId) ?? 0;
      return {
        cardId,
        cardName: entry.name ?? preview?.name,
        poolCopies: entry.copies,
        mainCopies,
        sideCopies: sideByCard.get(cardId) ?? 0,
        status: mainCopies > 0 ? "main" : (sideByCard.get(cardId) ?? 0) > 0 ? "side" : "unused",
        manaValue: typeof preview?.manaValue === "number" ? preview.manaValue : null,
        typeLine: preview?.typeLine,
        colors: preview?.colors,
      };
    });
  }, [deckCards, pool, poolCardIDs, previewByCardID]);

  const summary = useMemo(() => {
    let poolCopies = 0;
    let mainFromPool = 0;
    let distinctMain = 0;
    for (const card of poolCards) {
      poolCopies += card.poolCopies;
      mainFromPool += Math.min(card.mainCopies, card.poolCopies);
      if (card.mainCopies > 0) {
        distinctMain += 1;
      }
    }
    const mainNonPool = new Map<number, number>();
    const poolIDs = new Set(poolCardIDs);
    for (const card of deckCards) {
      if (card.section === "main" && !poolIDs.has(card.cardId)) {
        mainNonPool.set(card.cardId, (mainNonPool.get(card.cardId) ?? 0) + card.quantity);
      }
    }
    return { poolCopies, mainFromPool, distinctMain, mainNonPool };
  }, [deckCards, poolCardIDs, poolCards]);

  const mainColorCounts = useMemo(() => {
    const counts = new Map<string, number>();
    for (const card of poolCards) {
      if (card.mainCopies <= 0 || card.typeLine?.toLowerCase().includes("land")) {
        continue;
      }
      for (const color of card.colors ?? []) {
        counts.set(color, (counts.get(color) ?? 0) + card.mainCopies);
      }
    }
    return COLOR_ORDER.filter((color) => (counts.get(color) ?? 0) > 0).map((color) => ({
      color,
      count: counts.get(color) ?? 0,
    }));
  }, [poolCards]);

  const typeCounts = useMemo(() => {
    let creatures = 0;
    let spells = 0;
    let other = 0;
    for (const card of poolCards) {
      if (card.mainCopies <= 0 || !card.typeLine) {
        continue;
      }
      const lower = card.typeLine.toLowerCase();
      if (lower.includes("land")) {
        continue;
      }
      if (lower.includes("creature")) {
        creatures += card.mainCopies;
      } else if (lower.includes("instant") || lower.includes("sorcery")) {
        spells += card.mainCopies;
      } else {
        other += card.mainCopies;
      }
    }
    return { creatures, spells, other };
  }, [poolCards]);

  const isMetadataLoading = previewQueries.some((query) => query.isPending);

  if (picks.length === 0) {
    return null;
  }

  const statusOrder: PoolCardStatus[] = ["main", "side", "unused"];
  const sortedPoolCards = [...poolCards].sort((a, b) => {
    const statusDelta = statusOrder.indexOf(a.status) - statusOrder.indexOf(b.status);
    if (statusDelta !== 0) {
      return statusDelta;
    }
    const manaA = a.manaValue ?? Number.POSITIVE_INFINITY;
    const manaB = b.manaValue ?? Number.POSITIVE_INFINITY;
    if (manaA !== manaB) {
      return manaA - manaB;
    }
    return (a.cardName ?? "").localeCompare(b.cardName ?? "");
  });

  return (
    <section className="panel draft-pool-panel">
      <div className="panel-head">
        <div>
          <h3>Pool vs Submitted Deck</h3>
          <p>
            {deckSummary
              ? `Compared against the current list of the event deck`
              : "No submitted deck found for this event yet"}
          </p>
        </div>
        {deckSummary ? (
          <Link className="text-link" to={`/decks/${deckSummary.deckId}`}>
            Open deck
          </Link>
        ) : null}
      </div>

      {decksQuery.isLoading || candidateQueries.some((query) => query.isPending) ? (
        <StatusMessage>Loading submitted deck…</StatusMessage>
      ) : !deckSummary ? (
        <StatusMessage>
          The drafted pool is shown once Arena reports the event deck for this draft.
        </StatusMessage>
      ) : (
        <>
          <dl className="deck-analytics-tiles" aria-label="Pool usage summary">
            <div className="deck-analytics-tile">
              <dt>Picks</dt>
              <dd>
                <strong>{summary.poolCopies}</strong>
                <span>{poolCardIDs.length} distinct</span>
              </dd>
            </div>
            <div className="deck-analytics-tile">
              <dt>Made main deck</dt>
              <dd>
                <strong>{summary.mainFromPool}</strong>
                <span>
                  {summary.poolCopies > 0 ? pct(summary.mainFromPool / summary.poolCopies) : "—"} of picks
                </span>
              </dd>
            </div>
            <div className="deck-analytics-tile">
              <dt>Main colors</dt>
              <dd className="draft-pool-colors">
                {mainColorCounts.length === 0 ? (
                  <strong>—</strong>
                ) : (
                  mainColorCounts.map(({ color, count }) => (
                    <span className="draft-pool-color" key={color}>
                      <ManaSymbol token={color} />
                      <span>{count}</span>
                    </span>
                  ))
                )}
              </dd>
            </div>
            <div className="deck-analytics-tile">
              <dt>Main shape</dt>
              <dd>
                <strong>
                  {typeCounts.creatures}c / {typeCounts.spells}s
                </strong>
                <span>{typeCounts.other} other</span>
              </dd>
            </div>
            <div className="deck-analytics-tile">
              <dt>Added lands/cards</dt>
              <dd>
                <strong>{[...summary.mainNonPool.values()].reduce((sum, quantity) => sum + quantity, 0)}</strong>
                <span>not from pool</span>
              </dd>
            </div>
          </dl>

          <div className="draft-curve-row">
            <CurveBars title="Pool curve" buckets={curveBuckets(poolCards, (card) => card.poolCopies)} />
            <CurveBars title="Main deck curve" buckets={curveBuckets(poolCards, (card) => card.mainCopies)} />
          </div>
          {isMetadataLoading ? <StatusMessage>Loading card metadata…</StatusMessage> : null}

          <div className="table-wrap">
            <table className="data-table draft-pool-table">
              <thead>
                <tr>
                  <th>Card</th>
                  <th>Picked</th>
                  <th>Main</th>
                  <th>Status</th>
                </tr>
              </thead>
              <tbody>
                {sortedPoolCards.map((card) => (
                  <tr key={card.cardId} className={`is-${card.status}`}>
                    <td>
                      <CardPreviewName cardId={card.cardId} cardName={card.cardName} />
                    </td>
                    <td>{card.poolCopies}</td>
                    <td>{card.mainCopies > 0 ? card.mainCopies : "—"}</td>
                    <td>
                      <span className={`draft-pool-status is-${card.status}`}>{STATUS_LABELS[card.status]}</span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <p className="analytics-method-note">
            Compared against the deck's current list; later edits to the deck move cards between main and sideboard
            here too.
          </p>
        </>
      )}
    </section>
  );
}
