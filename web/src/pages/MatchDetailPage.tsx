import {
  type CSSProperties,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type FocusEvent,
  type KeyboardEvent,
  type MutableRefObject,
  type ReactNode,
  type RefObject,
} from "react";
import { Link, useParams } from "react-router-dom";
import { useQueries, useQuery } from "@tanstack/react-query";
import { createPortal } from "react-dom";

import { EventLabel } from "../components/EventLabel";
import { MatchDeckColors } from "../components/MatchDeckColors";
import { ManaSymbol } from "../components/ManaSymbol";
import { ResultPill } from "../components/ResultPill";
import { StatusMessage } from "../components/StatusMessage";
import { api } from "../lib/api";
import { formatDateTime, formatDuration } from "../lib/format";
import { useEventSets } from "../lib/useEventSets";
import { fetchCardPreview } from "../lib/scryfall";
import type { CardPreview } from "../lib/scryfall";
import type {
  MatchCardPlay,
  MatchReplayFrame,
  MatchReplayFrameObject,
} from "../lib/types";
import {
  battlefieldSectionKind,
  battlefieldSectionLabel,
  battlefieldSectionOrder,
  boardPlayMeta,
  boardTurnLabel,
  boardZoneKind,
  boardZoneLabel,
  buildReplayBeat,
  buildReplayLifeSeries,
  buildReplayTickKinds,
  buildReplayTurnBoundaries,
  cardDisplayName,
  cardFallbackHref,
  filterMeaningfulReplayFrames,
  findReplayKeyMoments,
  isInspectableZoneKind,
  parseManaCostParts,
  preferredReplayFrameIndex,
  replayAnnotationDetailIntValue,
  replayAnnotationHasType,
  replayFrameAnnotations,
  replayFrameMomentLabel,
  replayLifeDelta,
  replayLifeSeriesDomain,
  replayMomentLabel,
  replayObjectBlockAttackerIDs,
  replayObjectCounterSummaries,
  replayObjectIsAttacking,
  replayObjectIsBlocking,
  replayObjectName,
  replayObjectPTLabel,
  replayObjectStatePills,
  replayObjectStatusText,
  replayTurnLabel,
  replayTurnValue,
  shouldRenderOnBattlefield,
  sortBattlefieldSectionObjects,
  sortReplayObjects,
  summarizeReplayFrameZones,
  summarizeReplayGame,
  summarizeReplayZones,
  timelinePhaseLabel,
  timelinePlayerLabel,
  timelineZoneLabel,
  type BattlefieldSectionKind,
  type BoardZoneKind,
  type InspectableZoneKind,
  type PreviewCard,
  type ReplayBoardConnection,
  type ReplayConnectionKind,
  type ReplayBeat,
  type ReplayGameGroup,
  type ReplayGameSummary,
  type ReplayKeyMoment,
  type ReplayLifePoint,
  type ReplayTickKind,
  type ReplayTurnBoundary,
} from "../lib/replay";
import {
  REPLAY_SPEED_OPTIONS,
  useReplayPlayer,
} from "../lib/replay/useReplayPlayer";
import { useReplayKeyboard } from "../lib/replay/useReplayKeyboard";

type OpponentDeckCard = {
  cardId: number;
  cardName?: string;
  quantity: number;
};
type PopoverPlacement = "left" | "right";
type TimelineDisplayMode = "board" | "list";
type MatchReplayZoneDialogState =
  | {
      source: "replay";
      side: "self" | "opponent";
      zone: InspectableZoneKind;
      objects: MatchReplayFrameObject[];
    }
  | {
      source: "observed";
      side: "self" | "opponent";
      zone: InspectableZoneKind;
      plays: MatchCardPlay[];
    };

function cardPreviewQueryKey(card: PreviewCard): [string, number, string] {
  return ["card-preview", card.cardId, cardDisplayName(card)];
}

function ManaCostDisplay({ manaCost }: { manaCost: string }) {
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
  }>({ top: 0, left: 0 });
  const wrapperRef = useRef<HTMLDivElement | null>(null);

  const updatePopoverPlacement = () => {
    if (typeof window === "undefined") {
      return;
    }

    const wrapper = wrapperRef.current;
    if (!wrapper) {
      return;
    }

    const rect = wrapper.getBoundingClientRect();
    const viewportWidth =
      window.innerWidth || document.documentElement.clientWidth;
    const viewportHeight =
      window.innerHeight || document.documentElement.clientHeight;
    const popoverWidth = 336;
    const popoverHeight = 468;
    const horizontalGap = 14;
    const verticalMargin = 10;
    const availableRight = viewportWidth - rect.right;
    const availableLeft = rect.left;
    let placement: PopoverPlacement;

    if (availableRight >= popoverWidth + horizontalGap) {
      placement = "right";
    } else if (availableLeft >= popoverWidth + horizontalGap) {
      placement = "left";
    } else {
      placement = availableRight >= availableLeft ? "right" : "left";
    }

    const left =
      placement === "right"
        ? rect.right + horizontalGap
        : rect.left - popoverWidth - horizontalGap;
    const maxTop = Math.max(
      verticalMargin,
      viewportHeight - popoverHeight - verticalMargin,
    );
    const centeredTop = rect.top + rect.height / 2 - popoverHeight / 2;
    const top = Math.max(verticalMargin, Math.min(centeredTop, maxTop));

    setPopoverPlacement(placement);
    setPopoverStyle({ top, left });
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

function CardPreviewName({ card }: { card: PreviewCard }) {
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
              style={{
                top: `${popoverStyle.top}px`,
                left: `${popoverStyle.left}px`,
              }}
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

function ReplayCardPreviewAnchor({
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
              style={{
                top: `${popoverStyle.top}px`,
                left: `${popoverStyle.left}px`,
              }}
              role="tooltip"
            >
              <img
                src={preview.imageUrl}
                alt=""
                width={336}
                height={468}
              />
            </div>,
            document.body,
          )
        : null}
    </div>
  );
}

