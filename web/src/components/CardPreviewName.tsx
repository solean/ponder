import { useEffect, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useQuery } from "@tanstack/react-query";

import { fetchCardPreview } from "../lib/scryfall";

import { floatingCardPreviewPosition } from "../lib/cardPreviewPosition";

type FloatingPosition = ReturnType<typeof floatingCardPreviewPosition>;

function displayName(cardId: number, cardName?: string): string {
  return cardName?.trim() || `Card ${cardId}`;
}

function scryfallHref(cardId: number, cardName?: string): string {
  const name = cardName?.trim();
  return name
    ? `https://scryfall.com/search?q=${encodeURIComponent(`!"${name}"`)}`
    : `https://scryfall.com/search?q=${encodeURIComponent(`arenaid:${cardId}`)}`;
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
  resolveName = false,
  inline = false,
  passiveHover,
}: {
  cardId: number;
  cardName?: string;
  label?: ReactNode;
  /** Resolve a missing Arena card name before hover so audit-style lists stay readable. */
  resolveName?: boolean;
  /** Render a phrasing-content wrapper when the name appears inside prose. */
  inline?: boolean;
  /** Display-only preview driven by the overlay's passive native cursor feed. */
  passiveHover?: boolean;
}) {
  const [localOpen, setIsOpen] = useState(false);
  const isOpen = passiveHover ?? localOpen;
  const [position, setPosition] = useState<FloatingPosition | null>(null);
  const anchorRef = useRef<HTMLElement | null>(null);
  const wrapperRef = useRef<HTMLElement | null>(null);
  const knownName = cardName?.trim() ?? "";

  const openPopover = () => {
    if (anchorRef.current) {
      setPosition(floatingCardPreviewPosition(anchorRef.current));
    }
    setIsOpen(true);
  };

  const previewQuery = useQuery({
    queryKey: ["card-preview", cardId, displayName(cardId, cardName)],
    queryFn: () => fetchCardPreview(cardId, cardName),
    enabled: cardId > 0 && (isOpen || (resolveName && knownName.length === 0)),
    staleTime: 1000 * 60 * 60 * 24,
    gcTime: 1000 * 60 * 60 * 24,
    retry: 1,
  });
  const name = knownName || previewQuery.data?.name?.trim() || displayName(cardId);

  useEffect(() => {
    if (!isOpen) {
      return;
    }
    const reposition = () => {
      const anchor = anchorRef.current;
      if (!anchor) {
        return;
      }
      if (passiveHover === undefined && !anchor.matches(":hover") && document.activeElement !== anchor) {
        setIsOpen(false);
        return;
      }
      setPosition(floatingCardPreviewPosition(anchor));
    };
    if (passiveHover !== undefined) reposition();
    window.addEventListener("resize", reposition);
    window.addEventListener("scroll", reposition, true);
    return () => {
      window.removeEventListener("resize", reposition);
      window.removeEventListener("scroll", reposition, true);
    };
  }, [isOpen, passiveHover]);

  const Trigger = passiveHover === undefined ? "a" : "span";

  const content = (
    <>
      <Trigger
        className="card-preview-trigger"
        ref={(element) => { anchorRef.current = element; }}
        href={passiveHover === undefined ? previewQuery.data?.scryfallUrl ?? scryfallHref(cardId, cardName) : undefined}
        target={passiveHover === undefined ? "_blank" : undefined}
        rel={passiveHover === undefined ? "noreferrer" : undefined}
        onFocus={passiveHover === undefined ? openPopover : undefined}
        onBlur={(event) => {
          if (wrapperRef.current && event.relatedTarget instanceof Node && wrapperRef.current.contains(event.relatedTarget)) {
            return;
          }
          setIsOpen(false);
        }}
        aria-label={passiveHover === undefined ? `Open ${name} on Scryfall` : name}
      >
        {label ?? <code>{name}</code>}
      </Trigger>

      {isOpen && position
        ? createPortal(
            <div
              className="card-preview-popover card-preview-popover-floating"
              role="tooltip"
              style={position}
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
    </>
  );
  const anchorProps = {
    className: "card-preview-anchor",
    onMouseEnter: passiveHover === undefined ? openPopover : undefined,
    onMouseLeave: passiveHover === undefined ? () => setIsOpen(false) : undefined,
  };

  return inline ? (
    <span ref={(element) => { wrapperRef.current = element; }} {...anchorProps}>
      {content}
    </span>
  ) : (
    <div ref={(element) => { wrapperRef.current = element; }} {...anchorProps}>
      {content}
    </div>
  );
}
