import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { useQueries, useQuery } from "@tanstack/react-query";

import { DeckAnalyticsPanel } from "../components/DeckAnalyticsPanel";
import { DeckColorIdentity } from "../components/MatchDeckColors";
import { DeckPrimerPanel } from "../components/DeckPrimerPanel";
import { EventLabel } from "../components/EventLabel";
import { DeckMatchupsPanel, LimitedMatchupsPanel } from "../components/MatchupPanels";
import { ManaSymbol } from "../components/ManaSymbol";
import { RarityDot, RARITY_LABELS, RARITY_ORDER } from "../components/RarityDot";
import { ResultPill } from "../components/ResultPill";
import { StatusMessage } from "../components/StatusMessage";
import { api } from "../lib/api";
import { parseEventName } from "../lib/events";
import { formatDateTime, formatDuration } from "../lib/format";
import { fetchCardPreview, type CardPreview, type CardRarity } from "../lib/scryfall";
import { useEventSets } from "../lib/useEventSets";

type DeckListCard = {
  section: string;
  cardId: number;
  cardName?: string;
  quantity: number;
};

type PopoverPlacement = "left" | "right";
type PopoverPlacementMode = "auto" | "force-right";
type FloatingPopoverPosition = {
  top: number;
  left: number;
  width: number;
  height: number;
};
type ManaCostPart = { kind: "symbol"; token: string } | { kind: "separator"; value: string };
type DeckDisplayMode = "list" | "curve" | "visual";
type CurveBucketKey = number | "other" | "lands";
type VisualCategory =
  | "creatures"
  | "planeswalkers"
  | "instants"
  | "sorceries"
  | "battles"
  | "artifacts"
  | "enchantments"
  | "lands"
  | "other";

type MainboardCategory = "creatures" | "spells" | "artifacts" | "enchantments" | "lands";

type MainboardDeckListCard = DeckListCard & {
  manaCost: string;
  manaValue: number | null;
  imageUrl?: string;
  artCropUrl?: string;
  scryfallUrl?: string;
  typeLine?: string;
  rarity?: CardRarity;
};

type SideboardDeckListCard = DeckListCard & {
  manaCost: string;
  manaValue: number | null;
  imageUrl?: string;
  artCropUrl?: string;
  scryfallUrl?: string;
  typeLine?: string;
  rarity?: CardRarity;
};

type DeckCurveColumn = {
  key: string;
  label: string;
  accessibleLabel: string;
  totalCards: number;
  cards: MainboardDeckListCard[];
};

type DeckCurveCardEntry = {
  key: string;
  card: MainboardDeckListCard;
  quantityBadge?: number;
};

type DeckVisualGroup = {
  key: VisualCategory;
  label: string;
  cards: MainboardDeckListCard[];
};

const MAINBOARD_CATEGORY_ORDER: MainboardCategory[] = ["creatures", "spells", "artifacts", "enchantments", "lands"];
const MAINBOARD_SKELETON_CATEGORY_ORDER: MainboardCategory[] = ["creatures", "spells", "lands"];
const VISUAL_CATEGORY_LABELS: Record<VisualCategory, string> = {
  creatures: "Creatures",
  planeswalkers: "Planeswalkers",
  instants: "Instants",
  sorceries: "Sorceries",
  battles: "Battles",
  artifacts: "Artifacts",
  enchantments: "Enchantments",
  lands: "Lands",
  other: "Other",
};
const VISUAL_CATEGORY_ORDER = Object.keys(VISUAL_CATEGORY_LABELS) as VisualCategory[];
const DEFAULT_APP_SHELL_WIDTH = 1280;
const MAX_CURVE_APP_SHELL_WIDTH = 1520;
const CURVE_COLUMN_TARGET_WIDTH = 176;
const CURVE_COLUMN_TARGET_GAP = 14;
const CURVE_PANEL_CHROME_WIDTH = 120;
const BASIC_LAND_ORDER: Record<string, number> = {
  island: 0,
  swamp: 1,
  forest: 2,
  mountain: 3,
  plains: 4,
};

function cardDisplayName(card: DeckListCard): string {
  return card.cardName?.trim() || `Card ${card.cardId}`;
}

function cardPreviewQueryKey(card: DeckListCard): [string, number, string] {
  return ["card-preview", card.cardId, cardDisplayName(card)];
}

function cardScryfallHref(card: DeckListCard, scryfallUrl?: string): string {
  if (scryfallUrl) {
    return scryfallUrl;
  }
  const name = cardDisplayName(card);
  return card.cardName?.trim()
    ? `https://scryfall.com/search?q=${encodeURIComponent(`!"${name}"`)}`
    : `https://scryfall.com/search?q=${encodeURIComponent(`arenaid:${card.cardId}`)}`;
}

function floatingPopoverPosition(
  anchor: HTMLElement,
  popoverWidth: number,
  popoverHeight: number,
  horizontalGap = 16,
  viewportPadding = 8,
): FloatingPopoverPosition {
  const rect = anchor.getBoundingClientRect();
  const viewportWidth = window.innerWidth || document.documentElement.clientWidth;
  const viewportHeight = window.innerHeight || document.documentElement.clientHeight;
  const availableRight = viewportWidth - rect.right;
  const availableLeft = rect.left;
  const placement: PopoverPlacement =
    availableRight < popoverWidth + horizontalGap && availableLeft >= popoverWidth + horizontalGap ? "left" : "right";

  const rawLeft = placement === "right" ? rect.right + horizontalGap : rect.left - popoverWidth - horizontalGap;
  const maxLeft = Math.max(viewportPadding, viewportWidth - popoverWidth - viewportPadding);
  const left = Math.max(viewportPadding, Math.min(rawLeft, maxLeft));
  const rawTop = rect.top + rect.height / 2 - popoverHeight / 2;
  const maxTop = Math.max(viewportPadding, viewportHeight - popoverHeight - viewportPadding);
  const top = Math.max(viewportPadding, Math.min(rawTop, maxTop));

  return { top, left, width: popoverWidth, height: popoverHeight };
}

function classifyMainboardCard(typeLine?: string): MainboardCategory {
  const lower = typeLine?.toLowerCase() ?? "";
  if (lower.includes("land")) {
    return "lands";
  }
  if (lower.includes("creature")) {
    return "creatures";
  }
  if (lower.includes("artifact")) {
    return "artifacts";
  }
  if (lower.includes("enchantment")) {
    return "enchantments";
  }
  return "spells";
}

