import { useEffect, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";

import { fetchCardPreview, type CardPreview } from "../lib/scryfall";

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

function cardPreviewQueryKey(cardId: number, cardName?: string) {
  return ["card-preview", cardId, displayName(cardId, cardName)] as const;
}

function canFetchCardPreview(cardId: number, cardName?: string): boolean {
  return cardId > 0 || (cardId === 0 && (cardName?.trim() ?? "").length > 0);
}

function useCardPreviewQuery(cardId: number, cardName: string | undefined, enabled: boolean) {
  return useQuery({
    queryKey: cardPreviewQueryKey(cardId, cardName),
    queryFn: () => fetchCardPreview(cardId, cardName),
    enabled: enabled && canFetchCardPreview(cardId, cardName),
    staleTime: 1000 * 60 * 60 * 24,
    gcTime: 1000 * 60 * 60 * 24,
    retry: 1,
  });
}

type CardPreviewNameProps = {
  cardId: number;
  cardName?: string;
  label?: ReactNode;
  /** Resolve a missing Arena card name before hover so audit-style lists stay readable. */
  resolveName?: boolean;
  /** Render a phrasing-content wrapper when the name appears inside prose. */
  inline?: boolean;
  /** Display-only preview driven by the overlay's passive native cursor feed. */
  passiveHover?: boolean;
};

/**
 * A card name that reveals its Scryfall image preview on hover/focus and links
 * to Scryfall. The preview is portaled to <body> with fixed positioning so it
 * isn't clipped by overflow-hidden containers. Reuses the shared `.card-preview-*`
 * styles. Pass `label` to render custom trigger content (e.g. a quantity prefix).
 *
 * The preview query only mounts once the popover opens (or a missing name must
 * be resolved), so long tables of names don't each register a query observer.
 */
export function CardPreviewName(props: CardPreviewNameProps) {
  const needsName = props.resolveName === true && !props.cardName?.trim();
  return needsName ? <ResolvingCardPreviewName {...props} /> : <CardPreviewNameView {...props} />;
}

function ResolvingCardPreviewName(props: CardPreviewNameProps) {
  const query = useCardPreviewQuery(props.cardId, props.cardName, true);
  return <CardPreviewNameView {...props} resolvedName={query.data?.name} />;
}

function CardPreviewPopover({
  cardId,
  cardName,
  position,
}: {
  cardId: number;
  cardName?: string;
  position: FloatingPosition;
}) {
  const previewQuery = useCardPreviewQuery(cardId, cardName, true);
  return createPortal(
    <div className="card-preview-popover card-preview-popover-floating" role="tooltip" style={position}>
      {previewQuery.isLoading ? (
        <p className="card-preview-status">Loading preview…</p>
      ) : previewQuery.data ? (
        <img src={previewQuery.data.imageUrl} alt={previewQuery.data.name} loading="lazy" />
      ) : (
        <p className="card-preview-status">Preview unavailable.</p>
      )}
    </div>,
    document.body,
  );
}

function CardPreviewNameView({
  cardId,
  cardName,
  label,
  inline = false,
  passiveHover,
  resolvedName,
}: CardPreviewNameProps & { resolvedName?: string }) {
  const [localOpen, setIsOpen] = useState(false);
  const isOpen = passiveHover ?? localOpen;
  const [position, setPosition] = useState<FloatingPosition | null>(null);
  const anchorRef = useRef<HTMLElement | null>(null);
  const wrapperRef = useRef<HTMLElement | null>(null);
  const queryClient = useQueryClient();
  // Read the cache without subscribing: a preview fetched on an earlier hover
  // supplies the exact Scryfall URL and any resolved name.
  const cachedPreview = queryClient.getQueryData<CardPreview | null>(cardPreviewQueryKey(cardId, cardName));
  const name = cardName?.trim() || resolvedName?.trim() || cachedPreview?.name?.trim() || displayName(cardId);

  const openPopover = () => {
    if (anchorRef.current) {
      setPosition(floatingCardPreviewPosition(anchorRef.current));
    }
    setIsOpen(true);
  };

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
        href={passiveHover === undefined ? cachedPreview?.scryfallUrl ?? scryfallHref(cardId, cardName) : undefined}
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

      {isOpen && position ? <CardPreviewPopover cardId={cardId} cardName={cardName} position={position} /> : null}
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
