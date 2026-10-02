import { useQuery } from "@tanstack/react-query";
import {
  useEffect,
  useRef,
  useState,
  type CSSProperties,
  type FocusEvent,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";
import { formatDateTime } from "../../lib/format";
import {
  boardPlayMeta,
  boardTurnLabel,
  boardZoneKind,
  cardDisplayName,
  cardFallbackHref,
  parseManaCostParts,
  replayObjectCounterSummaries,
  replayObjectIsAttacking,
  replayObjectLoyalty,
  replayObjectName,
  replayObjectPTLabel,
  replayObjectStatePills,
  replayObjectStatusText,
  timelinePlayerLabel,
  timelineZoneLabel,
  type PreviewCard,
} from "../../lib/replay";
import { cardPreviewQueryKey } from "../../lib/replay/cardPreviewQuery";
import type { CardPreview } from "../../lib/scryfall";
import { fetchCardPreview } from "../../lib/scryfall";
import type { MatchCardPlay, MatchReplayFrameObject } from "../../lib/types";
import { ManaSymbol } from "../ManaSymbol";

import { floatingCardPreviewPosition } from "../../lib/cardPreviewPosition";

type PopoverPlacement = "left" | "right";

type ReplayRelationshipPill = {
  kind: "attached-to" | "attachment" | "linked";
  eyebrow: string;
  cardName: string;
};

function replayRelationshipPill(label: string): ReplayRelationshipPill {
  const attachedToPrefix = "Attached to ";
  if (label.startsWith(attachedToPrefix)) {
    return {
      kind: "attached-to",
      eyebrow: "Attached to",
      cardName: label.slice(attachedToPrefix.length),
    };
  }

  const attachmentPrefix = "Attached: ";
  if (label.startsWith(attachmentPrefix)) {
    return {
      kind: "attachment",
      eyebrow: "Attachment",
      cardName: label.slice(attachmentPrefix.length),
    };
  }

  return { kind: "linked", eyebrow: "Linked", cardName: label };
}

export function ManaCostDisplay({ manaCost }: { manaCost: string }) {
  const trimmed = manaCost.trim();
  if (!trimmed) {
    return <code className="deck-card-mana-cost">-</code>;
  }

  const parts = parseManaCostParts(trimmed);
  if (parts.length === 0) {
    return <code className="deck-card-mana-cost">{trimmed}</code>;
  }

  return (
    <span
      className="deck-card-mana-cost deck-card-mana-icons"
      aria-label={`Mana cost ${trimmed}`}
    >
      {parts.map((part, index) =>
        part.kind === "symbol" ? (
          <ManaSymbol
            key={`symbol-${part.token}-${index}`}
            token={part.token}
          />
        ) : (
          <span
            className="mana-symbol-separator"
            key={`sep-${part.value}-${index}`}
          >
            {part.value}
          </span>
        ),
      )}
    </span>
  );
}

function useFloatingCardPreviewPopover(isEnabled = true) {
  const [isOpen, setIsOpen] = useState(false);
  const [popoverPlacement, setPopoverPlacement] =
    useState<PopoverPlacement>("right");
  const [popoverStyle, setPopoverStyle] = useState<{
    top: number;
    left: number;
    width: number;
    height: number;
  }>({ top: 0, left: 0, width: 336, height: 468 });
  const wrapperRef = useRef<HTMLDivElement | null>(null);

  const updatePopoverPlacement = () => {
    if (typeof window === "undefined") {
      return;
    }

    const wrapper = wrapperRef.current;
    if (!wrapper) {
      return;
    }

    const position = floatingCardPreviewPosition(wrapper);
    setPopoverPlacement(position.left < wrapper.getBoundingClientRect().left ? "left" : "right");
    setPopoverStyle(position);
  };

  const openPopover = () => {
    if (!isEnabled) {
      return;
    }
    updatePopoverPlacement();
    setIsOpen(true);
  };

  const closePopover = () => {
    setIsOpen(false);
  };

  const handleBlur = (event: FocusEvent<HTMLElement>) => {
    if (
      wrapperRef.current &&
      event.relatedTarget instanceof Node &&
      wrapperRef.current.contains(event.relatedTarget)
    ) {
      return;
    }
    closePopover();
  };

  useEffect(() => {
    if (!isOpen) {
      return;
    }
    const onResize = () => updatePopoverPlacement();
    const onScroll = () => updatePopoverPlacement();
    window.addEventListener("resize", onResize);
    window.addEventListener("scroll", onScroll, true);
    return () => {
      window.removeEventListener("resize", onResize);
      window.removeEventListener("scroll", onScroll, true);
    };
  }, [isOpen]);

  useEffect(() => {
    if (isEnabled) {
      return;
    }
    setIsOpen(false);
  }, [isEnabled]);

  return {
    isOpen,
    popoverPlacement,
    popoverStyle,
    wrapperRef,
    openPopover,
    closePopover,
    handleBlur,
  };
}

export function CardPreviewName({ card }: { card: PreviewCard }) {
  const name = cardDisplayName(card);
  const fallbackHref = cardFallbackHref(card);
  const {
    isOpen,
    popoverPlacement,
    popoverStyle,
    wrapperRef,
    openPopover,
    closePopover,
    handleBlur,
  } = useFloatingCardPreviewPopover();

  const previewQuery = useQuery({
    queryKey: cardPreviewQueryKey(card),
    queryFn: () => fetchCardPreview(card.cardId, card.cardName),
    enabled: isOpen,
    staleTime: 1000 * 60 * 60 * 24,
    gcTime: 1000 * 60 * 60 * 24,
    retry: 1,
  });

  return (
    <div
      className="card-preview-anchor"
      data-popover-placement={popoverPlacement}
      ref={wrapperRef}
      onMouseEnter={openPopover}
      onMouseLeave={closePopover}
    >
      <a
        className="card-preview-trigger"
        href={previewQuery.data?.scryfallUrl ?? fallbackHref}
        target="_blank"
        rel="noreferrer"
        onFocus={openPopover}
        onBlur={handleBlur}
        aria-label={`Open ${name} on Scryfall`}
      >
        <code>{name}</code>
      </a>

      {isOpen && typeof document !== "undefined"
        ? createPortal(
            <div
              className="card-preview-popover card-preview-popover-floating"
              style={popoverStyle}
              role="tooltip"
            >
              {previewQuery.isLoading ? (
                <p className="card-preview-status">Loading preview…</p>
              ) : previewQuery.data ? (
                <img
                  src={previewQuery.data.imageUrl}
                  alt={previewQuery.data.name}
                  loading="lazy"
                />
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

export function ReplayCardPreviewAnchor({
  preview,
  wrapperClassName,
  wrapperStyle,
  children,
}: {
  preview: CardPreview | null;
  wrapperClassName?: string;
  wrapperStyle?: CSSProperties;
  children: ReactNode;
}) {
  const {
    isOpen,
    popoverPlacement,
    popoverStyle,
    wrapperRef,
    openPopover,
    closePopover,
    handleBlur,
  } = useFloatingCardPreviewPopover(Boolean(preview?.imageUrl));

  if (!preview?.imageUrl) {
    return <>{children}</>;
  }

  return (
    <div
      className={`card-preview-anchor is-replay-card${wrapperClassName ? ` ${wrapperClassName}` : ""}`}
      data-popover-placement={popoverPlacement}
      ref={wrapperRef}
      style={wrapperStyle}
      onMouseEnter={openPopover}
      onMouseLeave={closePopover}
      onFocus={openPopover}
      onBlur={handleBlur}
    >
      {children}
      {isOpen && typeof document !== "undefined"
        ? createPortal(
            <div
              className="card-preview-popover card-preview-popover-floating"
              style={popoverStyle}
              role="tooltip"
            >
              <img src={preview.imageUrl} alt="" width={336} height={468} />
            </div>,
            document.body,
          )
        : null}
    </div>
  );
}

export function MatchReplayCard({
  play,
  preview,
  active = false,
  size = "board",
}: {
  play: MatchCardPlay;
  preview: CardPreview | null;
  active?: boolean;
  size?: "board" | "stack";
}) {
  const card = { cardId: play.cardId, cardName: play.cardName };
  const name = preview?.name ?? cardDisplayName(card);
  const href = preview?.scryfallUrl ?? cardFallbackHref(card);

  return (
    <ReplayCardPreviewAnchor preview={preview}>
      <a
        className={`match-replay-card is-${size} ${active ? "is-active" : ""}`}
        href={href}
        target="_blank"
        rel="noreferrer"
        aria-label={`Open ${name} on Scryfall`}
        title={`${name} • ${timelinePlayerLabel(play.playerSide)} • ${timelineZoneLabel(play.firstPublicZone)} • ${
          play.playedAt ? formatDateTime(play.playedAt) : "Unknown time"
        }`}
      >
        {preview ? (
          <img
            src={preview.imageUrl}
            alt=""
            loading={size === "stack" ? "eager" : "lazy"}
            decoding="async"
            width={244}
            height={340}
          />
        ) : (
          <div className="match-replay-card-fallback">
            <strong>{name}</strong>
            <span>{timelineZoneLabel(play.firstPublicZone)}</span>
            <span>{boardPlayMeta(play)}</span>
          </div>
        )}
        <span className="match-replay-card-chip">
          {boardTurnLabel(play.turnNumber)}
        </span>
      </a>
    </ReplayCardPreviewAnchor>
  );
}

export function MatchReplayObjectCard({
  object,
  preview,
  previewByCardID,
  active = false,
  size = "board",
  chipLabel,
  shellRef,
  connectionHighlighted = false,
  onConnectionFocusChange,
  linkedExileObjects = [],
  relationshipLabels = [],
}: {
  object: MatchReplayFrameObject;
  preview: CardPreview | null;
  previewByCardID?: Map<number, CardPreview | null>;
  active?: boolean;
  size?: "board" | "stack" | "hand";
  chipLabel?: string;
  shellRef?: (element: HTMLDivElement | null) => void;
  connectionHighlighted?: boolean;
  onConnectionFocusChange?: (instanceId: number | null) => void;
  linkedExileObjects?: MatchReplayFrameObject[];
  relationshipLabels?: string[];
}) {
  const card = { cardId: object.cardId, cardName: object.cardName };
  const name = preview?.name ?? cardDisplayName(card);
  const href = preview?.scryfallUrl ?? cardFallbackHref(card);
  const linkedExileCards = linkedExileObjects.map((linkedObject) => {
    const linkedPreview = previewByCardID?.get(linkedObject.cardId) ?? null;
    return {
      object: linkedObject,
      preview: linkedPreview,
      name: replayObjectName(linkedObject, linkedPreview),
    };
  });
  const linkedExileSummary =
    linkedExileCards.length === 0
      ? null
      : linkedExileCards.length === 1
        ? `Exiling ${linkedExileCards[0]?.name ?? "1 card"}`
        : `Exiling ${linkedExileCards.length} cards`;
  const statusText = [
    replayObjectStatusText(object, preview),
    linkedExileSummary,
    ...relationshipLabels,
  ]
    .filter((part): part is string => Boolean(part))
    .join(" • ");
  // Tapped, attacking, and summoning sickness are already conveyed directly on
  // the card, so drop their text pills to keep the board compact.
  const statePills = replayObjectStatePills(object).filter(
    (pill) =>
      pill.label !== "Tapped" &&
      pill.label !== "Attacking" &&
      pill.label !== "Summoning Sick",
  );
  const allCounterPills = replayObjectCounterSummaries(object);
  const isBattlefieldBoardCard =
    size === "board" && boardZoneKind(object.zoneType) === "battlefield";
  const isTappedBoardCard = isBattlefieldBoardCard && object.isTapped;
  const isAttackingBoardCard =
    isBattlefieldBoardCard && replayObjectIsAttacking(object);
  const isSummoningSickBoardCard =
    isBattlefieldBoardCard && object.hasSummoningSickness;
  const loyalty = isBattlefieldBoardCard ? replayObjectLoyalty(object) : null;
  const counterPills =
    loyalty == null
      ? allCounterPills
      : allCounterPills.filter(
          (counter) => counter.label.trim().toLowerCase() !== "loyalty",
        );
  const cardAriaDetails = [
    loyalty == null ? null : `Current loyalty: ${loyalty}`,
    isSummoningSickBoardCard
      ? "Summoning sick: can't attack or use tap abilities this turn"
      : null,
    linkedExileSummary,
  ].filter((detail): detail is string => Boolean(detail));
  const statBadge =
    loyalty == null && isBattlefieldBoardCard
      ? replayObjectPTLabel(object, preview)
      : null;
  const visibleLinkedExileCards = isBattlefieldBoardCard
    ? linkedExileCards.slice(0, 2)
    : [];
  const hasLinkedExileCards = visibleLinkedExileCards.length > 0;

  const cardNode = (
    <ReplayCardPreviewAnchor preview={preview}>
      <a
        className={`match-replay-card is-${size} ${active ? "is-active" : ""} ${isTappedBoardCard ? "is-tapped" : ""} ${isSummoningSickBoardCard ? "is-summoning-sick" : ""} ${connectionHighlighted ? "is-connection-highlighted" : ""}`}
        href={href}
        target="_blank"
        rel="noreferrer"
        aria-label={`Open ${name} on Scryfall${cardAriaDetails.length > 0 ? `. ${cardAriaDetails.join(". ")}.` : ""}`}
        title={`${name} • ${statusText}`}
      >
        {preview ? (
          <img
            src={preview.imageUrl}
            alt=""
            loading={size === "stack" ? "eager" : "lazy"}
            decoding="async"
            width={244}
            height={340}
          />
        ) : (
          <div className="match-replay-card-fallback">
            <strong>{name}</strong>
            <span>{timelineZoneLabel(object.zoneType)}</span>
            <span>{timelinePlayerLabel(object.playerSide)}</span>
          </div>
        )}
        {chipLabel ? (
          <span className="match-replay-card-chip">{chipLabel}</span>
        ) : null}
        {statBadge ? (
          <span className="match-replay-card-power">{statBadge}</span>
        ) : null}
        {isSummoningSickBoardCard ? (
          <span className="match-replay-card-summoning-sick" aria-hidden="true">
            <SummoningSicknessGlyph />
          </span>
        ) : null}
        {loyalty != null ? (
          <span className="match-replay-card-loyalty" aria-hidden="true">
            <span>{loyalty.toLocaleString()}</span>
          </span>
        ) : null}
      </a>
    </ReplayCardPreviewAnchor>
  );

  if (size === "stack" || size === "hand") {
    return cardNode;
  }

  return (
    <div
      className={`match-replay-object ${isTappedBoardCard ? "is-tapped" : ""} ${isAttackingBoardCard ? "is-attacking" : ""} ${connectionHighlighted ? "is-connection-highlighted" : ""} ${hasLinkedExileCards ? "has-linked-exile" : ""} ${relationshipLabels.length > 0 ? "has-relationship" : ""}`}
      onMouseEnter={
        onConnectionFocusChange
          ? () => onConnectionFocusChange(object.instanceId)
          : undefined
      }
      onMouseLeave={
        onConnectionFocusChange
          ? () => onConnectionFocusChange(null)
          : undefined
      }
      onFocus={
        onConnectionFocusChange
          ? () => onConnectionFocusChange(object.instanceId)
          : undefined
      }
      onBlur={
        onConnectionFocusChange
          ? () => onConnectionFocusChange(null)
          : undefined
      }
    >
      <div className="match-replay-card-shell" ref={shellRef}>
        {visibleLinkedExileCards.length > 0 ? (
          <div className="match-replay-linked-exile-stack">
            {visibleLinkedExileCards.map((linkedCard, index) => (
              <ReplayCardPreviewAnchor
                key={linkedCard.object.instanceId}
                preview={linkedCard.preview}
                wrapperClassName="match-replay-linked-exile-card"
                wrapperStyle={
                  {
                    "--linked-exile-index": index,
                  } as CSSProperties
                }
              >
                <a
                  className="match-replay-linked-exile-anchor"
                  href={
                    linkedCard.preview?.scryfallUrl ??
                    cardFallbackHref({
                      cardId: linkedCard.object.cardId,
                      cardName: linkedCard.object.cardName,
                    })
                  }
                  target="_blank"
                  rel="noreferrer"
                  aria-label={`Open exiled card ${linkedCard.name} on Scryfall`}
                  title={`${linkedCard.name} • Exiled by ${name}`}
                >
                  {linkedCard.preview ? (
                    <img
                      src={linkedCard.preview.imageUrl}
                      alt=""
                      loading="lazy"
                      decoding="async"
                      width={244}
                      height={340}
                    />
                  ) : (
                    <div className="match-replay-linked-exile-fallback">
                      <span>{linkedCard.name}</span>
                    </div>
                  )}
                </a>
              </ReplayCardPreviewAnchor>
            ))}
            {linkedExileCards.length > visibleLinkedExileCards.length ? (
              <span className="match-replay-linked-exile-count">
                +{linkedExileCards.length - visibleLinkedExileCards.length}
              </span>
            ) : null}
          </div>
        ) : null}
        {cardNode}
      </div>
      {statePills.length > 0 ||
      counterPills.length > 0 ||
      relationshipLabels.length > 0 ? (
        <div
          className={`match-replay-card-statusrow ${relationshipLabels.length > 0 ? "has-relationship" : ""}`}
        >
          {statePills.map((pill) => (
            <span
              className="match-replay-state-pill"
              key={`${object.instanceId}-${pill.label}`}
            >
              {pill.label}
            </span>
          ))}
          {counterPills.map((counter) => (
            <span
              className="match-replay-state-pill is-counter"
              key={`${object.instanceId}-${counter.label}`}
            >
              {counter.count > 1
                ? `${counter.label} x${counter.count}`
                : counter.label}
            </span>
          ))}
          {relationshipLabels.map((label) => {
            const relationship = replayRelationshipPill(label);
            return (
              <span
                className={`match-replay-relationship-pill is-${relationship.kind}`}
                key={`${object.instanceId}-${label}`}
                title={label}
              >
                <span
                  className="match-replay-relationship-icon"
                  aria-hidden="true"
                >
                  <svg viewBox="0 0 20 20" focusable="false">
                    <path d="M7.6 12.4 6 14a3 3 0 0 1-4.2-4.2l2.8-2.8a3 3 0 0 1 4.2 0" />
                    <path d="m12.4 7.6 1.6-1.6a3 3 0 0 1 4.2 4.2L15.4 13a3 3 0 0 1-4.2 0" />
                    <path d="m7 13 6-6" />
                  </svg>
                </span>
                <span className="match-replay-relationship-copy">
                  <span className="match-replay-relationship-eyebrow">
                    {relationship.eyebrow}
                  </span>
                  <span className="match-replay-relationship-name">
                    {relationship.cardName}
                  </span>
                </span>
              </span>
            );
          })}
        </div>
      ) : null}
    </div>
  );
}

export function SummoningSicknessGlyph() {
  return (
    <svg viewBox="0 0 20 20" fill="none" focusable="false">
      <path d="M5 2.75h10M5 17.25h10" />
      <path d="M6.25 3.25v2.1c0 1.55.8 2.98 2.13 3.78L10 10l1.62-.87a4.4 4.4 0 0 0 2.13-3.78v-2.1M6.25 16.75v-2.1c0-1.55.8-2.98 2.13-3.78L10 10l1.62.87a4.4 4.4 0 0 1 2.13 3.78v2.1" />
      <path d="m7.3 15.85 2.7-2.2 2.7 2.2" />
    </svg>
  );
}