function MatchReplayCard({
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

function MatchReplayObjectCard({
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
  ]
    .filter((part): part is string => Boolean(part))
    .join(" • ");
  // "Tapped" and "Attacking" are already conveyed visually (90° rotation and the
  // red attack border), so drop those text pills to keep the board compact.
  const statePills = replayObjectStatePills(object).filter(
    (pill) => pill.label !== "Tapped" && pill.label !== "Attacking",
  );
  const counterPills = replayObjectCounterSummaries(object);
  const isTappedBoardCard =
    size === "board" &&
    boardZoneKind(object.zoneType) === "battlefield" &&
    object.isTapped;
  const isAttackingBoardCard =
    size === "board" &&
    boardZoneKind(object.zoneType) === "battlefield" &&
    replayObjectIsAttacking(object);
  const statBadge =
    size === "board" && boardZoneKind(object.zoneType) === "battlefield"
      ? replayObjectPTLabel(object, preview)
      : null;
  const visibleLinkedExileCards =
    size === "board" && boardZoneKind(object.zoneType) === "battlefield"
      ? linkedExileCards.slice(0, 2)
      : [];
  const hasLinkedExileCards = visibleLinkedExileCards.length > 0;

  const cardNode = (
    <ReplayCardPreviewAnchor preview={preview}>
      <a
        className={`match-replay-card is-${size} ${active ? "is-active" : ""} ${isTappedBoardCard ? "is-tapped" : ""} ${connectionHighlighted ? "is-connection-highlighted" : ""}`}
        href={href}
        target="_blank"
        rel="noreferrer"
        aria-label={`Open ${name} on Scryfall${linkedExileSummary ? `. ${linkedExileSummary}.` : ""}`}
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
      </a>
    </ReplayCardPreviewAnchor>
  );

  if (size === "stack" || size === "hand") {
    return cardNode;
  }

  return (
    <div
      className={`match-replay-object ${isTappedBoardCard ? "is-tapped" : ""} ${isAttackingBoardCard ? "is-attacking" : ""} ${connectionHighlighted ? "is-connection-highlighted" : ""} ${hasLinkedExileCards ? "has-linked-exile" : ""}`}
      onMouseEnter={
        onConnectionFocusChange
          ? () => onConnectionFocusChange(object.instanceId)
          : undefined
      }
      onMouseLeave={
        onConnectionFocusChange ? () => onConnectionFocusChange(null) : undefined
      }
      onFocus={
        onConnectionFocusChange
          ? () => onConnectionFocusChange(object.instanceId)
          : undefined
      }
      onBlur={
        onConnectionFocusChange ? () => onConnectionFocusChange(null) : undefined
      }
    >
      <div className="match-replay-card-shell" ref={shellRef}>
        {visibleLinkedExileCards.length > 0 ? (
          <div
            className="match-replay-linked-exile-stack"
          >
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
      {statePills.length > 0 || counterPills.length > 0 ? (
        <div className="match-replay-card-statusrow">
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
              {counter.count > 1 ? `${counter.label} x${counter.count}` : counter.label}
            </span>
          ))}
        </div>
      ) : null}
    </div>
  );
}

function MatchReplayConnectionOverlay({
  surfaceRef,
  cardShellsRef,
  connections,
  focusedInstanceId,
}: {
  surfaceRef: RefObject<HTMLDivElement | null>;
  cardShellsRef: MutableRefObject<Map<number, HTMLDivElement>>;
  connections: ReplayBoardConnection[];
  focusedInstanceId: number | null;
}) {
  const combatMarkerId = useId();
  const spellTargetMarkerId = useId();
  const [snapshot, setSnapshot] = useState<{
    width: number;
    height: number;
    paths: Array<{
      key: string;
      d: string;
      kind: ReplayConnectionKind;
      highlighted: boolean;
    }>;
  } | null>(null);

  useEffect(() => {
    if (connections.length === 0) {
      setSnapshot(null);
      return;
    }

    const surfaceElement = surfaceRef.current;
    if (!surfaceElement) {
      setSnapshot(null);
      return;
    }

    let frameID = 0;
    let resizeObserver: ResizeObserver | null = null;

    const measure = () => {
      frameID = 0;
      const root = surfaceRef.current;
      if (!root) {
        setSnapshot(null);
        return;
      }

      const surfaceRect = root.getBoundingClientRect();
      if (surfaceRect.width <= 0 || surfaceRect.height <= 0) {
        setSnapshot(null);
        return;
      }

      const paths = connections.flatMap((connection) => {
        const sourceElement = cardShellsRef.current.get(connection.sourceId);
        const targetElement = cardShellsRef.current.get(connection.targetId);
        if (!sourceElement || !targetElement) {
          return [];
        }

        const sourceRect = sourceElement.getBoundingClientRect();
        const targetRect = targetElement.getBoundingClientRect();
        const startX =
          sourceRect.left + sourceRect.width / 2 - surfaceRect.left;
        const startY =
          sourceRect.top + sourceRect.height / 2 - surfaceRect.top;
        const endX = targetRect.left + targetRect.width / 2 - surfaceRect.left;
        const endY = targetRect.top + targetRect.height / 2 - surfaceRect.top;
        const d =
          connection.kind === "spellTarget"
            ? (() => {
                const deltaX = endX - startX;
                const direction = deltaX >= 0 ? 1 : -1;
                const horizontalPull =
                  Math.max(Math.abs(deltaX) * 0.38, 72) * direction;
                return `M ${startX} ${startY} C ${startX + horizontalPull} ${startY}, ${endX - horizontalPull} ${endY}, ${endX} ${endY}`;
              })()
            : (() => {
                const deltaY = endY - startY;
                return `M ${startX} ${startY} C ${startX} ${startY + deltaY * 0.34}, ${endX} ${endY - deltaY * 0.34}, ${endX} ${endY}`;
              })();

        return [
          {
            key: `${connection.kind}-${connection.sourceId}-${connection.targetId}`,
            d,
            kind: connection.kind,
            highlighted:
              focusedInstanceId !== null &&
              (connection.sourceId === focusedInstanceId ||
                connection.targetId === focusedInstanceId),
          },
        ];
      });

      setSnapshot({
        width: surfaceRect.width,
        height: surfaceRect.height,
        paths,
      });
    };

    const scheduleMeasure = () => {
      if (frameID !== 0) {
        return;
      }
      frameID = window.requestAnimationFrame(measure);
    };

    scheduleMeasure();
    if (typeof ResizeObserver !== "undefined") {
      resizeObserver = new ResizeObserver(() => {
        scheduleMeasure();
      });
      resizeObserver.observe(surfaceElement);
      for (const element of cardShellsRef.current.values()) {
        resizeObserver.observe(element);
      }
    }
    window.addEventListener("resize", scheduleMeasure);

    return () => {
      if (frameID !== 0) {
        window.cancelAnimationFrame(frameID);
      }
      resizeObserver?.disconnect();
      window.removeEventListener("resize", scheduleMeasure);
    };
  }, [surfaceRef, cardShellsRef, connections, focusedInstanceId]);

  if (!snapshot || snapshot.paths.length === 0) {
    return null;
  }

  const shouldMuteIdleLines =
    focusedInstanceId === null && snapshot.paths.length > 3;

  return (
    <svg
      className={`match-replay-connection-overlay ${shouldMuteIdleLines ? "is-muted" : ""}`}
      viewBox={`0 0 ${snapshot.width} ${snapshot.height}`}
      preserveAspectRatio="none"
      aria-hidden="true"
    >
      <defs>
        <marker
          id={combatMarkerId}
          markerWidth="10"
          markerHeight="10"
          refX="9"
          refY="5"
          orient="auto"
          markerUnits="strokeWidth"
        >
          <path
            d="M 0 0 L 10 5 L 0 10 z"
            fill="var(--combat-connection-line)"
          />
        </marker>
        <marker
          id={spellTargetMarkerId}
          markerWidth="10"
          markerHeight="10"
          refX="9"
          refY="5"
          orient="auto"
          markerUnits="strokeWidth"
        >
          <path
            d="M 0 0 L 10 5 L 0 10 z"
            fill="var(--spell-target-connection-line)"
          />
        </marker>
      </defs>
      {snapshot.paths.map((path) => (
        <path
          key={path.key}
          className={`match-replay-connection-path is-${path.kind === "spellTarget" ? "spell-target" : "combat"} ${path.highlighted ? "is-highlighted" : ""}`}
          d={path.d}
          markerEnd={`url(#${path.kind === "spellTarget" ? spellTargetMarkerId : combatMarkerId})`}
        />
      ))}
    </svg>
  );
}

function MatchReplayZoneDialog({
  state,
  previewByCardID,
  onClose,
}: {
  state: MatchReplayZoneDialogState | null;
  previewByCardID: Map<number, CardPreview | null>;
  onClose: () => void;
}) {
  const titleId = useId();
  const descriptionId = useId();
  const dialogRef = useRef<HTMLDivElement | null>(null);
  const closeButtonRef = useRef<HTMLButtonElement | null>(null);
  const previousFocusRef = useRef<HTMLElement | null>(null);

  useEffect(() => {
    if (!state) {
      return;
    }

    previousFocusRef.current =
      document.activeElement instanceof HTMLElement
        ? document.activeElement
        : null;
    const originalOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";

    const focusFrameID = window.requestAnimationFrame(() => {
      closeButtonRef.current?.focus();
    });

    const handleKeyDown = (event: globalThis.KeyboardEvent) => {
      if (!dialogRef.current) {
        return;
      }

      if (event.key === "Escape") {
        event.preventDefault();
        onClose();
        return;
      }

      if (event.key !== "Tab") {
        return;
      }

      const focusable = Array.from(
        dialogRef.current.querySelectorAll<HTMLElement>(
          'button:not([disabled]), a[href], [tabindex]:not([tabindex="-1"])',
        ),
      ).filter(
        (element) =>
          !element.hasAttribute("disabled") &&
          element.getAttribute("aria-hidden") !== "true",
      );

      if (focusable.length === 0) {
        event.preventDefault();
        dialogRef.current.focus();
        return;
      }

      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      const activeElement =
        document.activeElement instanceof HTMLElement
          ? document.activeElement
          : null;

      if (event.shiftKey) {
        if (!activeElement || activeElement === first) {
          event.preventDefault();
          last.focus();
        }
        return;
      }

      if (activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    };

    document.addEventListener("keydown", handleKeyDown);
    return () => {
      document.removeEventListener("keydown", handleKeyDown);
      window.cancelAnimationFrame(focusFrameID);
      document.body.style.overflow = originalOverflow;
      previousFocusRef.current?.focus();
    };
  }, [onClose, state]);

  if (!state || typeof document === "undefined") {
    return null;
  }

  const title = `${timelinePlayerLabel(state.side)} ${boardZoneLabel(state.zone)}`;
  const cardCount =
    state.source === "replay" ? state.objects.length : state.plays.length;
  const subtitle =
    state.source === "replay"
      ? `${cardCount} card${cardCount === 1 ? "" : "s"} currently in ${boardZoneLabel(state.zone).toLowerCase()} this step.`
      : `${cardCount} observed card${cardCount === 1 ? "" : "s"} first seen in ${boardZoneLabel(state.zone).toLowerCase()} in this game.`;
  const replayObjects =
    state.source === "replay" ? [...state.objects].sort(sortReplayObjects) : [];
  const observedPlays = state.source === "observed" ? [...state.plays] : [];

  return createPortal(
    <div
      className="match-replay-zone-dialog-backdrop"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) {
          onClose();
        }
      }}
    >
      <section
        ref={dialogRef}
        className="match-replay-zone-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={descriptionId}
        tabIndex={-1}
      >
        <div className="match-replay-zone-dialog-head">
          <div className="match-replay-zone-dialog-head-copy">
            <p className="match-replay-sidebox-label">Zone Viewer</p>
            <h5 id={titleId}>{title}</h5>
            <p
              id={descriptionId}
              className="match-replay-zone-dialog-description"
            >
              {subtitle}
            </p>
          </div>
          <button
            ref={closeButtonRef}
            type="button"
            className="match-replay-zone-dialog-close"
            onClick={onClose}
          >
            Close
          </button>
        </div>

        <div className="match-replay-zone-dialog-body">
          {state.source === "replay" ? (
            replayObjects.length === 0 ? (
              <p className="match-replay-empty">No cards in this zone.</p>
            ) : (
              <div
                className="match-replay-zone-dialog-grid"
                aria-label={`${title} cards`}
              >
                {replayObjects.map((object) => (
                  <div
                    className="match-replay-zone-dialog-card"
                    key={object.instanceId}
                  >
                    <MatchReplayObjectCard
                      object={object}
                      preview={previewByCardID.get(object.cardId) ?? null}
                      size="hand"
                    />
                  </div>
                ))}
              </div>
            )
          ) : observedPlays.length === 0 ? (
            <p className="match-replay-empty">No cards in this zone.</p>
          ) : (
            <div
              className="match-replay-zone-dialog-grid"
              aria-label={`${title} cards`}
            >
              {observedPlays.map((play) => (
                <div className="match-replay-zone-dialog-card" key={play.id}>
                  <MatchReplayCard
                    play={play}
                    preview={previewByCardID.get(play.cardId) ?? null}
                  />
                </div>
              ))}
            </div>
          )}
        </div>
      </section>
    </div>,
    document.body,
  );
}

function MatchReplayFrameSideSummary({
  side,
  objects,
  lifeTotal,
  includeHand = false,
  onOpenZone,
  variant = "box",
}: {
  side: "self" | "opponent";
  objects: MatchReplayFrameObject[];
  lifeTotal?: number;
  includeHand?: boolean;
  onOpenZone?: (state: MatchReplayZoneDialogState) => void;
  variant?: "box" | "rail";
}) {
  const sideObjects = useMemo(
    () => objects.filter((object) => object.playerSide === side),
    [objects, side],
  );
  const zoneCounts = useMemo(
    () => summarizeReplayFrameZones(sideObjects),
    [sideObjects],
  );

  if (variant === "rail") {
    const railZones: BoardZoneKind[] = ["graveyard", "exile", "revealed"];
    const visibleZones = railZones.filter(
      (kind) => (zoneCounts.get(kind) ?? 0) > 0,
    );

    return (
      <section
        className={`match-replay-zonerail is-${side}`}
        aria-label={`${timelinePlayerLabel(side)} off-board zones`}
      >
        <span className="match-replay-zonerail-label">
          {timelinePlayerLabel(side)}
        </span>
        {visibleZones.length === 0 ? (
          <span className="match-replay-zonerail-empty">
            No graveyard, exile, or revealed cards
          </span>
        ) : (
          visibleZones.map((kind) => {
            const count = zoneCounts.get(kind) ?? 0;
            const canOpen = isInspectableZoneKind(kind) && onOpenZone;
            const inner = (
              <>
                <span className="match-replay-zonechip-term">
                  {boardZoneLabel(kind)}
                </span>
                <span className="match-replay-zonechip-value">{count}</span>
              </>
            );

            if (canOpen) {
              return (
                <button
                  type="button"
                  className="match-replay-zonechip is-button"
                  key={kind}
                  aria-haspopup="dialog"
                  aria-label={`View ${timelinePlayerLabel(side)} ${boardZoneLabel(kind).toLowerCase()}, ${count} card${count === 1 ? "" : "s"}`}
                  onClick={() =>
                    onOpenZone({
                      source: "replay",
                      side,
                      zone: kind,
                      objects: sideObjects.filter(
                        (object) => boardZoneKind(object.zoneType) === kind,
                      ),
                    })
                  }
                >
                  {inner}
                </button>
              );
            }

            return (
              <span className="match-replay-zonechip" key={kind}>
                {inner}
              </span>
            );
          })
        )}
      </section>
    );
  }

  const stats: BoardZoneKind[] = includeHand
    ? ["hand", "battlefield", "graveyard", "exile", "revealed"]
    : ["battlefield", "graveyard", "exile", "revealed"];

  return (
    <section
      className={`match-replay-sidebox is-${side}`}
      aria-label={`${timelinePlayerLabel(side)} visible summary`}
    >
      <div className="match-replay-sidebox-head">
        <div className="match-replay-sidebox-head-copy">
          <p className="match-replay-sidebox-label">
            {timelinePlayerLabel(side)}
          </p>
          <p className="match-replay-sidebox-total">
            {sideObjects.length} visible card{sideObjects.length === 1 ? "" : "s"}
          </p>
        </div>
        {typeof lifeTotal === "number" ? (
          <div
            className="match-replay-sidebox-life-stat"
            aria-label={`${timelinePlayerLabel(side)} life total ${lifeTotal}`}
          >
            <span className="match-replay-sidebox-life-label">Life</span>
            <span className="match-replay-sidebox-life-value">{lifeTotal}</span>
          </div>
        ) : null}
      </div>
      <dl className="match-replay-stats">
        {stats.map((kind) => {
          const count = zoneCounts.get(kind) ?? 0;
          const canOpen = isInspectableZoneKind(kind) && count > 0 && onOpenZone;
          const content = (
            <>
              <span className="match-replay-stat-term">{boardZoneLabel(kind)}</span>
              <span className="match-replay-stat-value">{count}</span>
              {canOpen ? (
                <span className="match-replay-stat-hint">View cards</span>
              ) : null}
            </>
          );

          if (canOpen) {
            return (
              <button
                type="button"
                className="match-replay-stat match-replay-stat-button"
                key={kind}
                aria-haspopup="dialog"
                aria-label={`View ${timelinePlayerLabel(side)} ${boardZoneLabel(kind).toLowerCase()}, ${count} card${count === 1 ? "" : "s"}`}
                onClick={() =>
                  onOpenZone({
                    source: "replay",
                    side,
                    zone: kind,
                    objects: sideObjects.filter(
                      (object) => boardZoneKind(object.zoneType) === kind,
                    ),
                  })
                }
              >
                {content}
              </button>
            );
          }

          return (
            <div className="match-replay-stat" key={kind}>
              {content}
            </div>
          );
        })}
      </dl>
    </section>
  );
}

