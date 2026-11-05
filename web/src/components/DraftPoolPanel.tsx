import { useMemo } from "react";
import { useQueries, useQuery } from "@tanstack/react-query";

import { ContextualLink } from "./Breadcrumbs";
import { DeckCurve } from "./DeckCurve";
import { StatusMessage } from "./StatusMessage";
import { api } from "../lib/api";
import type { CurveCard } from "../lib/deckCurve";
import { fetchCardPreview, type CardPreview } from "../lib/scryfall";
import type { DeckSummary, DraftPick } from "../lib/types";

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
  // The submitted main deck's cards that came from this pool, for the curve.
  const curveCards = useMemo<CurveCard[]>(() => {
    const mainByCard = new Map<number, number>();
    for (const card of deckCards) {
      if (card.section === "main") {
        mainByCard.set(card.cardId, (mainByCard.get(card.cardId) ?? 0) + card.quantity);
      }
    }
    return poolCardIDs.map((cardId) => {
      const entry = pool.get(cardId)!;
      const preview = previewByCardID.get(cardId);
      const mainCopies = mainByCard.get(cardId) ?? 0;
      return {
        cardId,
        name: entry.name?.trim() || preview?.name?.trim() || `Card ${cardId}`,
        copies: mainCopies,
        manaValue: typeof preview?.manaValue === "number" ? preview.manaValue : null,
        typeLine: preview?.typeLine,
        colors: preview?.colors,
      };
    });
  }, [deckCards, pool, poolCardIDs, previewByCardID]);

  const isMetadataLoading = previewQueries.some((query) => query.isPending);

  if (picks.length === 0) {
    return null;
  }

  return (
    <section className="panel draft-pool-panel">
      <div className="panel-head">
        <div>
          <h3>Draft Pool &amp; Deck</h3>
        </div>
        {deckSummary ? (
          <ContextualLink
            className="draft-deck-cta"
            to={`/decks/${deckSummary.deckId}`}
            title={`View ${deckSummary.deckName || `Deck ${deckSummary.deckId}`}`}
          >
            <span>View draft deck</span>
            <span aria-hidden="true">→</span>
          </ContextualLink>
        ) : null}
      </div>

      {decksQuery.isLoading || candidateQueries.some((query) => query.isPending) ? (
        <StatusMessage>Loading submitted deck…</StatusMessage>
      ) : !deckSummary ? (
        <StatusMessage>
          Arena has not reported a submitted deck for this draft yet.
        </StatusMessage>
      ) : (
        <>
          <DeckCurve title="Main deck curve" cards={curveCards} />
          {isMetadataLoading ? <StatusMessage>Loading card metadata…</StatusMessage> : null}
          <p className="analytics-method-note">
            Compared against the deck's current list; later edits update this summary too.
          </p>
        </>
      )}
    </section>
  );
}
