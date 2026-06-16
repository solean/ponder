import { useEffect, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useQuery } from "@tanstack/react-query";

import { fetchCardPreview } from "../lib/scryfall";

const POPOVER_WIDTH = 336;
const POPOVER_HEIGHT = 468;
const HORIZONTAL_GAP = 14;
const VIEWPORT_PADDING = 8;

type FloatingPosition = { top: number; left: number };

function displayName(cardId: number, cardName?: string): string {
  return cardName?.trim() || `Card ${cardId}`;
}

function scryfallHref(cardId: number, cardName?: string): string {
  const name = cardName?.trim();
  return name
    ? `https://scryfall.com/search?q=${encodeURIComponent(`!"${name}"`)}`
    : `https://scryfall.com/search?q=${encodeURIComponent(`arenaid:${cardId}`)}`;
}

// Position the popover beside the anchor in viewport coordinates so it can be
// rendered via a portal — escaping the live banner's (and table cells')
// `overflow: hidden`, which would otherwise clip an absolutely-positioned one.
function floatingPosition(anchor: HTMLElement): FloatingPosition {
  const rect = anchor.getBoundingClientRect();
  const viewportWidth = window.innerWidth || document.documentElement.clientWidth;
  const viewportHeight = window.innerHeight || document.documentElement.clientHeight;
  const availableRight = viewportWidth - rect.right;
  const availableLeft = rect.left;
  const placeLeft = availableRight < POPOVER_WIDTH + HORIZONTAL_GAP && availableLeft >= POPOVER_WIDTH + HORIZONTAL_GAP;

  const rawLeft = placeLeft ? rect.left - POPOVER_WIDTH - HORIZONTAL_GAP : rect.right + HORIZONTAL_GAP;
  const maxLeft = Math.max(VIEWPORT_PADDING, viewportWidth - POPOVER_WIDTH - VIEWPORT_PADDING);
  const left = Math.max(VIEWPORT_PADDING, Math.min(rawLeft, maxLeft));

  const rawTop = rect.top + rect.height / 2 - POPOVER_HEIGHT / 2;
  const maxTop = Math.max(VIEWPORT_PADDING, viewportHeight - POPOVER_HEIGHT - VIEWPORT_PADDING);
  const top = Math.max(VIEWPORT_PADDING, Math.min(rawTop, maxTop));

  return { top, left };
}

/**
 * A card name that reveals its Scryfall image preview on hover/focus and links
 * to Scryfall. The preview is portaled to <body> with fixed positioning so it
 * isn't clipped by overflow-hidden containers. Reuses the shared `.card-preview-*`
 * styles. Pass `label` to render custom trigger content (e.g. a quantity prefix).
 */
export function CardPreviewName({
  cardId,
  cardName,
  label,
}: {
  cardId: number;
  cardName?: string;
  label?: ReactNode;
}) {
  const [isOpen, setIsOpen] = useState(false);
  const [position, setPosition] = useState<FloatingPosition | null>(null);
  const anchorRef = useRef<HTMLAnchorElement | null>(null);
  const wrapperRef = useRef<HTMLDivElement | null>(null);
  const name = displayName(cardId, cardName);

  const openPopover = () => {
    if (anchorRef.current) {
      setPosition(floatingPosition(anchorRef.current));
    }
    setIsOpen(true);
  };

  const previewQuery = useQuery({
    queryKey: ["card-preview", cardId, name],
    queryFn: () => fetchCardPreview(cardId, cardName),
    enabled: isOpen,
    staleTime: 1000 * 60 * 60 * 24,
    gcTime: 1000 * 60 * 60 * 24,
    retry: 1,
  });

  useEffect(() => {
    if (!isOpen) {
      return;
    }
    const reposition = () => {
      if (anchorRef.current) {
        setPosition(floatingPosition(anchorRef.current));
      }
    };
    window.addEventListener("resize", reposition);
    window.addEventListener("scroll", reposition, true);
    return () => {
      window.removeEventListener("resize", reposition);
      window.removeEventListener("scroll", reposition, true);
    };
  }, [isOpen]);

  return (
    <div
      className="card-preview-anchor"
      ref={wrapperRef}
      onMouseEnter={openPopover}
      onMouseLeave={() => setIsOpen(false)}
    >
      <a
        className="card-preview-trigger"
        ref={anchorRef}
        href={previewQuery.data?.scryfallUrl ?? scryfallHref(cardId, cardName)}
        target="_blank"
        rel="noreferrer"
        onFocus={openPopover}
        onBlur={(event) => {
          if (wrapperRef.current && event.relatedTarget instanceof Node && wrapperRef.current.contains(event.relatedTarget)) {
            return;
          }
          setIsOpen(false);
        }}
        aria-label={`Open ${name} on Scryfall`}
      >
        {label ?? <code>{name}</code>}
      </a>

      {isOpen && position
        ? createPortal(
            <div
              className="card-preview-popover card-preview-popover-floating"
              role="tooltip"
              style={{ top: position.top, left: position.left, width: POPOVER_WIDTH, height: POPOVER_HEIGHT }}
            >
              {previewQuery.isLoading ? (
                <p className="card-preview-status">Loading preview…</p>
              ) : previewQuery.data ? (
                <img src={previewQuery.data.imageUrl} alt={previewQuery.data.name} loading="lazy" />
              ) : (
                <p className="card-preview-status">Preview unavailable.</p>
              )}
            </div>,
            document.body,
          )
        : null}
    </div>
  );
}