function MatchReplayFrameBattlefield({
  side,
  objects,
  previewByCardID,
  highlightedInstanceIDs,
  onRegisterCardShell,
  connectionHighlightedInstanceIDs,
  connectionInteractiveInstanceIDs,
  onConnectionFocusChange,
  linkedExileObjectsByParentId,
}: {
  side: "self" | "opponent";
  objects: MatchReplayFrameObject[];
  previewByCardID: Map<number, CardPreview | null>;
  highlightedInstanceIDs: Set<number>;
  onRegisterCardShell?: (instanceId: number, element: HTMLDivElement | null) => void;
  connectionHighlightedInstanceIDs?: Set<number>;
  connectionInteractiveInstanceIDs?: Set<number>;
  onConnectionFocusChange?: (instanceId: number | null) => void;
  linkedExileObjectsByParentId?: Map<number, MatchReplayFrameObject[]>;
}) {
  const sideObjects = useMemo(
    () => objects.filter((object) => object.playerSide === side),
    [objects, side],
  );
  const battlefieldObjects = useMemo(
    () =>
      sideObjects
        .filter((object) => boardZoneKind(object.zoneType) === "battlefield")
        .sort(sortReplayObjects),
    [sideObjects],
  );
  const battlefieldSections = useMemo(() => {
    const sectionOrder = battlefieldSectionOrder(side);
    const grouped = new Map<BattlefieldSectionKind, MatchReplayFrameObject[]>();
    for (const kind of sectionOrder) {
      grouped.set(kind, []);
    }

    for (const object of battlefieldObjects) {
      const preview = previewByCardID.get(object.cardId) ?? null;
      grouped.get(battlefieldSectionKind(preview, object))?.push(object);
    }

    return sectionOrder.map((kind) => ({
      kind,
      label: battlefieldSectionLabel(kind),
      objects: sortBattlefieldSectionObjects(
        kind,
        grouped.get(kind) ?? [],
        previewByCardID,
      ),
    })).filter((section) => section.objects.length > 0);
  }, [battlefieldObjects, previewByCardID, side]);
  const zoneCounts = useMemo(
    () => summarizeReplayFrameZones(sideObjects),
    [sideObjects],
  );
  const tappedCount = battlefieldObjects.filter((object) => object.isTapped).length;
  const attackingCount = battlefieldObjects.filter(replayObjectIsAttacking).length;
  const blockingCount = battlefieldObjects.filter(replayObjectIsBlocking).length;
  const sideBadges = (
    ["graveyard", "exile", "revealed"] as BoardZoneKind[]
  ).filter((kind) => (zoneCounts.get(kind) ?? 0) > 0);

  return (
    <section
      className={`match-replay-lane is-${side}`}
      aria-label={`${timelinePlayerLabel(side)} battlefield`}
    >
      <div className="match-replay-lane-head">
        <div>
          <p className="match-replay-lane-title">
            {timelinePlayerLabel(side)} Battlefield
          </p>
          <p className="match-replay-lane-subtitle">
            {battlefieldObjects.length} current on board
            {tappedCount > 0 ? ` • ${tappedCount} tapped` : ""}
            {attackingCount > 0 ? ` • ${attackingCount} attacking` : ""}
            {blockingCount > 0 ? ` • ${blockingCount} blocking` : ""}
            {sideBadges.length > 0
              ? ` • ${sideBadges.map((kind) => `${zoneCounts.get(kind)} ${boardZoneLabel(kind).toLowerCase()}`).join(" • ")}`
              : ""}
          </p>
        </div>
      </div>
      {battlefieldObjects.length === 0 ? (
        <p className="match-replay-empty">
          No battlefield cards in this frame.
        </p>
      ) : (
        <div className="match-replay-zone-groups">
          {battlefieldSections.map((section) => (
            <section
              key={section.kind}
              className="match-replay-zone-group"
              aria-label={`${timelinePlayerLabel(side)} ${section.label.toLowerCase()}`}
            >
              <div className="match-replay-zone-group-head">
                <p className="match-replay-zone-group-title">{section.label}</p>
                <p className="match-replay-zone-group-count">
                  {section.objects.length}
                </p>
              </div>
              <div className="match-replay-card-row is-sectioned">
                {section.objects.map((object) => (
                  <MatchReplayObjectCard
                    key={object.instanceId}
                    object={object}
                    preview={previewByCardID.get(object.cardId) ?? null}
                    previewByCardID={previewByCardID}
                    active={highlightedInstanceIDs.has(object.instanceId)}
                    shellRef={
                      onRegisterCardShell
                        ? (element) =>
                            onRegisterCardShell(object.instanceId, element)
                        : undefined
                    }
                    connectionHighlighted={
                      connectionHighlightedInstanceIDs?.has(object.instanceId) ??
                      false
                    }
                    onConnectionFocusChange={
                      connectionInteractiveInstanceIDs?.has(object.instanceId)
                        ? onConnectionFocusChange
                        : undefined
                    }
                    linkedExileObjects={
                      linkedExileObjectsByParentId?.get(object.instanceId) ?? []
                    }
                  />
                ))}
              </div>
            </section>
          ))}
        </div>
      )}
    </section>
  );
}

function MatchReplayHand({
  objects,
  previewByCardID,
  highlightedInstanceIDs,
}: {
  objects: MatchReplayFrameObject[];
  previewByCardID: Map<number, CardPreview | null>;
  highlightedInstanceIDs: Set<number>;
}) {
  const handObjects = useMemo(
    () =>
      objects
        .filter(
          (object) =>
            object.playerSide === "self" && boardZoneKind(object.zoneType) === "hand",
        )
        .sort(sortReplayObjects),
    [objects],
  );

  return (
    <section className="match-replay-lane is-hand" aria-label="Your hand">
      <div className="match-replay-lane-head">
        <div>
          <p className="match-replay-lane-title">Your Hand</p>
          <p className="match-replay-lane-subtitle">
            {handObjects.length} card{handObjects.length === 1 ? "" : "s"} currently in
            hand
          </p>
        </div>
      </div>
      {handObjects.length === 0 ? (
        <p className="match-replay-empty">No cards in hand in this step.</p>
      ) : (
        <div className="match-replay-card-row is-hand" aria-label="Current hand">
          {handObjects.map((object) => (
            <MatchReplayObjectCard
              key={object.instanceId}
              object={object}
              preview={previewByCardID.get(object.cardId) ?? null}
              active={highlightedInstanceIDs.has(object.instanceId)}
              size="hand"
            />
          ))}
        </div>
      )}
    </section>
  );
}

function MatchReplayStack({
  frame,
  previewByCardID,
  highlightedInstanceIDs,
  onRegisterCardShell,
  connectionHighlightedInstanceIDs,
  connectionInteractiveInstanceIDs,
  onConnectionFocusChange,
}: {
  frame: MatchReplayFrame;
  previewByCardID: Map<number, CardPreview | null>;
  highlightedInstanceIDs: Set<number>;
  onRegisterCardShell?: (instanceId: number, element: HTMLDivElement | null) => void;
  connectionHighlightedInstanceIDs?: Set<number>;
  connectionInteractiveInstanceIDs?: Set<number>;
  onConnectionFocusChange?: (instanceId: number | null) => void;
}) {
  const stackObjects = useMemo(
    () =>
      [...(frame.objects ?? [])]
        .filter((object) => boardZoneKind(object.zoneType) === "stack")
        .sort(sortReplayObjects),
    [frame],
  );
  const topObject = stackObjects[stackObjects.length - 1] ?? null;

  return (
    <section className="match-replay-stackbox" aria-label="Current stack">
      <div className="match-replay-stackbox-head">
        <div>
          <p className="match-replay-sidebox-label">Stack</p>
          <p className="match-replay-sidebox-total">
            {stackObjects.length === 0
              ? "Empty"
              : `${stackObjects.length} public card${stackObjects.length === 1 ? "" : "s"}`}
          </p>
        </div>
        {topObject ? (
          <p className="match-replay-stackbox-player">
            Top • {timelinePlayerLabel(topObject.playerSide)}
          </p>
        ) : null}
      </div>
      <div
        className={`match-replay-stackbox-body is-replay-stack ${stackObjects.length === 0 ? "is-empty" : ""}`}
      >
        <div
          className="match-replay-stack-cards"
          aria-label="Current stack ordered bottom to top"
        >
          {stackObjects.length === 0 ? (
            <p className="match-replay-empty">
              No public stack in this step.
            </p>
          ) : (
            stackObjects.map((object, index) => (
              <div
                className={`match-replay-stack-slot ${
                  connectionHighlightedInstanceIDs?.has(object.instanceId)
                    ? "is-connection-highlighted"
                    : ""
                }`}
                key={object.instanceId}
                onMouseEnter={
                  connectionInteractiveInstanceIDs?.has(object.instanceId) &&
                  onConnectionFocusChange
                    ? () => onConnectionFocusChange(object.instanceId)
                    : undefined
                }
                onMouseLeave={
                  connectionInteractiveInstanceIDs?.has(object.instanceId) &&
                  onConnectionFocusChange
                    ? () => onConnectionFocusChange(null)
                    : undefined
                }
                onFocus={
                  connectionInteractiveInstanceIDs?.has(object.instanceId) &&
                  onConnectionFocusChange
                    ? () => onConnectionFocusChange(object.instanceId)
                    : undefined
                }
                onBlur={
                  connectionInteractiveInstanceIDs?.has(object.instanceId) &&
                  onConnectionFocusChange
                    ? () => onConnectionFocusChange(null)
                    : undefined
                }
              >
                <div
                  className="match-replay-stack-card-shell"
                  ref={
                    onRegisterCardShell
                      ? (element) =>
                          onRegisterCardShell(object.instanceId, element)
                      : undefined
                  }
                >
                  <MatchReplayObjectCard
                    object={object}
                    preview={previewByCardID.get(object.cardId) ?? null}
                    active={
                      highlightedInstanceIDs.has(object.instanceId) ||
                      index === stackObjects.length - 1
                    }
                    size="stack"
                    chipLabel={
                      index === stackObjects.length - 1
                        ? "Top"
                        : `${index + 1}`
                    }
                    connectionHighlighted={
                      connectionHighlightedInstanceIDs?.has(object.instanceId) ??
                      false
                    }
                  />
                </div>
                <p className="match-replay-stack-slot-copy">
                  {timelinePlayerLabel(object.playerSide)}
                </p>
              </div>
            ))
          )}
        </div>
      </div>
    </section>
  );
}

const SCRUBBER_VIEW_W = 1000;
const SCRUBBER_VIEW_H = 64;
const SCRUBBER_LIFE_TOP = 7;
const SCRUBBER_LIFE_H = 31;
const SCRUBBER_TICK_TOP = 43;
const SCRUBBER_TICK_BOTTOM = 51;

