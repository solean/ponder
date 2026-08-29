import { useId, useMemo } from "react";
import { useQueries } from "@tanstack/react-query";

import { CardPreviewName } from "./CardPreviewName";
import { StatusMessage } from "./StatusMessage";
import { draftPickLogPacks } from "../lib/draftReport";
import { fetchCardPreview, type CardPreview } from "../lib/scryfall";
import type { DraftPick, DraftPickCard } from "../lib/types";

function DraftCardList({
  cards,
  previews,
}: {
  cards: DraftPickCard[];
  previews: Map<number, CardPreview>;
}) {
  if (cards.length === 0) {
    return <span className="draft-card-empty">Selection unavailable</span>;
  }

  return (
    <div className="draft-card-list">
      {cards.map((card, index) => {
        const preview = previews.get(card.cardId);
        const name = card.cardName?.trim() || preview?.name?.trim() || `Card ${card.cardId}`;
        const artURL = preview?.artCropUrl ?? preview?.imageUrl;
        return (
          <CardPreviewName
            cardId={card.cardId}
            cardName={card.cardName}
            key={`${card.cardId}-${index}`}
            label={
              <>
                <span className="draft-pick-thumb">
                  {artURL ? <img src={artURL} alt="" loading="lazy" decoding="async" /> : null}
                </span>
                <code>{name}</code>
              </>
            }
            resolveName
          />
        );
      })}
    </div>
  );
}

export function DraftPickLog({ picks }: { picks: DraftPick[] }) {
  const headingID = useId();
  const packs = useMemo(() => draftPickLogPacks(picks), [picks]);

  // One deduped batch for the whole log: Scryfall lookups are collection-batched
  // and share React Query keys with the pool/journey panels on this page.
  const previewCards = useMemo(() => {
    const seen = new Map<number, DraftPickCard>();
    for (const pack of packs) {
      for (const pick of pack.picks) {
        for (const card of pick.pickedCards) {
          if (card.cardId > 0 && !seen.has(card.cardId)) {
            seen.set(card.cardId, card);
          }
        }
      }
    }
    return [...seen.values()].sort((left, right) => left.cardId - right.cardId);
  }, [packs]);

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

  return (
    <section className="panel draft-pick-log" aria-labelledby={headingID}>
      <div className="panel-head">
        <div>
          <h3 id={headingID}>Pick Log</h3>
          <p>Every recorded selection, grouped by pack</p>
        </div>
      </div>

      {packs.length === 0 ? (
        <StatusMessage>No picks were recorded for this draft.</StatusMessage>
      ) : (
        <div className="draft-pack-grid">
          {packs.map((pack) => (
            <article className="panel inner decklist-panel draft-pack-panel" key={pack.packNumber}>
              <h4>Pack {pack.displayPack}</h4>
              <div className="table-wrap draft-pack-table-wrap">
                <table className="data-table compact draft-pack-table">
                  <thead>
                    <tr>
                      <th>Pick</th>
                      <th>Selected Cards</th>
                    </tr>
                  </thead>
                  <tbody>
                    {pack.picks.map((pick) => (
                      <tr key={`${pack.packNumber}-${pick.pickNumber}`}>
                        <td>{pick.displayPick}</td>
                        <td>
                          <DraftCardList cards={pick.pickedCards} previews={previewByCardID} />
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </article>
          ))}
        </div>
      )}
    </section>
  );
}