function classifyVisualCard(typeLine?: string): VisualCategory {
  const lower = typeLine?.toLowerCase() ?? "";
  if (lower.includes("land")) {
    return "lands";
  }
  if (lower.includes("creature")) {
    return "creatures";
  }
  if (lower.includes("planeswalker")) {
    return "planeswalkers";
  }
  if (lower.includes("instant")) {
    return "instants";
  }
  if (lower.includes("sorcery")) {
    return "sorceries";
  }
  if (lower.includes("battle")) {
    return "battles";
  }
  if (lower.includes("artifact")) {
    return "artifacts";
  }
  if (lower.includes("enchantment")) {
    return "enchantments";
  }
  return "other";
}

function compareMainboardCards(a: MainboardDeckListCard, b: MainboardDeckListCard): number {
  const manaA = a.manaValue ?? Number.POSITIVE_INFINITY;
  const manaB = b.manaValue ?? Number.POSITIVE_INFINITY;
  if (manaA !== manaB) {
    return manaA - manaB;
  }
  const byName = cardDisplayName(a).localeCompare(cardDisplayName(b), undefined, { sensitivity: "base" });
  if (byName !== 0) {
    return byName;
  }
  return a.cardId - b.cardId;
}

function basicLandRank(card: DeckListCard): number {
  const normalized = cardDisplayName(card).trim().toLowerCase();
  const rank = BASIC_LAND_ORDER[normalized];
  if (typeof rank === "number") {
    return rank;
  }
  return Number.POSITIVE_INFINITY;
}

function compareLandCards(a: MainboardDeckListCard, b: MainboardDeckListCard): number {
  const basicRankA = basicLandRank(a);
  const basicRankB = basicLandRank(b);
  if (basicRankA !== basicRankB) {
    return basicRankA - basicRankB;
  }

  const byName = cardDisplayName(a).localeCompare(cardDisplayName(b), undefined, { sensitivity: "base" });
  if (byName !== 0) {
    return byName;
  }
  return a.cardId - b.cardId;
}

function buildVisualGroups(cards: MainboardDeckListCard[]): DeckVisualGroup[] {
  const groups = new Map<VisualCategory, MainboardDeckListCard[]>();
  for (const card of cards) {
    const category = classifyVisualCard(card.typeLine);
    const categoryCards = groups.get(category) ?? [];
    categoryCards.push(card);
    groups.set(category, categoryCards);
  }

  return VISUAL_CATEGORY_ORDER.flatMap((category) => {
    const categoryCards = groups.get(category);
    if (!categoryCards?.length) {
      return [];
    }
    return [{
      key: category,
      label: VISUAL_CATEGORY_LABELS[category],
      cards: [...categoryCards].sort(category === "lands" ? compareLandCards : compareMainboardCards),
    }];
  });
}

function formatSectionLabel(section: string): string {
  const trimmed = section.trim();
  if (!trimmed) {
    return "Other";
  }
  return `${trimmed.charAt(0).toUpperCase()}${trimmed.slice(1)}`;
}

function parseDeckDisplayMode(value: string | null): DeckDisplayMode {
  return value === "curve" || value === "visual" ? value : "list";
}

function sectionTotal(cards: DeckListCard[]): number {
  return cards.reduce((sum, card) => sum + card.quantity, 0);
}

function countRarities(cards: Array<{ quantity: number; rarity?: CardRarity }>): Record<CardRarity, number> {
  const counts: Record<CardRarity, number> = { common: 0, uncommon: 0, rare: 0, mythic: 0 };
  for (const card of cards) {
    if (card.rarity) {
      counts[card.rarity] += card.quantity;
    }
  }
  return counts;
}

function RaritySummaryGroup({
  label,
  cards,
}: {
  label: string;
  cards: Array<{ quantity: number; rarity?: CardRarity }>;
}) {
  const counts = countRarities(cards);
  const entries = RARITY_ORDER.filter((rarity) => counts[rarity] > 0);
  if (entries.length === 0) {
    return null;
  }

  return (
    <span className="deck-rarity-group">
      <span className="deck-rarity-group-label">{label}</span>
      {entries.map((rarity) => (
        <span
          className="deck-rarity-item"
          key={rarity}
          title={`${counts[rarity]} ${RARITY_LABELS[rarity].toLowerCase()}`}
          aria-label={`${counts[rarity]} ${RARITY_LABELS[rarity].toLowerCase()}`}
        >
          <RarityDot rarity={rarity} />
          {counts[rarity]}
        </span>
      ))}
    </span>
  );
}

function formatManaValueLabel(manaValue: number): string {
  return new Intl.NumberFormat(undefined, {
    maximumFractionDigits: 1,
  }).format(manaValue);
}

function curveBucketKey(card: MainboardDeckListCard | SideboardDeckListCard): CurveBucketKey {
  if (classifyMainboardCard(card.typeLine) === "lands") {
    return "lands";
  }
  if (typeof card.manaValue === "number" && Number.isFinite(card.manaValue)) {
    return card.manaValue;
  }
  return "other";
}

function compareCurveBucketKeys(a: CurveBucketKey, b: CurveBucketKey): number {
  if (typeof a === "number" && typeof b === "number") {
    return a - b;
  }
  if (typeof a === "number") {
    return -1;
  }
  if (typeof b === "number") {
    return 1;
  }
  if (a === b) {
    return 0;
  }
  if (a === "other") {
    return -1;
  }
  if (b === "other") {
    return 1;
  }
  return 0;
}

function curveBucketLabel(bucket: CurveBucketKey): string {
  if (bucket === "lands") {
    return "Lands";
  }
  if (bucket === "other") {
    return "Other";
  }
  return `MV ${formatManaValueLabel(bucket)}`;
}

function curveBucketAccessibleLabel(bucket: CurveBucketKey): string {
  if (bucket === "lands") {
    return "Lands";
  }
  if (bucket === "other") {
    return "Other mana values";
  }
  return `Mana value ${formatManaValueLabel(bucket)}`;
}