function MatchReplayScrubber({
  length,
  index,
  onSeek,
  turnBoundaries,
  itemLabel,
  lifeSeries,
  tickKinds,
  keyMoments,
}: {
  length: number;
  index: number;
  onSeek: (index: number) => void;
  turnBoundaries: ReplayTurnBoundary[];
  itemLabel: "step" | "action";
  lifeSeries?: ReplayLifePoint[];
  tickKinds?: ReplayTickKind[];
  keyMoments?: ReplayKeyMoment[];
}) {
  const trackRef = useRef<HTMLDivElement | null>(null);
  const [isScrubbing, setIsScrubbing] = useState(false);
  const lastIndex = length > 0 ? length - 1 : 0;

  const xOf = (i: number) =>
    lastIndex > 0 ? (i / lastIndex) * SCRUBBER_VIEW_W : 0;
  const pctOf = (i: number) => (lastIndex > 0 ? (i / lastIndex) * 100 : 0);

  const domain =
    lifeSeries && lifeSeries.length > 0
      ? replayLifeSeriesDomain(lifeSeries)
      : null;
  const yOf = (value: number) => {
    if (!domain) {
      return SCRUBBER_LIFE_TOP + SCRUBBER_LIFE_H;
    }
    const span = domain.max - domain.min || 1;
    return SCRUBBER_LIFE_TOP + (1 - (value - domain.min) / span) * SCRUBBER_LIFE_H;
  };
  const lifePath = (side: "self" | "opponent") => {
    if (!lifeSeries) {
      return "";
    }
    const points: string[] = [];
    lifeSeries.forEach((point, i) => {
      const value = point[side];
      if (typeof value === "number") {
        points.push(`${xOf(i).toFixed(1)},${yOf(value).toFixed(2)}`);
      }
    });
    return points.join(" ");
  };

  const seekFromClientX = (clientX: number) => {
    const element = trackRef.current;
    if (!element) {
      return;
    }
    const rect = element.getBoundingClientRect();
    if (rect.width <= 0) {
      return;
    }
    const fraction = Math.max(
      0,
      Math.min(1, (clientX - rect.left) / rect.width),
    );
    onSeek(Math.round(fraction * lastIndex));
  };

  return (
    <div className="match-replay-scrubber">
      <div
        ref={trackRef}
        className={`match-replay-scrubber-track ${isScrubbing ? "is-scrubbing" : ""}`}
        role="slider"
        tabIndex={0}
        aria-label={`Replay ${itemLabel} position`}
        aria-valuemin={1}
        aria-valuemax={Math.max(length, 1)}
        aria-valuenow={index + 1}
        aria-valuetext={`${itemLabel} ${index + 1} of ${length}`}
        onPointerDown={(event) => {
          event.preventDefault();
          try {
            event.currentTarget.setPointerCapture(event.pointerId);
          } catch {
            // pointer capture is best-effort
          }
          setIsScrubbing(true);
          seekFromClientX(event.clientX);
        }}
        onPointerMove={(event) => {
          if (isScrubbing) {
            seekFromClientX(event.clientX);
          }
        }}
        onPointerUp={(event) => {
          setIsScrubbing(false);
          try {
            event.currentTarget.releasePointerCapture(event.pointerId);
          } catch {
            // pointer capture may already be released
          }
        }}
        onPointerCancel={() => setIsScrubbing(false)}
      >
        <svg
          className="match-replay-scrubber-svg"
          viewBox={`0 0 ${SCRUBBER_VIEW_W} ${SCRUBBER_VIEW_H}`}
          preserveAspectRatio="none"
          aria-hidden="true"
        >
          {turnBoundaries.map((boundary) => (
            <line
              key={`turn-${boundary.turnKey}-${boundary.firstIndex}`}
              className="match-replay-scrubber-turn-line"
              x1={xOf(boundary.firstIndex)}
              x2={xOf(boundary.firstIndex)}
              y1={0}
              y2={SCRUBBER_VIEW_H}
              vectorEffect="non-scaling-stroke"
            />
          ))}
          {tickKinds?.map((kind, i) =>
            kind === "other" ? null : (
              <line
                key={`tick-${i}`}
                className={`match-replay-scrubber-tick is-${kind}`}
                x1={xOf(i)}
                x2={xOf(i)}
                y1={SCRUBBER_TICK_TOP}
                y2={SCRUBBER_TICK_BOTTOM}
                vectorEffect="non-scaling-stroke"
              />
            ),
          )}
          {lifeSeries ? (
            <>
              <polyline
                className="match-replay-scrubber-life is-opponent"
                points={lifePath("opponent")}
                vectorEffect="non-scaling-stroke"
              />
              <polyline
                className="match-replay-scrubber-life is-self"
                points={lifePath("self")}
                vectorEffect="non-scaling-stroke"
              />
            </>
          ) : null}
        </svg>

        <div className="match-replay-scrubber-turn-labels" aria-hidden="true">
          {turnBoundaries.map((boundary) => (
            <span
              key={`turn-label-${boundary.turnKey}-${boundary.firstIndex}`}
              className="match-replay-scrubber-turn-label"
              style={{ left: `${pctOf(boundary.firstIndex)}%` }}
            >
              {boardTurnLabel(boundary.turnKey)}
            </span>
          ))}
        </div>

        {keyMoments?.map((moment) => (
          <button
            key={`moment-${moment.index}`}
            type="button"
            className={`match-replay-scrubber-pin is-${moment.kind}`}
            style={{ left: `${pctOf(moment.index)}%` }}
            title={moment.label}
            aria-label={`Jump to ${moment.label}`}
            onPointerDown={(event) => event.stopPropagation()}
            onClick={() => onSeek(moment.index)}
          />
        ))}

        <div
          className="match-replay-scrubber-head"
          style={{ left: `${pctOf(index)}%` }}
          aria-hidden="true"
        />
      </div>

      {lifeSeries ? (
        <div className="match-replay-scrubber-legend" aria-hidden="true">
          <span className="match-replay-scrubber-legend-item is-self">
            <span className="match-replay-scrubber-legend-swatch" /> You
          </span>
          <span className="match-replay-scrubber-legend-item is-opponent">
            <span className="match-replay-scrubber-legend-swatch" /> Opponent
          </span>
        </div>
      ) : null}
    </div>
  );
}

function MatchReplaySpeedControl({
  speed,
  onSelectSpeed,
}: {
  speed: number;
  onSelectSpeed: (speed: number) => void;
}) {
  return (
    <div
      className="match-replay-speed"
      role="group"
      aria-label="Playback speed"
    >
      <span className="match-replay-speed-label">Speed</span>
      {REPLAY_SPEED_OPTIONS.map((option) => (
        <button
          key={option}
          type="button"
          className={`match-replay-speed-button ${speed === option ? "is-active" : ""}`}
          aria-pressed={speed === option}
          onClick={() => onSelectSpeed(option)}
        >
          {option}×
        </button>
      ))}
    </div>
  );
}

function MatchReplayHudLife({
  side,
  life,
  delta,
  flashKey,
}: {
  side: "self" | "opponent";
  life?: number;
  delta: number | null;
  flashKey: number;
}) {
  return (
    <div className={`match-replay-hud-life is-${side}`}>
      <span className="match-replay-hud-avatar" aria-hidden="true">
        {side === "opponent" ? "OP" : "YOU"}
      </span>
      <div className="match-replay-hud-life-body">
        <p className="match-replay-hud-life-label">{timelinePlayerLabel(side)}</p>
        <div className="match-replay-hud-life-readout">
          <span className="match-replay-hud-life-value">
            {typeof life === "number" ? life : "—"}
          </span>
          {delta !== null ? (
            <span
              key={flashKey}
              className={`match-replay-hud-delta ${delta > 0 ? "is-up" : "is-down"}`}
              aria-label={`${timelinePlayerLabel(side)} life ${delta > 0 ? "gained" : "lost"} ${Math.abs(delta)}`}
            >
              {delta > 0 ? `+${delta}` : delta}
            </span>
          ) : null}
        </div>
      </div>
    </div>
  );
}

function MatchReplayHud({
  currentFrame,
  previousFrame,
  stepNumber,
  stepCount,
  beat,
}: {
  currentFrame: MatchReplayFrame;
  previousFrame: MatchReplayFrame | null;
  stepNumber: number;
  stepCount: number;
  beat: ReplayBeat;
}) {
  return (
    <section className="match-replay-hud" aria-label="Replay status">
      <MatchReplayHudLife
        side="opponent"
        life={currentFrame.opponentLifeTotal}
        delta={replayLifeDelta(previousFrame, currentFrame, "opponent")}
        flashKey={currentFrame.id}
      />
      <div className="match-replay-hud-center">
        <div className="match-replay-hud-meta">
          <span className="match-replay-hud-moment">
            {replayFrameMomentLabel(currentFrame)}
          </span>
          <span className="match-replay-hud-step">
            Step {stepNumber} / {stepCount}
          </span>
        </div>
        <p className="match-replay-hud-headline">
          {beat.text}
          {beat.note ? (
            <span className="match-replay-hud-headline-note"> — {beat.note}</span>
          ) : null}
        </p>
      </div>
      <MatchReplayHudLife
        side="self"
        life={currentFrame.selfLifeTotal}
        delta={replayLifeDelta(previousFrame, currentFrame, "self")}
        flashKey={currentFrame.id}
      />
    </section>
  );
}

function MatchReplayMoveList({
  frames,
  turnBoundaries,
  currentIndex,
  onSeek,
}: {
  frames: MatchReplayFrame[];
  turnBoundaries: ReplayTurnBoundary[];
  currentIndex: number;
  onSeek: (index: number) => void;
}) {
  const beats = useMemo(
    () =>
      frames.map((frame, index) =>
        buildReplayBeat(frame, index > 0 ? frames[index - 1] ?? null : null),
      ),
    [frames],
  );
  const activeRef = useRef<HTMLButtonElement | null>(null);

  useEffect(() => {
    activeRef.current?.scrollIntoView({ block: "nearest" });
  }, [currentIndex]);

  return (
    <aside className="match-replay-movelist" aria-label="Play-by-play">
      <p className="match-replay-movelist-title">Play-by-play</p>
      <div className="match-replay-movelist-scroll">
        {turnBoundaries.map((boundary) => (
          <div
            className="match-replay-movelist-turn"
            key={`${boundary.turnKey}-${boundary.firstIndex}`}
          >
            <p className="match-replay-movelist-turn-label">
              {replayTurnLabel(boundary.turnKey)}
            </p>
            {Array.from(
              { length: boundary.lastIndex - boundary.firstIndex + 1 },
              (_, offset) => boundary.firstIndex + offset,
            ).map((index) => {
              const isCurrent = index === currentIndex;
              const beat = beats[index];
              if (!beat) {
                return null;
              }
              return (
                <button
                  key={index}
                  ref={isCurrent ? activeRef : undefined}
                  type="button"
                  className={`match-replay-movelist-beat ${isCurrent ? "is-current" : ""}`}
                  aria-current={isCurrent ? "step" : undefined}
                  onClick={() => onSeek(index)}
                >
                  <span className="match-replay-movelist-beat-text">
                    {beat.text}
                  </span>
                  {beat.note ? (
                    <span className="match-replay-movelist-beat-note">
                      {" "}
                      · {beat.note}
                    </span>
                  ) : null}
                </button>
              );
            })}
          </div>
        ))}
      </div>
    </aside>
  );
}

