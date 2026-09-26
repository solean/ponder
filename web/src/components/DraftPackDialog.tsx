import { useEffect, useId, useMemo, useRef, useState, type CSSProperties } from "react";
import { createPortal } from "react-dom";
import { useQueries } from "@tanstack/react-query";

import { draftAlternativeCards, type DraftPickLogPick } from "../lib/draftReport";
import { fetchCardPreview, type CardPreview, type CardRarity } from "../lib/scryfall";
import type { DraftPickCard } from "../lib/types";

const RARITY_ORDER: Record<CardRarity, number> = { mythic: 0, rare: 1, uncommon: 2, common: 3 };

function DraftPackCard({ card, picked, preview, isPending }: {
  card: DraftPickCard;
  picked: boolean;
  preview?: CardPreview | null;
  isPending: boolean;
}) {
  const [imageLoaded, setImageLoaded] = useState(false);
  const [imageFailed, setImageFailed] = useState(false);
  const name = card.cardName?.trim() || preview?.name?.trim() || `Card ${card.cardId}`;
  const imageURL = preview?.imageUrl;
  const loading = isPending || Boolean(imageURL && !imageLoaded && !imageFailed);
  const search = card.cardName?.trim() || preview?.name?.trim();
  const href = preview?.scryfallUrl ?? `https://scryfall.com/search?q=${encodeURIComponent(search ? `!"${search}"` : `arenaid:${card.cardId}`)}`;

  return (
    <li className={`draft-pack-card${picked ? " is-picked" : ""}`}>
      <a
        className="draft-pack-card-image"
        href={href}
        target="_blank"
        rel="noreferrer"
        aria-label={`${name}${picked ? ", your pick" : ""}. Open on Scryfall`}
      >
        {!imageLoaded || imageFailed ? (
          <span className="draft-pack-card-placeholder">
            <span>{name}</span>
            <span className="draft-pack-card-status">{loading ? "Loading image…" : "Image unavailable"}</span>
          </span>
        ) : null}
        {imageURL && !imageFailed ? (
          <img
            src={imageURL}
            alt={name}
            loading="lazy"
            decoding="async"
            className={imageLoaded ? "is-loaded" : undefined}
            onLoad={() => setImageLoaded(true)}
            onError={() => setImageFailed(true)}
          />
        ) : null}
      </a>
      {picked ? <span className="draft-pack-card-picked">✓ Your pick</span> : null}
    </li>
  );
}

export function DraftPackDialog({
  pick,
  displayPack,
  onClose,
}: {
  pick: DraftPickLogPick;
  displayPack: number;
  onClose: () => void;
}) {
  const titleID = useId();
  const descriptionID = useId();
  const dialogRef = useRef<HTMLDialogElement>(null);
  const closeButtonRef = useRef<HTMLButtonElement>(null);
  const backdropPressRef = useRef(false);
  // The helper retains each unpicked card object, including duplicate copies.
  const alternatives = new Set(draftAlternativeCards(pick));
  const selectedCount = pick.packCards.length - alternatives.size;
  const previewCards = useMemo(
    () => [...new Map(pick.packCards.map((card) => [card.cardId, card])).values()],
    [pick.packCards],
  );
  const previewQueries = useQueries({
    queries: previewCards.map((card) => ({
      queryKey: ["card-preview", card.cardId, card.cardName?.trim() || `Card ${card.cardId}`],
      queryFn: () => fetchCardPreview(card.cardId, card.cardName),
      staleTime: 1000 * 60 * 60 * 24,
      gcTime: 1000 * 60 * 60 * 24,
      retry: 1,
    })),
  });
  const queryByCardID = new Map(previewCards.map((card, index) => [card.cardId, previewQueries[index]]));
  const cards = pick.packCards.map((card, index) => ({ card, index, query: queryByCardID.get(card.cardId) }));
  // Sort once the batch is ready so cards do not shuffle with each response.
  // Original indices keep duplicate copies and their picked state stable.
  if (previewQueries.every((query) => !query.isPending)) {
    cards.sort((left, right) => {
      const leftRarity = left.query?.data?.rarity;
      const rightRarity = right.query?.data?.rarity;
      return (leftRarity ? RARITY_ORDER[leftRarity] : 4) -
        (rightRarity ? RARITY_ORDER[rightRarity] : 4) || left.index - right.index;
    });
  }
  const dialogStyle = {
    "--draft-pack-width": `${Math.max(420, Math.min(7, cards.length) * 156 + 48)}px`,
  } as CSSProperties;

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;

    const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    dialog.showModal();
    closeButtonRef.current?.focus();

    return () => {
      dialog.close();
      document.body.style.overflow = previousOverflow;
      previousFocus?.focus({ preventScroll: true });
    };
  }, []);

  return createPortal(
    <dialog
      ref={dialogRef}
      className="draft-pack-dialog"
      style={dialogStyle}
      aria-modal="true"
      aria-labelledby={titleID}
      aria-describedby={descriptionID}
      onKeyDown={(event) => {
        if (event.key !== "Tab") return;
        const first = closeButtonRef.current;
        const links = event.currentTarget.querySelectorAll<HTMLAnchorElement>("a[href]");
        const last = links.item(links.length - 1) ?? first;
        if (event.shiftKey && document.activeElement === first) {
          event.preventDefault();
          last?.focus();
        } else if (!event.shiftKey && document.activeElement === last) {
          event.preventDefault();
          first?.focus();
        }
      }}
      onCancel={(event) => {
        event.preventDefault();
        onClose();
      }}
      onPointerDown={(event) => {
        const bounds = event.currentTarget.getBoundingClientRect();
        backdropPressRef.current = event.target === event.currentTarget && (
          event.clientX < bounds.left || event.clientX > bounds.right ||
          event.clientY < bounds.top || event.clientY > bounds.bottom
        );
      }}
      onClick={(event) => {
        if (backdropPressRef.current && event.target === event.currentTarget) onClose();
        backdropPressRef.current = false;
      }}
    >
      <div className="draft-pack-dialog-head">
        <div>
          <p className="draft-pack-dialog-eyebrow">Pack contents</p>
          <h2 id={titleID}>Pack {displayPack} <span aria-hidden="true">·</span> Pick {pick.displayPick}</h2>
          <p id={descriptionID} className="draft-pack-dialog-description">
            {pick.packCards.length} {pick.packCards.length === 1 ? "card" : "cards"}
            {selectedCount > 0 ? ` · Your ${selectedCount === 1 ? "pick is" : "picks are"} highlighted` : " · Selection unavailable"}
          </p>
        </div>
        <button ref={closeButtonRef} type="button" className="draft-pack-dialog-close" onClick={onClose}>
          Close <span aria-hidden="true">×</span>
        </button>
      </div>
      <div className="draft-pack-dialog-body">
        <ul className="draft-pack-card-grid" aria-label="Cards in this pack">
          {cards.map(({ card, index, query }) => (
            <DraftPackCard
              key={`${card.cardId}-${index}`}
              card={card}
              picked={!alternatives.has(card)}
              preview={query?.data}
              isPending={query?.isPending ?? false}
            />
          ))}
        </ul>
      </div>
    </dialog>,
    document.body,
  );
}