function buildCurveColumns(cards: MainboardDeckListCard[]): DeckCurveColumn[] {
  const buckets = new Map<CurveBucketKey, MainboardDeckListCard[]>();

  for (const card of cards) {
    const bucket = curveBucketKey(card);
    const entries = buckets.get(bucket) ?? [];
    entries.push(card);
    buckets.set(bucket, entries);
  }

  return Array.from(buckets.entries())
    .sort(([left], [right]) => compareCurveBucketKeys(left, right))
    .map(([bucket, bucketCards]) => {
      const sortedCards = [...bucketCards].sort(bucket === "lands" ? compareLandCards : compareMainboardCards);
      return {
        key: typeof bucket === "number" ? `mv-${bucket}` : bucket,
        label: curveBucketLabel(bucket),
        accessibleLabel: curveBucketAccessibleLabel(bucket),
        totalCards: sectionTotal(sortedCards),
        cards: sortedCards,
      };
    });
}

function estimateCurveShellWidth(columnCount: number): number {
  if (!Number.isFinite(columnCount) || columnCount <= 0) {
    return DEFAULT_APP_SHELL_WIDTH;
  }

  const desiredWidth =
    columnCount * CURVE_COLUMN_TARGET_WIDTH +
    Math.max(0, columnCount - 1) * CURVE_COLUMN_TARGET_GAP +
    CURVE_PANEL_CHROME_WIDTH;

  return Math.min(MAX_CURVE_APP_SHELL_WIDTH, Math.max(DEFAULT_APP_SHELL_WIDTH, Math.ceil(desiredWidth)));
}

function buildCurveCardEntries(columnKey: string, cards: MainboardDeckListCard[]): DeckCurveCardEntry[] {
  const out: DeckCurveCardEntry[] = [];

  for (const card of cards) {
    if (card.quantity > 4) {
      out.push({
        key: `${columnKey}-${card.cardId}-stack`,
        card,
        quantityBadge: card.quantity,
      });
      continue;
    }

    for (let copyIndex = 0; copyIndex < card.quantity; copyIndex += 1) {
      out.push({
        key: `${columnKey}-${card.cardId}-${copyIndex}`,
        card,
      });
    }
  }

  return out;
}

function parseManaCostParts(manaCost: string): ManaCostPart[] {
  const trimmed = manaCost.trim();
  if (!trimmed) {
    return [];
  }

  const parts: ManaCostPart[] = [];
  const tokenPattern = /\{([^}]+)\}/g;
  let lastIndex = 0;

  while (true) {
    const match = tokenPattern.exec(trimmed);
    if (!match) {
      break;
    }

    const between = trimmed.slice(lastIndex, match.index).trim();
    if (between) {
      parts.push({ kind: "separator", value: between });
    }

    const token = match[1]?.trim();
    if (token) {
      parts.push({ kind: "symbol", token });
    }

    lastIndex = tokenPattern.lastIndex;
  }

  const tail = trimmed.slice(lastIndex).trim();
  if (tail) {
    parts.push({ kind: "separator", value: tail });
  }

  return parts;
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
    <span className="deck-card-mana-cost deck-card-mana-icons" aria-label={`Mana cost ${trimmed}`}>
      {parts.map((part, index) =>
        part.kind === "symbol" ? (
          <ManaSymbol key={`symbol-${part.token}-${index}`} token={part.token} />
        ) : (
          <span className="mana-symbol-separator" key={`sep-${part.value}-${index}`}>
            {part.value}
          </span>
        ),
      )}
    </span>
  );
}