function MatchReplayFrameBoard({
  gameNumber,
  frames,
  gameSummary,
  previewByCardID,
}: {
  gameNumber: number;
  frames: MatchReplayFrame[];
  gameSummary: ReplayGameSummary | null;
  previewByCardID: Map<number, CardPreview | null>;
}) {
  const {
    index: safeSelectedFrameIndex,
    setIndex: setSelectedFrameIndex,
    isPlaying,
    setIsPlaying,
    speed,
    setSpeed,
    lastIndex: lastFrameIndex,
  } = useReplayPlayer(frames.length, preferredReplayFrameIndex(frames));
  const [zoneDialogState, setZoneDialogState] =
    useState<MatchReplayZoneDialogState | null>(null);
  const [focusedConnectionInstanceId, setFocusedConnectionInstanceId] = useState<
    number | null
  >(null);
  const canvasRef = useRef<HTMLDivElement | null>(null);
  const replayCardShellsRef = useRef(new Map<number, HTMLDivElement>());

  const currentFrame = frames[safeSelectedFrameIndex] ?? null;
  const previousFrame =
    safeSelectedFrameIndex > 0
      ? frames[safeSelectedFrameIndex - 1] ?? null
      : null;
  const turnBoundaries = useMemo(() => buildReplayTurnBoundaries(frames), [frames]);
  const lifeSeries = useMemo(() => buildReplayLifeSeries(frames), [frames]);
  const tickKinds = useMemo(() => buildReplayTickKinds(frames), [frames]);
  const keyMoments = useMemo(() => findReplayKeyMoments(frames), [frames]);

  const currentTurnBoundaryIndex = currentFrame
    ? turnBoundaries.findIndex(
        (boundary) =>
          boundary.turnKey === replayTurnValue(currentFrame.turnNumber),
      )
    : -1;
  const currentObjects = currentFrame?.objects ?? [];
  const combatConnections = useMemo(() => {
    const battlefieldByID = new Map<number, MatchReplayFrameObject>();
    for (const object of currentObjects) {
      if (boardZoneKind(object.zoneType) !== "battlefield") {
        continue;
      }
      battlefieldByID.set(object.instanceId, object);
    }

    const next: ReplayBoardConnection[] = [];
    for (const object of battlefieldByID.values()) {
      if (!replayObjectIsBlocking(object)) {
        continue;
      }
      for (const attackerId of replayObjectBlockAttackerIDs(object)) {
        const attacker = battlefieldByID.get(attackerId);
        if (!attacker || !replayObjectIsAttacking(attacker)) {
          continue;
        }
        next.push({
          kind: "combat",
          sourceId: object.instanceId,
          targetId: attackerId,
        });
      }
    }
    return next;
  }, [currentObjects]);
  const spellTargetConnections = useMemo(() => {
    const battlefieldByID = new Map<number, MatchReplayFrameObject>();
    const stackByID = new Set<number>();
    for (const object of currentObjects) {
      const kind = boardZoneKind(object.zoneType);
      if (kind === "battlefield") {
        battlefieldByID.set(object.instanceId, object);
      }
      if (kind === "stack") {
        stackByID.add(object.instanceId);
      }
    }

    const seen = new Set<string>();
    const next: ReplayBoardConnection[] = [];
    for (const annotation of replayFrameAnnotations(currentFrame)) {
      if (!replayAnnotationHasType(annotation, "AnnotationType_TargetSpec")) {
        continue;
      }
      if (
        typeof annotation.affectorId !== "number" ||
        !stackByID.has(annotation.affectorId)
      ) {
        continue;
      }

      const affectedIds = Array.isArray(annotation.affectedIds)
        ? annotation.affectedIds
        : [];
      for (const targetId of affectedIds) {
        if (typeof targetId !== "number" || !battlefieldByID.has(targetId)) {
          continue;
        }
        const key = `${annotation.affectorId}-${targetId}`;
        if (seen.has(key)) {
          continue;
        }
        seen.add(key);
        next.push({
          kind: "spellTarget",
          sourceId: annotation.affectorId,
          targetId,
        });
      }
    }

    return next;
  }, [currentFrame, currentObjects]);
  const overlayConnections = useMemo(
    () => [...combatConnections, ...spellTargetConnections],
    [combatConnections, spellTargetConnections],
  );
  const linkedExileObjectsByParentId = useMemo(() => {
    const currentObjectsById = new Map<number, MatchReplayFrameObject>();
    for (const object of currentObjects) {
      currentObjectsById.set(object.instanceId, object);
    }

    const linkedIdsByParentId = new Map<number, Set<number>>();
    for (
      let frameIndex = 0;
      frameIndex <= safeSelectedFrameIndex;
      frameIndex += 1
    ) {
      const frame = frames[frameIndex] ?? null;
      for (const annotation of replayFrameAnnotations(frame)) {
        if (
          !replayAnnotationHasType(annotation, "AnnotationType_DisplayCardUnderCard")
        ) {
          continue;
        }
        if (typeof annotation.affectorId !== "number") {
          continue;
        }
        const affectedIds = Array.isArray(annotation.affectedIds)
          ? annotation.affectedIds.filter(
              (value): value is number => typeof value === "number",
            )
          : [];
        if (affectedIds.length === 0) {
          continue;
        }

        const isDisabled =
          replayAnnotationDetailIntValue(annotation, "Disable") === 1;
        if (isDisabled) {
          const existing = linkedIdsByParentId.get(annotation.affectorId);
          if (!existing) {
            continue;
          }
          for (const affectedId of affectedIds) {
            existing.delete(affectedId);
          }
          if (existing.size === 0) {
            linkedIdsByParentId.delete(annotation.affectorId);
          }
          continue;
        }

        let nextLinkedIds = linkedIdsByParentId.get(annotation.affectorId);
        if (!nextLinkedIds) {
          nextLinkedIds = new Set<number>();
          linkedIdsByParentId.set(annotation.affectorId, nextLinkedIds);
        }
        for (const affectedId of affectedIds) {
          nextLinkedIds.add(affectedId);
        }
      }
    }

    const next = new Map<number, MatchReplayFrameObject[]>();
    for (const [parentId, linkedIds] of linkedIdsByParentId) {
      const parentObject = currentObjectsById.get(parentId);
      if (!parentObject || boardZoneKind(parentObject.zoneType) !== "battlefield") {
        continue;
      }

      const linkedObjects = [...linkedIds]
        .map((linkedId) => currentObjectsById.get(linkedId) ?? null)
        .filter(
          (linkedObject): linkedObject is MatchReplayFrameObject =>
            linkedObject !== null &&
            boardZoneKind(linkedObject.zoneType) === "exile",
        )
        .sort(sortReplayObjects);
      if (linkedObjects.length > 0) {
        next.set(parentId, linkedObjects);
      }
    }

    return next;
  }, [currentObjects, frames, safeSelectedFrameIndex]);
  const overlayInteractiveInstanceIDs = useMemo(() => {
    const ids = new Set<number>();
    for (const connection of overlayConnections) {
      ids.add(connection.sourceId);
      ids.add(connection.targetId);
    }
    return ids;
  }, [overlayConnections]);
  const overlayHighlightedInstanceIDs = useMemo(() => {
    if (focusedConnectionInstanceId === null) {
      return new Set<number>();
    }

    const ids = new Set<number>([focusedConnectionInstanceId]);
    for (const connection of overlayConnections) {
      if (
        connection.sourceId === focusedConnectionInstanceId ||
        connection.targetId === focusedConnectionInstanceId
      ) {
        ids.add(connection.sourceId);
        ids.add(connection.targetId);
      }
    }
    return ids;
  }, [overlayConnections, focusedConnectionInstanceId]);
  const currentFrameChanges = currentFrame?.changes ?? [];
  const changedInstanceIDs = new Set(
    currentFrameChanges.map((change) => change.instanceId),
  );
  const currentBeat = currentFrame
    ? buildReplayBeat(currentFrame, previousFrame)
    : { text: "" };
  const canStepBackward = safeSelectedFrameIndex > 0;
  const canStepForward = safeSelectedFrameIndex < lastFrameIndex;
  const canJumpPrevTurn = currentTurnBoundaryIndex > 0;
  const canJumpNextTurn =
    currentTurnBoundaryIndex >= 0 &&
    currentTurnBoundaryIndex < turnBoundaries.length - 1;

  const goToFirstStep = () => {
    setIsPlaying(false);
    setSelectedFrameIndex(0);
  };
  const goToLastStep = () => {
    setIsPlaying(false);
    setSelectedFrameIndex(lastFrameIndex);
  };
  const goToPrevStep = () => {
    setIsPlaying(false);
    setSelectedFrameIndex(Math.max(safeSelectedFrameIndex - 1, 0));
  };
  const goToNextStep = () => {
    setIsPlaying(false);
    setSelectedFrameIndex(Math.min(safeSelectedFrameIndex + 1, lastFrameIndex));
  };
  const goToPrevTurn = () => {
    setIsPlaying(false);
    setSelectedFrameIndex(
      turnBoundaries[currentTurnBoundaryIndex - 1]?.firstIndex ?? 0,
    );
  };
  const goToNextTurn = () => {
    setIsPlaying(false);
    setSelectedFrameIndex(
      turnBoundaries[currentTurnBoundaryIndex + 1]?.firstIndex ??
        frames.length - 1,
    );
  };
  const togglePlay = () => setIsPlaying((currentValue) => !currentValue);

  useReplayKeyboard({
    onStepBackward: goToPrevStep,
    onStepForward: goToNextStep,
    onPrevTurn: goToPrevTurn,
    onNextTurn: goToNextTurn,
    onTogglePlay: togglePlay,
    onFirst: goToFirstStep,
    onLast: goToLastStep,
  });

  useEffect(() => {
    setFocusedConnectionInstanceId(null);
  }, [currentFrame?.id]);

  function registerCardShell(instanceId: number, element: HTMLDivElement | null) {
    if (element) {
      replayCardShellsRef.current.set(instanceId, element);
      return;
    }
    replayCardShellsRef.current.delete(instanceId);
  }

  if (!currentFrame) {
    return (
      <article className="panel inner match-replay-game">
        <div className="match-replay-head">
          <div className="match-replay-head-copy">
            <h4>Game {gameNumber}</h4>
            {gameSummary ? (
              <div className="match-replay-result">
                <ResultPill result={gameSummary.result} />
                <p className="match-replay-result-copy">{gameSummary.detail}</p>
              </div>
            ) : null}
          </div>
          <p className="match-replay-kicker">Replay</p>
        </div>
        <StatusMessage>No replay steps for this game.</StatusMessage>
      </article>
    );
  }

  return (
    <article className="panel inner match-replay-game">
      <div className="match-replay-head">
        <div className="match-replay-head-copy">
          <h4>Game {gameNumber}</h4>
          {gameSummary ? (
            <div className="match-replay-result">
              <ResultPill result={gameSummary.result} />
              <p className="match-replay-result-copy">{gameSummary.detail}</p>
            </div>
          ) : null}
        </div>
        <p className="match-replay-kicker">Replay</p>
      </div>

      <div className="match-replay-controls">
        <div className="match-replay-controls-bar">
          <div
            className="match-replay-button-row"
            role="group"
            aria-label={`Game ${gameNumber} replay controls`}
          >
            <button
              type="button"
              className="match-replay-button"
              onClick={togglePlay}
              aria-pressed={isPlaying}
            >
              {isPlaying ? "Pause" : "Play"}
            </button>
            <button
              type="button"
              className="match-replay-button"
              onClick={goToPrevTurn}
              disabled={!canJumpPrevTurn}
            >
              Previous Turn
            </button>
            <button
              type="button"
              className="match-replay-button"
              onClick={goToPrevStep}
              disabled={!canStepBackward}
            >
              Previous Step
            </button>
            <button
              type="button"
              className="match-replay-button"
              onClick={goToNextStep}
              disabled={!canStepForward}
            >
              Next Step
            </button>
            <button
              type="button"
              className="match-replay-button"
              onClick={goToNextTurn}
              disabled={!canJumpNextTurn}
            >
              Next Turn
            </button>
          </div>

          <div className="match-replay-controls-aux">
            <MatchReplaySpeedControl speed={speed} onSelectSpeed={setSpeed} />
            <p className="match-replay-kbd-hint">
              <kbd>←</kbd>
              <kbd>→</kbd> step · <kbd>⇧</kbd> turn · <kbd>space</kbd> play
            </p>
          </div>
        </div>

        <div className="match-replay-track-panel">
          <MatchReplayScrubber
            length={frames.length}
            index={safeSelectedFrameIndex}
            onSeek={(nextIndex) => {
              setIsPlaying(false);
              setSelectedFrameIndex(nextIndex);
            }}
            turnBoundaries={turnBoundaries}
            itemLabel="step"
            lifeSeries={lifeSeries}
            tickKinds={tickKinds}
            keyMoments={keyMoments}
          />
        </div>
      </div>

      <MatchReplayHud
        currentFrame={currentFrame}
        previousFrame={previousFrame}
        stepNumber={safeSelectedFrameIndex + 1}
        stepCount={frames.length}
        beat={currentBeat}
      />

      <div className="match-replay-canvas is-arena">
        <div className="match-replay-arena" ref={canvasRef}>
          <MatchReplayConnectionOverlay
            surfaceRef={canvasRef}
            cardShellsRef={replayCardShellsRef}
            connections={overlayConnections}
            focusedInstanceId={focusedConnectionInstanceId}
          />

          <div className="match-replay-arena-top">
            <MatchReplayFrameSideSummary
              side="opponent"
              objects={currentObjects}
              variant="rail"
              onOpenZone={setZoneDialogState}
            />
            <div className="match-replay-arena-stack">
              <MatchReplayStack
                frame={currentFrame}
                previewByCardID={previewByCardID}
                highlightedInstanceIDs={changedInstanceIDs}
                onRegisterCardShell={registerCardShell}
                connectionHighlightedInstanceIDs={overlayHighlightedInstanceIDs}
                connectionInteractiveInstanceIDs={overlayInteractiveInstanceIDs}
                onConnectionFocusChange={setFocusedConnectionInstanceId}
              />
            </div>
          </div>

          <MatchReplayFrameBattlefield
            side="opponent"
            objects={currentObjects}
            previewByCardID={previewByCardID}
            highlightedInstanceIDs={changedInstanceIDs}
            onRegisterCardShell={registerCardShell}
            connectionHighlightedInstanceIDs={overlayHighlightedInstanceIDs}
            connectionInteractiveInstanceIDs={overlayInteractiveInstanceIDs}
            onConnectionFocusChange={setFocusedConnectionInstanceId}
            linkedExileObjectsByParentId={linkedExileObjectsByParentId}
          />

          <MatchReplayFrameBattlefield
            side="self"
            objects={currentObjects}
            previewByCardID={previewByCardID}
            highlightedInstanceIDs={changedInstanceIDs}
            onRegisterCardShell={registerCardShell}
            connectionHighlightedInstanceIDs={overlayHighlightedInstanceIDs}
            connectionInteractiveInstanceIDs={overlayInteractiveInstanceIDs}
            onConnectionFocusChange={setFocusedConnectionInstanceId}
            linkedExileObjectsByParentId={linkedExileObjectsByParentId}
          />

          <MatchReplayFrameSideSummary
            side="self"
            objects={currentObjects}
            variant="rail"
            onOpenZone={setZoneDialogState}
          />

          <MatchReplayHand
            objects={currentObjects}
            previewByCardID={previewByCardID}
            highlightedInstanceIDs={changedInstanceIDs}
          />
        </div>

        <MatchReplayMoveList
          frames={frames}
          turnBoundaries={turnBoundaries}
          currentIndex={safeSelectedFrameIndex}
          onSeek={(nextIndex) => {
            setIsPlaying(false);
            setSelectedFrameIndex(nextIndex);
          }}
        />
      </div>

      <MatchReplayZoneDialog
        state={zoneDialogState}
        previewByCardID={previewByCardID}
        onClose={() => setZoneDialogState(null)}
      />
    </article>
  );
}

function MatchReplaySideSummary({
  side,
  plays,
  onOpenZone,
}: {
  side: "self" | "opponent";
  plays: MatchCardPlay[];
  onOpenZone?: (state: MatchReplayZoneDialogState) => void;
}) {
  const zoneCounts = useMemo(() => summarizeReplayZones(plays), [plays]);
  const stats: BoardZoneKind[] = [
    "battlefield",
    "graveyard",
    "exile",
    "revealed",
  ];

  return (
    <section
      className={`match-replay-sidebox is-${side}`}
      aria-label={`${timelinePlayerLabel(side)} observed summary`}
    >
      <div className="match-replay-sidebox-head">
        <div>
          <p className="match-replay-sidebox-label">
            {timelinePlayerLabel(side)}
          </p>
          <p className="match-replay-sidebox-total">
            {plays.length} observed card{plays.length === 1 ? "" : "s"}
          </p>
        </div>
      </div>
      <dl className="match-replay-stats">
        {stats.map((kind) => {
          const count = zoneCounts.get(kind) ?? 0;
          const canOpen = isInspectableZoneKind(kind) && count > 0 && onOpenZone;
          const content = (
            <>
              <span className="match-replay-stat-term">{boardZoneLabel(kind)}</span>
              <span className="match-replay-stat-value">{count}</span>
              {canOpen ? (
                <span className="match-replay-stat-hint">View cards</span>
              ) : null}
            </>
          );

          if (canOpen) {
            return (
              <button
                type="button"
                className="match-replay-stat match-replay-stat-button"
                key={kind}
                aria-haspopup="dialog"
                aria-label={`View ${timelinePlayerLabel(side)} ${boardZoneLabel(kind).toLowerCase()}, ${count} card${count === 1 ? "" : "s"}`}
                onClick={() =>
                  onOpenZone({
                    source: "observed",
                    side,
                    zone: kind,
                    plays: plays.filter(
                      (play) => boardZoneKind(play.firstPublicZone) === kind,
                    ),
                  })
                }
              >
                {content}
              </button>
            );
          }

          return (
            <div className="match-replay-stat" key={kind}>
              {content}
            </div>
          );
        })}
      </dl>
    </section>
  );
}

function MatchReplayBattlefield({
  side,
  plays,
  activePlayID,
  previewByCardID,
}: {
  side: "self" | "opponent";
  plays: MatchCardPlay[];
  activePlayID: number;
  previewByCardID: Map<number, CardPreview | null>;
}) {
  const battlefieldPlays = useMemo(
    () =>
      plays.filter((play) =>
        shouldRenderOnBattlefield(
          play,
          previewByCardID.get(play.cardId) ?? null,
          activePlayID,
        ),
      ),
    [activePlayID, plays, previewByCardID],
  );
  const battlefieldSections = useMemo(() => {
    const sectionOrder = battlefieldSectionOrder(side);
    const grouped = new Map<BattlefieldSectionKind, MatchCardPlay[]>();
    for (const kind of sectionOrder) {
      grouped.set(kind, []);
    }

    for (const play of battlefieldPlays) {
      const preview = previewByCardID.get(play.cardId) ?? null;
      grouped.get(battlefieldSectionKind(preview))?.push(play);
    }

    return sectionOrder.map((kind) => ({
      kind,
      label: battlefieldSectionLabel(kind),
      plays: grouped.get(kind) ?? [],
    })).filter((section) => section.plays.length > 0);
  }, [battlefieldPlays, previewByCardID, side]);
  const zoneCounts = useMemo(() => summarizeReplayZones(plays), [plays]);
  const sideBadges = (
    ["graveyard", "exile", "revealed"] as BoardZoneKind[]
  ).filter((kind) => (zoneCounts.get(kind) ?? 0) > 0);

  return (
    <section
      className={`match-replay-lane is-${side}`}
      aria-label={`${timelinePlayerLabel(side)} battlefield`}
    >
      <div className="match-replay-lane-head">
        <div>
          <p className="match-replay-lane-title">
            {timelinePlayerLabel(side)} Battlefield
          </p>
          <p className="match-replay-lane-subtitle">
            {battlefieldPlays.length} observed on board
            {sideBadges.length > 0
              ? ` • ${sideBadges.map((kind) => `${zoneCounts.get(kind)} ${boardZoneLabel(kind).toLowerCase()}`).join(" • ")}`
              : ""}
          </p>
        </div>
      </div>
      {battlefieldPlays.length === 0 ? (
        <p className="match-replay-empty">No battlefield cards observed yet.</p>
      ) : (
        <div className="match-replay-zone-groups">
          {battlefieldSections.map((section) => (
            <section
              key={section.kind}
              className="match-replay-zone-group"
              aria-label={`${timelinePlayerLabel(side)} ${section.label.toLowerCase()}`}
            >
              <div className="match-replay-zone-group-head">
                <p className="match-replay-zone-group-title">{section.label}</p>
                <p className="match-replay-zone-group-count">
                  {section.plays.length}
                </p>
              </div>
              <div className="match-replay-card-row is-sectioned">
                {section.plays.map((play) => (
                  <MatchReplayCard
                    key={play.id}
                    play={play}
                    preview={previewByCardID.get(play.cardId) ?? null}
                    active={play.id === activePlayID}
                  />
                ))}
              </div>
            </section>
          ))}
        </div>
      )}
    </section>
  );
}