function DeckCardPreviewName({ card, placementMode = "auto" }: { card: DeckListCard; placementMode?: PopoverPlacementMode }) {
  const [isOpen, setIsOpen] = useState(false);
  const [popoverPlacement, setPopoverPlacement] = useState<PopoverPlacement>("right");
  const wrapperRef = useRef<HTMLDivElement | null>(null);
  const name = cardDisplayName(card);
  const fallbackHref = cardScryfallHref(card);

  const updatePopoverPlacement = () => {
    if (placementMode === "force-right") {
      setPopoverPlacement("right");
      return;
    }

    if (typeof window === "undefined") {
      return;
    }

    const wrapper = wrapperRef.current;
    if (!wrapper) {
      return;
    }

    const rect = wrapper.getBoundingClientRect();
    const viewportWidth = window.innerWidth || document.documentElement.clientWidth;
    const popoverWidth = 336;
    const horizontalGap = 14;
    const availableRight = viewportWidth - rect.right;
    const availableLeft = rect.left;

    if (availableRight >= popoverWidth + horizontalGap) {
      setPopoverPlacement("right");
      return;
    }
    if (availableLeft >= popoverWidth + horizontalGap) {
      setPopoverPlacement("left");
      return;
    }
    setPopoverPlacement(availableRight >= availableLeft ? "right" : "left");
  };

  const openPopover = () => {
    updatePopoverPlacement();
    setIsOpen(true);
  };

  const previewQuery = useQuery({
    queryKey: cardPreviewQueryKey(card),
    queryFn: () => fetchCardPreview(card.cardId, card.cardName),
    enabled: isOpen,
    staleTime: 1000 * 60 * 60 * 24,
    gcTime: 1000 * 60 * 60 * 24,
    retry: 1,
  });

  useEffect(() => {
    if (!isOpen) {
      return;
    }
    const onResize = () => updatePopoverPlacement();
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, [isOpen, placementMode]);

  return (
    <div
      className="card-preview-anchor"
      data-popover-placement={popoverPlacement}
      ref={wrapperRef}
      onMouseEnter={openPopover}
      onMouseLeave={() => setIsOpen(false)}
    >
      <a
        className="card-preview-trigger"
        href={previewQuery.data?.scryfallUrl ?? fallbackHref}
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
        <code>{name}</code>
      </a>

      {isOpen ? (
        <div className="card-preview-popover" role="tooltip">
          {previewQuery.isLoading ? (
            <p className="card-preview-status">Loading preview…</p>
          ) : previewQuery.data ? (
            <img src={previewQuery.data.imageUrl} alt={previewQuery.data.name} loading="lazy" />
          ) : (
            <p className="card-preview-status">Preview unavailable.</p>
          )}
        </div>
      ) : null}
    </div>
  );
}

function DeckImageCardLink({
  card,
  className,
  eager = false,
  quantityBadge,
  quantityBadgeText,
  displayImageUrl,
  onPreviewStart,
  onPreviewEnd,
}: {
  card: MainboardDeckListCard;
  className: string;
  eager?: boolean;
  quantityBadge?: number;
  quantityBadgeText?: string;
  onPreviewStart: (card: MainboardDeckListCard, anchor: HTMLAnchorElement) => void;
  onPreviewEnd: () => void;
  displayImageUrl?: string;
}) {
  const name = cardDisplayName(card);
  const ariaLabel =
    quantityBadge !== undefined
      ? `Open ${name} on Scryfall, quantity ${quantityBadge}`
      : `Open ${name} on Scryfall`;
  const imageURL = displayImageUrl ?? card.imageUrl;

  return (
    <a
      className={className}
      href={cardScryfallHref(card, card.scryfallUrl)}
      target="_blank"
      rel="noreferrer"
      aria-label={ariaLabel}
      title={name}
      onMouseEnter={(event) => onPreviewStart(card, event.currentTarget)}
      onMouseLeave={onPreviewEnd}
      onFocus={(event) => onPreviewStart(card, event.currentTarget)}
    >
      {imageURL ? (
        <img
          src={imageURL}
          alt=""
          loading={eager ? "eager" : "lazy"}
          decoding="async"
          width={630}
          height={880}
        />
      ) : (
        <span className="deck-curve-card-fallback">
          <strong>{name}</strong>
          <span>{card.manaCost ? `Mana cost ${card.manaCost}` : "Preview unavailable"}</span>
        </span>
      )}
      {quantityBadge !== undefined ? (
        <span className="deck-curve-card-qty">{quantityBadgeText ?? `x${quantityBadge}`}</span>
      ) : null}
    </a>
  );
}

function DeckImagePreviewRegion({
  className,
  ariaLabel,
  children,
}: {
  className: string;
  ariaLabel: string;
  children: (handlers: {
    showPreview: (card: MainboardDeckListCard, anchor: HTMLAnchorElement) => void;
    schedulePreviewClose: () => void;
  }) => ReactNode;
}) {
  const [activePreviewCard, setActivePreviewCard] = useState<MainboardDeckListCard | null>(null);
  const [activePreviewPosition, setActivePreviewPosition] = useState<FloatingPopoverPosition | null>(null);
  const activePreviewAnchorRef = useRef<HTMLAnchorElement | null>(null);
  const closePreviewTimeoutRef = useRef<number | null>(null);

  const cancelPreviewClose = () => {
    if (closePreviewTimeoutRef.current === null || typeof window === "undefined") {
      return;
    }
    window.clearTimeout(closePreviewTimeoutRef.current);
    closePreviewTimeoutRef.current = null;
  };

  const clearPreview = () => {
    cancelPreviewClose();
    activePreviewAnchorRef.current = null;
    setActivePreviewCard(null);
    setActivePreviewPosition(null);
  };

  const schedulePreviewClose = () => {
    cancelPreviewClose();
    if (typeof window === "undefined") {
      clearPreview();
      return;
    }
    closePreviewTimeoutRef.current = window.setTimeout(() => {
      activePreviewAnchorRef.current = null;
      setActivePreviewCard(null);
      setActivePreviewPosition(null);
      closePreviewTimeoutRef.current = null;
    }, 120);
  };

  const showPreview = (card: MainboardDeckListCard, anchor: HTMLAnchorElement) => {
    cancelPreviewClose();
    if (typeof window === "undefined" || !card.imageUrl) {
      clearPreview();
      return;
    }
    activePreviewAnchorRef.current = anchor;
    setActivePreviewCard(card);
    setActivePreviewPosition(floatingPopoverPosition(anchor, 360, 503));
  };

  useEffect(() => {
    return () => cancelPreviewClose();
  }, []);

  useEffect(() => {
    if (!activePreviewCard || !activePreviewAnchorRef.current) {
      return;
    }
    const onViewportChange = () => {
      if (!activePreviewAnchorRef.current) {
        return;
      }
      setActivePreviewPosition(floatingPopoverPosition(activePreviewAnchorRef.current, 360, 503));
    };
    window.addEventListener("resize", onViewportChange);
    window.addEventListener("scroll", onViewportChange, true);
    return () => {
      window.removeEventListener("resize", onViewportChange);
      window.removeEventListener("scroll", onViewportChange, true);
    };
  }, [activePreviewCard]);

  return (
    <article
      className={className}
      aria-label={ariaLabel}
      onBlurCapture={(event) => {
        if (event.relatedTarget instanceof Node && event.currentTarget.contains(event.relatedTarget)) {
          return;
        }
        clearPreview();
      }}
    >
      {children({ showPreview, schedulePreviewClose })}
      {activePreviewCard && activePreviewPosition && activePreviewCard.imageUrl && typeof document !== "undefined"
        ? createPortal(
            <div
              className="card-preview-popover card-preview-popover-floating deck-curve-card-preview"
              style={{
                top: `${activePreviewPosition.top}px`,
                left: `${activePreviewPosition.left}px`,
                width: `${activePreviewPosition.width}px`,
                height: `${activePreviewPosition.height}px`,
              }}
              role="tooltip"
            >
              <img src={activePreviewCard.imageUrl} alt={cardDisplayName(activePreviewCard)} loading="lazy" />
            </div>,
            document.body,
          )
        : null}
    </article>
  );
}

function DeckCurveSection({
  title,
  cards,
}: {
  title: string;
  cards: MainboardDeckListCard[];
}) {
  const columns = useMemo(() => buildCurveColumns(cards), [cards]);

  if (columns.length === 0) {
    return null;
  }

  return (
    <DeckImagePreviewRegion className="deck-curve-panel" ariaLabel={`${title} arranged by mana value`}>
      {({ showPreview, schedulePreviewClose }) => (
        <>
          <div className="deck-curve-panel-head">
            <h4>{title}</h4>
            <p>{sectionTotal(cards).toLocaleString()} cards</p>
          </div>
          <div className="deck-curve-scroll">
            <div className="deck-curve-grid">
              {columns.map((column, columnIndex) => (
                <section className="deck-curve-column" key={column.key} aria-label={column.accessibleLabel}>
                  <div className="deck-curve-column-head">
                    <p className="deck-curve-column-count">{column.totalCards.toLocaleString()}</p>
                    <p className="deck-curve-column-label">{column.label}</p>
                  </div>
                  <div className="deck-curve-stack">
                    {buildCurveCardEntries(column.key, column.cards).map((entry, entryIndex) => (
                      <DeckImageCardLink
                        key={entry.key}
                        card={entry.card}
                        className="deck-curve-card"
                        quantityBadge={entry.quantityBadge}
                        eager={columnIndex < 2 && entryIndex < 2}
                        onPreviewStart={showPreview}
                        onPreviewEnd={schedulePreviewClose}
                      />
                    ))}
                  </div>
                </section>
              ))}
            </div>
          </div>
        </>
      )}
    </DeckImagePreviewRegion>
  );
}

function DeckVisualSection({
  title,
  cards,
  groupByType = false,
}: {
  title: string;
  cards: MainboardDeckListCard[];
  groupByType?: boolean;
}) {
  const groups = useMemo(() => {
    if (groupByType) {
      return buildVisualGroups(cards);
    }
    return [{
      key: "other" as const,
      label: title,
      cards: [...cards].sort(compareMainboardCards),
    }];
  }, [cards, groupByType, title]);

  if (groups.length === 0) {
    return null;
  }

  return (
    <DeckImagePreviewRegion className="deck-visual-panel" ariaLabel={`${title} card gallery`}>
      {({ showPreview, schedulePreviewClose }) => (
        <>
          <div className="deck-curve-panel-head">
            <h4>{title}</h4>
            <p>{sectionTotal(cards).toLocaleString()} cards</p>
          </div>
          <div className="deck-visual-scroll">
            <div className="deck-visual-groups">
              {groups.map((group) => (
                <section className="deck-visual-group" key={group.key} aria-label={group.label}>
                  {groupByType ? (
                    <h5>
                      {group.label} <span>({sectionTotal(group.cards).toLocaleString()})</span>
                    </h5>
                  ) : null}
                  <div className="deck-visual-cards">
                    {group.cards.map((card, cardIndex) => (
                      <DeckImageCardLink
                        key={`${group.key}-${card.cardId}`}
                        card={card}
                        className="deck-visual-card"
                        quantityBadge={card.quantity}
                        quantityBadgeText={`${card.quantity}×`}
                        displayImageUrl={card.artCropUrl}
                        eager={cardIndex < 4}
                        onPreviewStart={showPreview}
                        onPreviewEnd={schedulePreviewClose}
                      />
                    ))}
                  </div>
                </section>
              ))}
            </div>
          </div>
        </>
      )}
    </DeckImagePreviewRegion>
  );
}

function DeckCurveSkeleton({ title = "Mainboard", columnCount = 6 }: { title?: string; columnCount?: number }) {
  return (
    <article className="deck-curve-panel is-skeleton" aria-hidden="true">
      <div className="deck-curve-panel-head">
        <span className="skeleton-line skeleton-heading-sm" />
        <span className="skeleton-line skeleton-count" />
      </div>
      <div className="deck-curve-scroll">
        <div className="deck-curve-grid">
          {Array.from({ length: columnCount }).map((_, columnIndex) => (
            <div className="deck-curve-column" key={`${title}-curve-skeleton-${columnIndex}`}>
              <div className="deck-curve-column-head">
                <span className="skeleton-line skeleton-count" />
                <span className="skeleton-line skeleton-table-line is-short" />
              </div>
              <div className="deck-curve-stack">
                {Array.from({ length: 4 }).map((_, cardIndex) => (
                  <span className="deck-curve-card is-skeleton" key={`${title}-curve-card-${columnIndex}-${cardIndex}`} />
                ))}
              </div>
            </div>
          ))}
        </div>
      </div>
    </article>
  );
}

function DeckVisualSkeleton({ title = "Mainboard", groupCount = 4 }: { title?: string; groupCount?: number }) {
  return (
    <article className="deck-visual-panel is-skeleton" aria-hidden="true">
      <div className="deck-curve-panel-head">
        <span className="skeleton-line skeleton-heading-sm" />
        <span className="skeleton-line skeleton-count" />
      </div>
      <div className="deck-visual-scroll">
        <div className="deck-visual-groups">
          {Array.from({ length: groupCount }).map((_, groupIndex) => (
            <section className="deck-visual-group" key={`${title}-visual-skeleton-${groupIndex}`}>
              <span className="skeleton-line skeleton-table-line is-short" />
              <div className="deck-visual-cards">
                {Array.from({ length: groupIndex === 0 ? 5 : 3 }).map((_, cardIndex) => (
                  <span
                    className="deck-visual-card is-skeleton"
                    key={`${title}-visual-card-${groupIndex}-${cardIndex}`}
                  />
                ))}
              </div>
            </section>
          ))}
        </div>
      </div>
    </article>
  );
}

function clampSkeletonRows(value: number, fallback: number): number {
  if (!Number.isFinite(value) || value <= 0) {
    return fallback;
  }
  return Math.min(12, Math.max(3, value));
}

function DeckSectionSkeleton({ rowCount = 7, showMana = true }: { rowCount?: number; showMana?: boolean }) {
  return (
    <article className="deck-card is-skeleton" aria-hidden="true">
      <h4>
        <span className="skeleton-line skeleton-title" />
      </h4>
      <ul>
        {Array.from({ length: rowCount }).map((_, index) => (
          <li key={`deck-skeleton-row-${index}`}>
            <span className="deck-card-qty">
              <span className="skeleton-chip skeleton-qty" />
            </span>
            <span className="skeleton-line skeleton-card-name" />
            {showMana ? (
              <span className="deck-card-mana">
                <span className="skeleton-chip skeleton-mana" />
              </span>
            ) : null}
          </li>
        ))}
      </ul>
    </article>
  );
}

function DeckDetailSkeleton() {
  return (
    <div className="stack-lg deck-detail-stack" aria-busy="true" aria-live="polite">
      <section className="panel decklist-panel">
        <div className="panel-head">
          <div className="deck-skeleton-head">
            <span className="skeleton-line skeleton-heading" />
            <span className="skeleton-line skeleton-subheading" />
          </div>
          <span className="skeleton-line skeleton-link" aria-hidden="true" />
        </div>

        <div className="stack-md">
          <div className="grid-cards deck-mainboard-skeleton-grid">
            {MAINBOARD_SKELETON_CATEGORY_ORDER.map((category) => (
              <DeckSectionSkeleton key={`main-skeleton-${category}`} rowCount={6} />
            ))}
          </div>
          <DeckSectionSkeleton rowCount={5} />
        </div>
      </section>

      <section className="panel">
        <div className="panel-head">
          <span className="skeleton-line skeleton-heading-sm" aria-hidden="true" />
          <span className="skeleton-line skeleton-count" aria-hidden="true" />
        </div>
        <div className="table-wrap">
          <table className="data-table deck-matches-skeleton-table">
            <thead>
              <tr>
                <th>Started</th>
                <th>Event</th>
                <th>Opponent</th>
                <th>Result</th>
                <th>Turns</th>
                <th>Duration</th>
                <th>Details</th>
              </tr>
            </thead>
            <tbody>
              {Array.from({ length: 5 }).map((_, rowIndex) => (
                <tr key={`deck-match-skeleton-${rowIndex}`}>
                  <td>
                    <span className="skeleton-line skeleton-table-line" />
                  </td>
                  <td>
                    <span className="skeleton-line skeleton-table-line is-wide" />
                  </td>
                  <td>
                    <span className="skeleton-line skeleton-table-line is-wide" />
                    <span className="skeleton-line skeleton-table-line is-short" />
                  </td>
                  <td>
                    <span className="skeleton-line skeleton-table-line is-short" />
                  </td>
                  <td>
                    <span className="skeleton-line skeleton-table-line is-short" />
                  </td>
                  <td>
                    <span className="skeleton-line skeleton-table-line" />
                  </td>
                  <td>
                    <span className="skeleton-line skeleton-table-line is-short" />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  );
}

export function DeckDetailPage() {
  const params = useParams();
  const [searchParams, setSearchParams] = useSearchParams();
  const deckId = Number(params.deckId);
  const deckDisplayMode = parseDeckDisplayMode(searchParams.get("view"));

  const { data, isLoading, error } = useQuery({
    queryKey: ["deck", deckId],
    queryFn: () => api.deckDetail(deckId),
    enabled: Number.isFinite(deckId),
  });
  const { lookup: setLookup } = useEventSets([
    data?.eventName,
    ...(data?.matches ?? []).map((match) => match.eventName),
  ]);

  const cards = useMemo(() => {
    return (data?.cards ?? []).map((card) => ({
      section: card.section,
      cardId: card.cardId,
      cardName: card.cardName,
      quantity: card.quantity,
    }));
  }, [data?.cards]);

  const mainboardCards = useMemo(() => {
    return cards.filter((card) => card.section === "main");
  }, [cards]);

  const mainCardPreviewQueries = useQueries({
    queries: mainboardCards.map((card) => ({
      queryKey: cardPreviewQueryKey(card),
      queryFn: () => fetchCardPreview(card.cardId, card.cardName),
      enabled: card.cardId > 0,
      staleTime: 1000 * 60 * 60 * 24,
      gcTime: 1000 * 60 * 60 * 24,
      retry: 1,
    })),
  });

  const mainboardMetadataByCardID = useMemo(() => {
    const out = new Map<number, CardPreview>();
    for (let i = 0; i < mainboardCards.length; i += 1) {
      const card = mainboardCards[i];
      const preview = mainCardPreviewQueries[i]?.data;
      if (!preview) {
        continue;
      }
      out.set(card.cardId, preview);
    }
    return out;
  }, [mainboardCards, mainCardPreviewQueries]);

  const enrichedMainboardCards = useMemo(() => {
    return mainboardCards.map((card): MainboardDeckListCard => {
      const metadata = mainboardMetadataByCardID.get(card.cardId);
      return {
        ...card,
        manaCost: metadata?.manaCost?.trim() ?? "",
        manaValue:
          typeof metadata?.manaValue === "number" && Number.isFinite(metadata.manaValue) ? metadata.manaValue : null,
        imageUrl: metadata?.imageUrl,
        artCropUrl: metadata?.artCropUrl,
        scryfallUrl: metadata?.scryfallUrl,
        typeLine: metadata?.typeLine,
        rarity: metadata?.rarity,
      };
    });
  }, [mainboardCards, mainboardMetadataByCardID]);

  const groupedMainboardCards = useMemo(() => {
    const byCategory: Record<MainboardCategory, MainboardDeckListCard[]> = {
      creatures: [],
      spells: [],
      artifacts: [],
      enchantments: [],
      lands: [],
    };

    for (const card of enrichedMainboardCards) {
      const category = classifyMainboardCard(card.typeLine);
      byCategory[category].push(card);
    }

    for (const category of MAINBOARD_CATEGORY_ORDER) {
      if (category === "lands") {
        byCategory[category].sort(compareLandCards);
      } else {
        byCategory[category].sort(compareMainboardCards);
      }
    }

    return byCategory;
  }, [enrichedMainboardCards]);

  const nonMainSections = useMemo(() => {
    const bySection: Record<string, DeckListCard[]> = {};
    for (const card of cards) {
      if (card.section === "main") {
        continue;
      }
      if (!bySection[card.section]) {
        bySection[card.section] = [];
      }
      bySection[card.section].push(card);
    }

    for (const entries of Object.values(bySection)) {
      entries.sort((a, b) => cardDisplayName(a).localeCompare(cardDisplayName(b), undefined, { sensitivity: "base" }));
    }

    return bySection;
  }, [cards]);

  const sideboardCards = nonMainSections.sideboard ?? [];
  const auxiliarySections = useMemo(() => {
    return Object.entries(nonMainSections).filter(([section]) => section !== "sideboard");
  }, [nonMainSections]);

  const sideboardPreviewQueries = useQueries({
    queries: sideboardCards.map((card) => ({
      queryKey: cardPreviewQueryKey(card),
      queryFn: () => fetchCardPreview(card.cardId, card.cardName),
      enabled: card.cardId > 0,
      staleTime: 1000 * 60 * 60 * 24,
      gcTime: 1000 * 60 * 60 * 24,
      retry: 1,
    })),
  });

  const sideboardMetadataByCardID = useMemo(() => {
    const out = new Map<number, CardPreview>();
    for (let i = 0; i < sideboardCards.length; i += 1) {
      const card = sideboardCards[i];
      const preview = sideboardPreviewQueries[i]?.data;
      if (!preview) {
        continue;
      }
      out.set(card.cardId, preview);
    }
    return out;
  }, [sideboardCards, sideboardPreviewQueries]);

  const enrichedSideboardCards = useMemo(() => {
    return sideboardCards.map((card): SideboardDeckListCard => {
      const metadata = sideboardMetadataByCardID.get(card.cardId);
      return {
        ...card,
        manaCost: metadata?.manaCost?.trim() ?? "",
        manaValue:
          typeof metadata?.manaValue === "number" && Number.isFinite(metadata.manaValue) ? metadata.manaValue : null,
        imageUrl: metadata?.imageUrl,
        artCropUrl: metadata?.artCropUrl,
        scryfallUrl: metadata?.scryfallUrl,
        typeLine: metadata?.typeLine,
        rarity: metadata?.rarity,
      };
    });
  }, [sideboardCards, sideboardMetadataByCardID]);

  const curveShellWidth = useMemo(() => {
    const maxColumnCount = Math.max(
      buildCurveColumns(enrichedMainboardCards).length,
      buildCurveColumns(enrichedSideboardCards).length,
    );
    return estimateCurveShellWidth(maxColumnCount);
  }, [enrichedMainboardCards, enrichedSideboardCards]);

  useEffect(() => {
    if (typeof document === "undefined") {
      return;
    }

    const shouldUseWideCurveShell = deckDisplayMode === "curve" && curveShellWidth > DEFAULT_APP_SHELL_WIDTH;
    document.body.classList.toggle("has-wide-deck-curve", shouldUseWideCurveShell);

    if (shouldUseWideCurveShell) {
      document.body.style.setProperty("--deck-curve-shell-width", `${curveShellWidth}px`);
    } else {
      document.body.style.removeProperty("--deck-curve-shell-width");
    }

    return () => {
      document.body.classList.remove("has-wide-deck-curve");
      document.body.style.removeProperty("--deck-curve-shell-width");
    };
  }, [curveShellWidth, deckDisplayMode]);

  const isMainboardMetadataLoading = mainCardPreviewQueries.some((query) => query.isPending);
  const isSideboardMetadataLoading = sideboardPreviewQueries.some((query) => query.isPending);
  const isCardMetadataLoading = isMainboardMetadataLoading || isSideboardMetadataLoading;
  const mainboardSkeletonRows = clampSkeletonRows(Math.ceil(mainboardCards.length / MAINBOARD_CATEGORY_ORDER.length), 6);
  const sideboardSkeletonRows = clampSkeletonRows(sideboardCards.length, 5);

  if (!Number.isFinite(deckId)) return <StatusMessage tone="error">Invalid deck id.</StatusMessage>;
  if (isLoading) return <DeckDetailSkeleton />;
  if (error) return <StatusMessage tone="error">{(error as Error).message}</StatusMessage>;
  if (!data) return <StatusMessage>Deck not found.</StatusMessage>;

  const matches = data.matches ?? [];
  const versions = data.versions ?? [];
  const setDeckDisplayMode = (mode: DeckDisplayMode) => {
    setSearchParams(
      (current) => {
        const next = new URLSearchParams(current);
        if (mode === "list") {
          next.delete("view");
        } else {
          next.set("view", mode);
        }
        return next;
      },
      { replace: true },
    );
  };

  return (
    <div className={`stack-lg deck-detail-stack ${deckDisplayMode === "list" ? "" : "is-curve-layout"}`}>
      <section className="panel decklist-panel">
        <div className="panel-head">
          <div>
            <h3>{data.name || "Unnamed Deck"}</h3>
            <p>
              {data.format || "Unknown format"} •{" "}
              <EventLabel eventName={data.eventName} lookup={setLookup} fallback="No event" />
            </p>
            {!isCardMetadataLoading ? (
              <div className="deck-rarity-summary">
                <RaritySummaryGroup label="Main" cards={enrichedMainboardCards} />
                <RaritySummaryGroup label="Side" cards={enrichedSideboardCards} />
              </div>
            ) : null}
          </div>
          <div className="deck-detail-actions">
            <div className="tabs deck-view-toggle" role="group" aria-label="Deck display mode">
              <button
                type="button"
                className={`tab ${deckDisplayMode === "list" ? "is-active" : ""}`}
                aria-pressed={deckDisplayMode === "list"}
                onClick={() => setDeckDisplayMode("list")}
              >
                List
              </button>
              <button
                type="button"
                className={`tab ${deckDisplayMode === "curve" ? "is-active" : ""}`}
                aria-pressed={deckDisplayMode === "curve"}
                onClick={() => setDeckDisplayMode("curve")}
              >
                Curve
              </button>
              <button
                type="button"
                className={`tab ${deckDisplayMode === "visual" ? "is-active" : ""}`}
                aria-pressed={deckDisplayMode === "visual"}
                onClick={() => setDeckDisplayMode("visual")}
              >
                Visual
              </button>
            </div>
            <Link className="text-link" to="/decks">
              Back to decks
            </Link>
          </div>
        </div>

        <div className="stack-md">
          {deckDisplayMode === "curve" ? (
            isMainboardMetadataLoading ? (
              <DeckCurveSkeleton title="Mainboard" />
            ) : (
              <DeckCurveSection title="Mainboard" cards={enrichedMainboardCards} />
            )
          ) : deckDisplayMode === "visual" ? (
            isMainboardMetadataLoading ? (
              <DeckVisualSkeleton title="Mainboard" />
            ) : (
              <DeckVisualSection title="Mainboard" cards={enrichedMainboardCards} groupByType />
            )
          ) : (
            isMainboardMetadataLoading ? (
              <div className="grid-cards deck-mainboard-skeleton-grid">
                {MAINBOARD_SKELETON_CATEGORY_ORDER.map((category) => (
                  <DeckSectionSkeleton key={`main-loading-${category}`} rowCount={mainboardSkeletonRows} />
                ))}
              </div>
            ) : (
              <div className="grid-cards">
                {MAINBOARD_CATEGORY_ORDER.map((category) => {
                  const categoryCards = groupedMainboardCards[category];
                  if (categoryCards.length === 0) {
                    return null;
                  }
                  return (
                    <article className="deck-card" key={`main-${category}`}>
                      <h4>
                        {formatSectionLabel(category)} ({sectionTotal(categoryCards)})
                      </h4>
                      <ul>
                        {categoryCards.map((card) => (
                          <li key={`main-${category}-${card.cardId}`}>
                            <span className="deck-card-qty">{card.quantity}x</span>
                            <DeckCardPreviewName card={card} />
                            <span className="deck-card-mana">
                              <ManaCostDisplay manaCost={card.manaCost} />
                              <RarityDot rarity={card.rarity} />
                            </span>
                          </li>
                        ))}
                      </ul>
                    </article>
                  );
                })}
              </div>
            )
          )}

          {sideboardCards.length > 0 ? (
            deckDisplayMode === "curve" ? (
              isSideboardMetadataLoading ? (
                <DeckCurveSkeleton title="Sideboard" columnCount={4} />
              ) : (
                <DeckCurveSection title="Sideboard" cards={enrichedSideboardCards} />
              )
            ) : deckDisplayMode === "visual" ? (
              isSideboardMetadataLoading ? (
                <DeckVisualSkeleton title="Sideboard" groupCount={1} />
              ) : (
                <DeckVisualSection title="Sideboard" cards={enrichedSideboardCards} />
              )
            ) : isSideboardMetadataLoading ? (
              <DeckSectionSkeleton rowCount={sideboardSkeletonRows} />
            ) : (
              <article className="deck-card">
                <h4>
                  {formatSectionLabel("sideboard")} ({sectionTotal(enrichedSideboardCards)})
                </h4>
                <ul>
                  {enrichedSideboardCards.map((card) => (
                    <li key={`sideboard-${card.cardId}`}>
                      <span className="deck-card-qty">{card.quantity}x</span>
                      <DeckCardPreviewName card={card} placementMode="force-right" />
                      <span className="deck-card-mana">
                        <ManaCostDisplay manaCost={card.manaCost} />
                        <RarityDot rarity={card.rarity} />
                      </span>
                    </li>
                  ))}
                </ul>
              </article>
            )
          ) : null}

          {auxiliarySections.length > 0 ? (
            <div className="grid-cards">
              {auxiliarySections.map(([section, sectionCards]) => (
                <article className="deck-card" key={section}>
                  <h4>
                    {formatSectionLabel(section)} ({sectionTotal(sectionCards)})
                  </h4>
                  <ul>
                    {sectionCards.map((card) => (
                      <li key={`${section}-${card.cardId}`}>
                        <span className="deck-card-qty">{card.quantity}x</span>
                        <DeckCardPreviewName card={card} />
                      </li>
                    ))}
                  </ul>
                </article>
              ))}
            </div>
          ) : null}
        </div>
        {isCardMetadataLoading ? <StatusMessage>Loading deck card details…</StatusMessage> : null}
      </section>

      <section className="panel deck-versions-panel">
        <div className="panel-head">
          <div>
            <h3>Deck Versions</h3>
            <p>Immutable card snapshots used by match analytics</p>
          </div>
          <span className="deck-version-count">
            {versions.length.toLocaleString()} version{versions.length === 1 ? "" : "s"}
          </span>
        </div>
        {versions.length === 0 ? (
          <StatusMessage>
            A version will be created the next time Arena reports this deck list.
          </StatusMessage>
        ) : (
          <ol className="deck-version-list" aria-label="Deck version history">
            {versions.map((version, index) => {
              const totalCards = version.cards.reduce((sum, card) => sum + card.quantity, 0);
              const mainCards = version.cards
                .filter((card) => card.section === "main")
                .reduce((sum, card) => sum + card.quantity, 0);
              const sideboardCards = totalCards - mainCards;
              return (
                <li className="deck-version-row" key={version.id}>
                  <div className="deck-version-identity">
                    <strong>Version {version.versionNumber.toLocaleString()}</strong>
                    {index === 0 ? <span className="deck-version-current">Current</span> : null}
                  </div>
                  <dl>
                    <div>
                      <dt>Observed</dt>
                      <dd>{version.effectiveAt ? formatDateTime(version.effectiveAt) : "Unknown"}</dd>
                    </div>
                    <div>
                      <dt>Cards</dt>
                      <dd>
                        {mainCards.toLocaleString()} main
                        {sideboardCards > 0 ? ` · ${sideboardCards.toLocaleString()} side` : ""}
                      </dd>
                    </div>
                    <div>
                      <dt>Source</dt>
                      <dd>{version.source?.split("_").join(" ") || "Arena deck list"}</dd>
                    </div>
                    <div>
                      <dt>Fingerprint</dt>
                      <dd title={version.cardsHash}>{version.cardsHash.slice(0, 10)}</dd>
                    </div>
                  </dl>
                </li>
              );
            })}
          </ol>
        )}
      </section>

      <DeckAnalyticsPanel deckId={deckId} versions={versions} />

      {/draft|sealed|limited/.test(`${data.format} ${data.eventName}`.toLowerCase()) ? (
        <LimitedMatchupsPanel setCode={parseEventName(data.eventName).setCode ?? ""} />
      ) : (
        <DeckMatchupsPanel deckId={deckId} />
      )}

      <DeckPrimerPanel deckId={deckId} />

      <section className="panel">
        <div className="panel-head">
          <h3>Matches with this deck</h3>
          <p>{matches.length} matches</p>
        </div>
        <div className="table-wrap">
          <table className="data-table">
            <thead>
              <tr>
                <th>Started</th>
                <th>Event</th>
                <th>Opponent</th>
                <th>Result</th>
                <th>Turns</th>
                <th>Duration</th>
                <th>Details</th>
              </tr>
            </thead>
            <tbody>
              {matches.map((match) => (
                <tr key={match.id}>
                  <td>{formatDateTime(match.startedAt)}</td>
                  <td>
                    <EventLabel eventName={match.eventName} lookup={setLookup} />
                  </td>
                  <td>
                    <div className="deck-match-opponent-cell">
                      <span>{match.opponent || "-"}</span>
                      <DeckColorIdentity
                        className="deck-match-opponent-colors"
                        colors={match.opponentDeckColors}
                        known={match.opponentDeckColorsKnown}
                      />
                    </div>
                  </td>
                  <td>
                    <ResultPill result={match.result} />
                  </td>
                  <td>{match.turnCount ?? "-"}</td>
                  <td>{formatDuration(match.secondsCount ?? undefined)}</td>
                  <td>
                    <Link className="text-link" to={`/matches/${match.id}`}>
                      View
                    </Link>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  );
}