function MatchTimelineBoard({
  gameNumber,
  plays,
  previewByCardID,
}: {
  gameNumber: number;
  plays: MatchCardPlay[];
  previewByCardID: Map<number, CardPreview | null>;
}) {
  const {
    index: selectedActionIndex,
    setIndex: setSelectedActionIndex,
    isPlaying,
    setIsPlaying,
    speed,
    setSpeed,
  } = useReplayPlayer(plays.length, plays.length > 0 ? plays.length - 1 : 0);
  const [zoneDialogState, setZoneDialogState] =
    useState<MatchReplayZoneDialogState | null>(null);

  const currentAction = plays[selectedActionIndex] ?? null;
  const visiblePlays = useMemo(
    () => plays.slice(0, selectedActionIndex + 1),
    [plays, selectedActionIndex],
  );
  const opponentVisiblePlays = visiblePlays.filter(
    (play) => play.playerSide === "opponent",
  );
  const selfVisiblePlays = visiblePlays.filter(
    (play) => play.playerSide === "self",
  );
  const unknownVisiblePlays = visiblePlays.filter(
    (play) => play.playerSide === "unknown",
  );
  const turnBoundaries = useMemo(() => buildReplayTurnBoundaries(plays), [plays]);

  const currentTurnBoundaryIndex = currentAction
    ? turnBoundaries.findIndex(
        (boundary) =>
          boundary.turnKey === replayTurnValue(currentAction.turnNumber),
      )
    : -1;

  const lastActionIndex = plays.length > 0 ? plays.length - 1 : 0;
  const goToFirstAction = () => {
    setIsPlaying(false);
    setSelectedActionIndex(0);
  };
  const goToLastAction = () => {
    setIsPlaying(false);
    setSelectedActionIndex(lastActionIndex);
  };
  const goToPrevAction = () => {
    setIsPlaying(false);
    setSelectedActionIndex((currentIndex) => Math.max(currentIndex - 1, 0));
  };
  const goToNextAction = () => {
    setIsPlaying(false);
    setSelectedActionIndex((currentIndex) =>
      Math.min(currentIndex + 1, lastActionIndex),
    );
  };
  const goToPrevActionTurn = () => {
    setIsPlaying(false);
    setSelectedActionIndex(
      turnBoundaries[currentTurnBoundaryIndex - 1]?.firstIndex ?? 0,
    );
  };
  const goToNextActionTurn = () => {
    setIsPlaying(false);
    setSelectedActionIndex(
      turnBoundaries[currentTurnBoundaryIndex + 1]?.firstIndex ??
        lastActionIndex,
    );
  };
  const togglePlay = () => setIsPlaying((currentValue) => !currentValue);

  useReplayKeyboard({
    onStepBackward: goToPrevAction,
    onStepForward: goToNextAction,
    onPrevTurn: goToPrevActionTurn,
    onNextTurn: goToNextActionTurn,
    onTogglePlay: togglePlay,
    onFirst: goToFirstAction,
    onLast: goToLastAction,
  });

  if (!currentAction) {
    return (
      <article className="panel inner match-replay-game">
        <h4>Game {gameNumber}</h4>
        <StatusMessage>No observed card plays for this game.</StatusMessage>
      </article>
    );
  }

  const currentPreview = previewByCardID.get(currentAction.cardId) ?? null;
  const currentName =
    currentPreview?.name ??
    cardDisplayName({
      cardId: currentAction.cardId,
      cardName: currentAction.cardName,
    });
  const canStepBackward = selectedActionIndex > 0;
  const canStepForward = selectedActionIndex < plays.length - 1;
  const canJumpPrevTurn = currentTurnBoundaryIndex > 0;
  const canJumpNextTurn =
    currentTurnBoundaryIndex >= 0 &&
    currentTurnBoundaryIndex < turnBoundaries.length - 1;

  return (
    <article className="panel inner match-replay-game">
      <div className="match-replay-head">
        <div className="match-replay-head-copy">
          <h4>Game {gameNumber}</h4>
          <p className="match-replay-caption">
            {replayMomentLabel(currentAction)} • Action{" "}
            {selectedActionIndex + 1} of {plays.length}
          </p>
        </div>
        <p className="match-replay-kicker">Observed replay</p>
      </div>

      <div className="match-replay-controls">
        <div className="match-replay-controls-bar">
          <div
            className="match-replay-button-row"
            role="group"
            aria-label={`Game ${gameNumber} replay controls`}
          >
            <button
              type="button"
              className="match-replay-button"
              onClick={togglePlay}
              aria-pressed={isPlaying}
            >
              {isPlaying ? "Pause" : "Play"}
            </button>
            <button
              type="button"
              className="match-replay-button"
              onClick={goToPrevActionTurn}
              disabled={!canJumpPrevTurn}
            >
              Previous Turn
            </button>
            <button
              type="button"
              className="match-replay-button"
              onClick={goToPrevAction}
              disabled={!canStepBackward}
            >
              Previous Action
            </button>
            <button
              type="button"
              className="match-replay-button"
              onClick={goToNextAction}
              disabled={!canStepForward}
            >
              Next Action
            </button>
            <button
              type="button"
              className="match-replay-button"
              onClick={goToNextActionTurn}
              disabled={!canJumpNextTurn}
            >
              Next Turn
            </button>
          </div>

          <div className="match-replay-controls-aux">
            <MatchReplaySpeedControl speed={speed} onSelectSpeed={setSpeed} />
            <p className="match-replay-kbd-hint">
              <kbd>←</kbd>
              <kbd>→</kbd> step · <kbd>⇧</kbd> turn · <kbd>space</kbd> play
            </p>
          </div>
        </div>

        <div className="match-replay-track-panel">
          <MatchReplayScrubber
            length={plays.length}
            index={selectedActionIndex}
            onSeek={(nextIndex) => {
              setIsPlaying(false);
              setSelectedActionIndex(nextIndex);
            }}
            turnBoundaries={turnBoundaries}
            itemLabel="action"
          />
        </div>
      </div>

      <div className="match-replay-canvas">
        <aside className="match-replay-sidebar">
          <MatchReplaySideSummary
            side="opponent"
            plays={opponentVisiblePlays}
            onOpenZone={setZoneDialogState}
          />

          <section
            className="match-replay-stackbox"
            aria-label={`Game ${gameNumber} current action`}
          >
            <div className="match-replay-stackbox-head">
              <p className="match-replay-sidebox-label">Current Action</p>
              <p className="match-replay-sidebox-total">
                #{selectedActionIndex + 1}
              </p>
            </div>
            <div className="match-replay-stackbox-body">
              <MatchReplayCard
                play={currentAction}
                preview={currentPreview}
                active
                size="stack"
              />
              <div className="match-replay-stackbox-copy">
                <p className="match-replay-stackbox-player">
                  {timelinePlayerLabel(currentAction.playerSide)}
                </p>
                <h5>{currentName}</h5>
                <p>{timelineZoneLabel(currentAction.firstPublicZone)}</p>
                <p>{boardPlayMeta(currentAction)}</p>
                <p>
                  {currentAction.playedAt
                    ? formatDateTime(currentAction.playedAt)
                    : "Unknown time"}
                </p>
              </div>
            </div>
          </section>

          <MatchReplaySideSummary
            side="self"
            plays={selfVisiblePlays}
            onOpenZone={setZoneDialogState}
          />
        </aside>

        <div className="match-replay-board is-observed-board">
          <MatchReplayBattlefield
            side="opponent"
            plays={opponentVisiblePlays}
            activePlayID={currentAction.id}
            previewByCardID={previewByCardID}
          />

          <section
            className="match-replay-centerline"
            aria-label="Replay status"
          >
            <p className="match-replay-centerline-title">
              {replayMomentLabel(currentAction)}
            </p>
            <p className="match-replay-centerline-copy">
              {timelinePlayerLabel(currentAction.playerSide)} first showed{" "}
              {currentName} in{" "}
              {timelineZoneLabel(currentAction.firstPublicZone)}.
            </p>
            {unknownVisiblePlays.length > 0 ? (
              <p className="match-replay-centerline-copy">
                {unknownVisiblePlays.length} observation
                {unknownVisiblePlays.length === 1 ? "" : "s"} still have an
                unknown owner.
              </p>
            ) : null}
          </section>

          <MatchReplayBattlefield
            side="self"
            plays={selfVisiblePlays}
            activePlayID={currentAction.id}
            previewByCardID={previewByCardID}
          />
        </div>
      </div>

      <MatchReplayZoneDialog
        state={zoneDialogState}
        previewByCardID={previewByCardID}
        onClose={() => setZoneDialogState(null)}
      />
    </article>
  );
}

export function MatchDetailPage() {
  const params = useParams();
  const matchId = Number(params.matchId);
  const isValidMatchID = Number.isFinite(matchId);
  const [timelineDisplayMode, setTimelineDisplayMode] =
    useState<TimelineDisplayMode>("board");
  const [selectedTimelineGameNumber, setSelectedTimelineGameNumber] =
    useState<number | null>(null);
  const timelineGameTabBaseId = useId();

  const query = useQuery({
    queryKey: ["match-detail", matchId],
    queryFn: () => api.matchDetail(matchId),
    enabled: isValidMatchID,
  });
  const timelineQuery = useQuery({
    queryKey: ["match-timeline", matchId],
    queryFn: () => api.matchTimeline(matchId),
    enabled: isValidMatchID,
  });
  const replayQuery = useQuery({
    queryKey: ["match-replay", matchId],
    queryFn: () => api.matchReplay(matchId),
    enabled: isValidMatchID && timelineDisplayMode === "board",
  });
  const { lookup: setLookup } = useEventSets([query.data?.match.eventName]);

  const opponentObservedCards = query.data?.opponentObservedCards ?? [];
  const opponentCards = useMemo<OpponentDeckCard[]>(() => {
    return opponentObservedCards.map((card) => ({
      cardId: card.cardId,
      cardName: card.cardName,
      quantity: card.quantity,
    }));
  }, [opponentObservedCards]);

  const opponentCardPreviewQueries = useQueries({
    queries: opponentCards.map((card) => ({
      queryKey: cardPreviewQueryKey(card),
      queryFn: () => fetchCardPreview(card.cardId, card.cardName),
      enabled: card.cardId > 0,
      staleTime: 1000 * 60 * 60 * 24,
      gcTime: 1000 * 60 * 60 * 24,
      retry: 1,
    })),
  });

  const opponentManaCostsByCardID = useMemo(() => {
    const out = new Map<number, string>();
    for (let i = 0; i < opponentCards.length; i += 1) {
      const card = opponentCards[i];
      const preview = opponentCardPreviewQueries[i]?.data;
      out.set(card.cardId, preview?.manaCost?.trim() ?? "");
    }
    return out;
  }, [opponentCards, opponentCardPreviewQueries]);

  const isOpponentCardMetadataLoading = opponentCardPreviewQueries.some(
    (previewQuery) => previewQuery.isPending,
  );
  const timelineRows = timelineQuery.data ?? query.data?.cardPlays ?? [];
  const replayFrames = replayQuery.data ?? [];
  const replayGroups = useMemo<ReplayGameGroup[]>(() => {
    const byGame = new Map<number, MatchReplayFrame[]>();
    for (const frame of replayFrames) {
      const gameNumber =
        frame.gameNumber && frame.gameNumber > 0 ? frame.gameNumber : 1;
      const rows = byGame.get(gameNumber);
      if (rows) {
        rows.push(frame);
      } else {
        byGame.set(gameNumber, [frame]);
      }
    }

    const groups = Array.from(byGame.entries())
      .map(([gameNumber, frames]) => ({
        gameNumber,
        frames: filterMeaningfulReplayFrames(frames),
      }))
      .filter((group) => group.frames.length > 0)
      .sort((a, b) => a.gameNumber - b.gameNumber);
    const finalGameNumber = groups[groups.length - 1]?.gameNumber ?? null;

    return groups.map((group) => ({
      ...group,
      summary: summarizeReplayGame(group.frames, {
        isFinalGame: group.gameNumber === finalGameNumber,
        matchResult: query.data?.match.result ?? "unknown",
      }),
    }));
  }, [query.data?.match.result, replayFrames]);
  const visibleReplayFrames = useMemo(
    () => replayGroups.flatMap((group) => group.frames),
    [replayGroups],
  );
  const hasReplayFrames = visibleReplayFrames.length > 0;
  const boardPreviewCards = useMemo<PreviewCard[]>(() => {
    const uniqueCards = new Map<number, PreviewCard>();

    if (hasReplayFrames) {
      for (const frame of visibleReplayFrames) {
        for (const object of frame.objects ?? []) {
          if (!uniqueCards.has(object.cardId)) {
            uniqueCards.set(object.cardId, {
              cardId: object.cardId,
              cardName: object.cardName,
            });
          }
        }
        for (const change of frame.changes ?? []) {
          if (!uniqueCards.has(change.cardId)) {
            uniqueCards.set(change.cardId, {
              cardId: change.cardId,
              cardName: change.cardName,
            });
          }
        }
      }
    } else {
      for (const play of timelineRows) {
        if (!uniqueCards.has(play.cardId)) {
          uniqueCards.set(play.cardId, {
            cardId: play.cardId,
            cardName: play.cardName,
          });
        }
      }
    }

    return Array.from(uniqueCards.values());
  }, [hasReplayFrames, timelineRows, visibleReplayFrames]);
  const boardCardPreviewQueries = useQueries({
    queries: boardPreviewCards.map((card) => ({
      queryKey: cardPreviewQueryKey(card),
      queryFn: () => fetchCardPreview(card.cardId, card.cardName),
      enabled: timelineDisplayMode === "board" && card.cardId > 0,
      staleTime: 1000 * 60 * 60 * 24,
      gcTime: 1000 * 60 * 60 * 24,
      retry: 1,
    })),
  });
  const boardPreviewByCardID = useMemo(() => {
    const out = new Map<number, CardPreview | null>();
    for (let index = 0; index < boardPreviewCards.length; index += 1) {
      const card = boardPreviewCards[index];
      out.set(card.cardId, boardCardPreviewQueries[index]?.data ?? null);
    }
    return out;
  }, [boardPreviewCards, boardCardPreviewQueries]);
  const isBoardCardPreviewLoading =
    timelineDisplayMode === "board" &&
    boardCardPreviewQueries.some((queryRow) => queryRow.isPending);
  const timelineGroups = useMemo(() => {
    const byGame = new Map<number, MatchCardPlay[]>();
    for (const play of timelineRows) {
      const gameNumber =
        play.gameNumber && play.gameNumber > 0 ? play.gameNumber : 1;
      const rows = byGame.get(gameNumber);
      if (rows) {
        rows.push(play);
      } else {
        byGame.set(gameNumber, [play]);
      }
    }

    return Array.from(byGame.entries()).sort((a, b) => a[0] - b[0]);
  }, [timelineRows]);
  const timelineSummary = hasReplayFrames
    ? `${visibleReplayFrames.length} public replay step${visibleReplayFrames.length === 1 ? "" : "s"} across ${replayGroups.length} game${replayGroups.length === 1 ? "" : "s"}`
    : `${timelineRows.length} observed play${timelineRows.length === 1 ? "" : "s"}${timelineRows.length > 0 ? ` across ${timelineGroups.length} game${timelineGroups.length === 1 ? "" : "s"}` : ""}`;
  const timelineGameNumbers = useMemo(() => {
    if (timelineDisplayMode === "board" && hasReplayFrames) {
      return replayGroups.map((group) => group.gameNumber);
    }
    return timelineGroups.map(([gameNumber]) => gameNumber);
  }, [hasReplayFrames, replayGroups, timelineDisplayMode, timelineGroups]);
  const activeTimelineGameNumber =
    selectedTimelineGameNumber ?? timelineGameNumbers[0] ?? null;
  const activeReplayGroup =
    activeTimelineGameNumber === null
      ? null
      : replayGroups.find(
          (group) => group.gameNumber === activeTimelineGameNumber,
        ) ?? null;
  const activeTimelineGroup =
    activeTimelineGameNumber === null
      ? null
      : timelineGroups.find(
          ([gameNumber]) => gameNumber === activeTimelineGameNumber,
        ) ?? null;
  const activeTimelinePlays = activeTimelineGroup?.[1] ?? null;
  const showTimelineGameTabs = timelineGameNumbers.length > 1;
  const activeTimelineGameTabID =
    activeTimelineGameNumber === null
      ? undefined
      : `${timelineGameTabBaseId}-tab-${activeTimelineGameNumber}`;
  const activeTimelineGamePanelID =
    activeTimelineGameNumber === null
      ? undefined
      : `${timelineGameTabBaseId}-panel-${activeTimelineGameNumber}`;

  useEffect(() => {
    if (timelineGameNumbers.length === 0) {
      setSelectedTimelineGameNumber(null);
      return;
    }

    setSelectedTimelineGameNumber((currentGameNumber) =>
      currentGameNumber !== null &&
      timelineGameNumbers.includes(currentGameNumber)
        ? currentGameNumber
        : timelineGameNumbers[0],
    );
  }, [timelineGameNumbers]);

  function handleTimelineGameTabKeyDown(
    event: KeyboardEvent<HTMLButtonElement>,
    gameNumber: number,
  ) {
    const currentIndex = timelineGameNumbers.indexOf(gameNumber);
    if (currentIndex === -1) return;

    switch (event.key) {
      case "ArrowLeft":
      case "ArrowUp":
        event.preventDefault();
        setSelectedTimelineGameNumber(
          timelineGameNumbers[
            (currentIndex + timelineGameNumbers.length - 1) %
              timelineGameNumbers.length
          ],
        );
        break;
      case "ArrowRight":
      case "ArrowDown":
        event.preventDefault();
        setSelectedTimelineGameNumber(
          timelineGameNumbers[
            (currentIndex + 1) % timelineGameNumbers.length
          ],
        );
        break;
      case "Home":
        event.preventDefault();
        setSelectedTimelineGameNumber(timelineGameNumbers[0]);
        break;
      case "End":
        event.preventDefault();
        setSelectedTimelineGameNumber(
          timelineGameNumbers[timelineGameNumbers.length - 1],
        );
        break;
      default:
        break;
    }
  }

  if (!isValidMatchID)
    return <StatusMessage tone="error">Invalid match id.</StatusMessage>;
  if (query.isLoading) return <StatusMessage>Loading match…</StatusMessage>;
  if (query.error)
    return (
      <StatusMessage tone="error">
        {(query.error as Error).message}
      </StatusMessage>
    );
  if (!query.data) return <StatusMessage>Match not found.</StatusMessage>;

  const { match } = query.data;

  return (
    <div className="stack-lg">
      <section className="panel match-detail-overview-panel">
        <div className="panel-head">
          <h3>Match #{match.id}</h3>
          <Link className="text-link" to="/matches">
            Back to matches
          </Link>
        </div>
        <dl className="match-detail-summary" aria-label="Match overview">
          <div className="match-detail-summary-item">
            <dt>Event</dt>
            <dd>
              <EventLabel eventName={match.eventName} lookup={setLookup} />
            </dd>
          </div>
          <div className="match-detail-summary-item">
            <dt>Deck</dt>
            <dd>
              {match.deckId ? (
                <Link className="text-link" to={`/decks/${match.deckId}`}>
                  {match.deckName || `Deck ${match.deckId}`}
                </Link>
              ) : (
                "-"
              )}
            </dd>
          </div>
          <div className="match-detail-summary-item">
            <dt>Opponent</dt>
            <dd>{match.opponent || "Unknown"}</dd>
          </div>
          <div className="match-detail-summary-item">
            <dt>Deck Colors</dt>
            <dd>
              <MatchDeckColors
                className="match-deck-colors-detail"
                deckColors={match.deckColors}
                deckColorsKnown={match.deckColorsKnown}
                opponentDeckColors={match.opponentDeckColors}
                opponentDeckColorsKnown={match.opponentDeckColorsKnown}
              />
            </dd>
          </div>
          <div className="match-detail-summary-item match-detail-summary-item-mono">
            <dt>Started</dt>
            <dd>{formatDateTime(match.startedAt)}</dd>
          </div>
          <div className="match-detail-summary-item">
            <dt>Result</dt>
            <dd>
              <ResultPill result={match.result} />
            </dd>
          </div>
          <div className="match-detail-summary-item">
            <dt>Reason</dt>
            <dd>{match.winReason || "-"}</dd>
          </div>
          <div className="match-detail-summary-item match-detail-summary-item-mono">
            <dt>Turns</dt>
            <dd>{match.turnCount ?? "-"}</dd>
          </div>
          <div className="match-detail-summary-item match-detail-summary-item-mono">
            <dt>Duration</dt>
            <dd>{formatDuration(match.secondsCount ?? undefined)}</dd>
          </div>
        </dl>
      </section>

      <section className="panel">
        <div className="panel-head match-timeline-toolbar">
          <div className="match-timeline-heading">
            <h3>Card Play Timeline</h3>
            <p>{timelineSummary}</p>
          </div>
          <div
            className="tabs"
            role="group"
            aria-label="Card play timeline display"
          >
            <button
              type="button"
              className={`tab match-timeline-button ${timelineDisplayMode === "board" ? "is-active" : ""}`}
              aria-pressed={timelineDisplayMode === "board"}
              onClick={() => setTimelineDisplayMode("board")}
            >
              Board
            </button>
            <button
              type="button"
              className={`tab match-timeline-button ${timelineDisplayMode === "list" ? "is-active" : ""}`}
              aria-pressed={timelineDisplayMode === "list"}
              onClick={() => setTimelineDisplayMode("list")}
            >
              List
            </button>
          </div>
        </div>
        {showTimelineGameTabs ? (
          <div
            className="tabs match-timeline-game-tabs"
            role="tablist"
            aria-label="Timeline game selector"
          >
            {timelineGameNumbers.map((gameNumber) => (
              <button
                key={gameNumber}
                type="button"
                id={`${timelineGameTabBaseId}-tab-${gameNumber}`}
                role="tab"
                aria-selected={activeTimelineGameNumber === gameNumber}
                aria-controls={`${timelineGameTabBaseId}-panel-${gameNumber}`}
                tabIndex={activeTimelineGameNumber === gameNumber ? 0 : -1}
                className={`tab match-timeline-button ${activeTimelineGameNumber === gameNumber ? "is-active" : ""}`}
                onClick={() => setSelectedTimelineGameNumber(gameNumber)}
                onKeyDown={(event) =>
                  handleTimelineGameTabKeyDown(event, gameNumber)
                }
              >
                Game {gameNumber}
              </button>
            ))}
          </div>
        ) : null}
        {timelineDisplayMode === "board" ? (
          <div className="stack-md">
            {!hasReplayFrames ? (
              <p className="match-board-disclaimer">
                Replay frames are not available for this match yet, so this fallback board still uses first public sightings and cannot show a true stack.
              </p>
            ) : null}
            {replayQuery.isPending ? (
              <StatusMessage>Loading replay frames…</StatusMessage>
            ) : hasReplayFrames ? (
              activeReplayGroup ? (
                <div
                  id={showTimelineGameTabs ? activeTimelineGamePanelID : undefined}
                  role={showTimelineGameTabs ? "tabpanel" : undefined}
                  aria-labelledby={
                    showTimelineGameTabs ? activeTimelineGameTabID : undefined
                  }
                >
                  <MatchReplayFrameBoard
                    key={activeReplayGroup.gameNumber}
                    gameNumber={activeReplayGroup.gameNumber}
                    frames={activeReplayGroup.frames}
                    gameSummary={activeReplayGroup.summary}
                    previewByCardID={boardPreviewByCardID}
                  />
                </div>
              ) : (
                <StatusMessage>
                  No observed replay data for this match yet.
                </StatusMessage>
              )
            ) : timelineQuery.error ? (
              <StatusMessage tone="error">
                {(timelineQuery.error as Error).message}
              </StatusMessage>
            ) : timelineRows.length === 0 ? (
              <StatusMessage>
                No observed replay data for this match yet.
              </StatusMessage>
            ) : (
              activeTimelineGroup ? (
                <div
                  id={showTimelineGameTabs ? activeTimelineGamePanelID : undefined}
                  role={showTimelineGameTabs ? "tabpanel" : undefined}
                  aria-labelledby={
                    showTimelineGameTabs ? activeTimelineGameTabID : undefined
                  }
                >
                  <MatchTimelineBoard
                    gameNumber={activeTimelineGroup[0]}
                    plays={activeTimelineGroup[1]}
                    previewByCardID={boardPreviewByCardID}
                  />
                </div>
              ) : (
                <StatusMessage>
                  No observed replay data for this match yet.
                </StatusMessage>
              )
            )}
            {replayQuery.error && !hasReplayFrames ? (
              <StatusMessage tone="error">
                {(replayQuery.error as Error).message}
              </StatusMessage>
            ) : null}
            {isBoardCardPreviewLoading ? (
              <StatusMessage>Loading replay card art…</StatusMessage>
            ) : null}
          </div>
        ) : timelineQuery.error ? (
          <StatusMessage tone="error">
            {(timelineQuery.error as Error).message}
          </StatusMessage>
        ) : timelineRows.length === 0 ? (
          <StatusMessage>
            No observed card plays for this match yet.
          </StatusMessage>
        ) : (
          <div
            className="stack-md"
            id={showTimelineGameTabs ? activeTimelineGamePanelID : undefined}
            role={showTimelineGameTabs ? "tabpanel" : undefined}
            aria-labelledby={
              showTimelineGameTabs ? activeTimelineGameTabID : undefined
            }
          >
            {activeTimelineGameNumber !== null && activeTimelinePlays ? (
              <div className="stack-md">
                <h4>Game {activeTimelineGameNumber}</h4>
                <div className="table-wrap">
                  <table className="data-table">
                    <thead>
                      <tr>
                        <th>#</th>
                        <th>Turn</th>
                        <th>Player</th>
                        <th>Card</th>
                        <th>Zone</th>
                        <th>Phase</th>
                        <th>Seen At</th>
                      </tr>
                    </thead>
                    <tbody>
                      {activeTimelinePlays.map((play, index) => (
                        <tr key={play.id}>
                          <td>{index + 1}</td>
                          <td>{play.turnNumber ?? "-"}</td>
                          <td>{timelinePlayerLabel(play.playerSide)}</td>
                          <td>
                            <CardPreviewName
                              card={{
                                cardId: play.cardId,
                                cardName: play.cardName,
                              }}
                            />
                          </td>
                          <td>{timelineZoneLabel(play.firstPublicZone)}</td>
                          <td>{timelinePhaseLabel(play.phase)}</td>
                          <td>{formatDateTime(play.playedAt ?? "")}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </div>
            ) : (
              <StatusMessage>
                No observed card plays for this match yet.
              </StatusMessage>
            )}
          </div>
        )}
      </section>

      <section className="panel">
        <div className="panel-head">
          <h3>Observed Opponent Cards</h3>
          <p>{opponentObservedCards.length} unique cards</p>
        </div>
        {opponentObservedCards.length === 0 ? (
          <StatusMessage>
            No public opponent cards observed for this match yet.
          </StatusMessage>
        ) : (
          <div className="table-wrap">
            <table className="data-table">
              <thead>
                <tr>
                  <th>Qty</th>
                  <th>Card</th>
                  <th>Mana</th>
                </tr>
              </thead>
              <tbody>
                {opponentCards.map((card) => (
                  <tr key={card.cardId}>
                    <td>{card.quantity}</td>
                    <td>
                      <CardPreviewName card={card} />
                    </td>
                    <td>
                      <span className="deck-card-mana">
                        <ManaCostDisplay
                          manaCost={
                            opponentManaCostsByCardID.get(card.cardId) ?? ""
                          }
                        />
                      </span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {isOpponentCardMetadataLoading ? (
          <StatusMessage>Loading card previews and mana details…</StatusMessage>
        ) : null}
      </section>
    </div>
  );
}

