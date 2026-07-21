import { useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { Link, useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";

import { DraftJourneyPanel, DraftPackReplayPanel } from "../components/DraftJourneyPanel";
import { DraftPoolPanel } from "../components/DraftPoolPanel";
import { LimitedMatchupsPanel } from "../components/MatchupPanels";
import { StatusMessage } from "../components/StatusMessage";
import { api } from "../lib/api";
import { parseEventName } from "../lib/events";
import { fetchCardPreview } from "../lib/scryfall";
import type { DraftPickCard } from "../lib/types";

type DraftPickDisplay = {
  pickNumber: number;
  displayPick: number;
  pickedCards: DraftPickCard[];
};

type DraftPackDisplay = {
  packNumber: number;
  displayPack: number;
  picks: DraftPickDisplay[];
};

type PopoverPlacement = "left" | "right";
type FloatingPopoverPosition = {
  top: number;
  left: number;
  width: number;
};

function parseDraftCardIDs(raw: string): number[] {
  const trimmed = raw.trim();
  if (!trimmed || trimmed === "[]") {
    return [];
  }

  try {
    const decoded = JSON.parse(trimmed) as unknown;
    if (Array.isArray(decoded)) {
      return decoded
        .map((value) => {
          if (typeof value === "number" && Number.isFinite(value)) {
            return Math.trunc(value);
          }
          if (typeof value === "string") {
            const parsed = Number.parseInt(value.trim(), 10);
            return Number.isFinite(parsed) ? parsed : 0;
          }
          return 0;
        })
        .filter((cardID) => cardID > 0);
    }
  } catch {
    // Fall through to regex fallback.
  }

  const matches = trimmed.match(/\d+/g);
  if (!matches) {
    return [];
  }
  return matches
    .map((value) => Number.parseInt(value, 10))
    .filter((cardID) => Number.isFinite(cardID) && cardID > 0);
}

function normalizedDraftCards(rawCardIDs: string, cards?: DraftPickCard[]): DraftPickCard[] {
  if (Array.isArray(cards) && cards.length > 0) {
    return cards.map((card) => ({ cardId: card.cardId, cardName: card.cardName }));
  }

  const parsedIDs = parseDraftCardIDs(rawCardIDs);
  return parsedIDs.map((cardId) => ({ cardId }));
}

function draftCardDisplayName(card: DraftPickCard, fallbackName?: string): string {
  const known = card.cardName?.trim();
  if (known) {
    return known;
  }
  const resolved = fallbackName?.trim();
  if (resolved) {
    return resolved;
  }
  return `Card ${card.cardId}`;
}

function draftCardPreviewQueryKey(card: DraftPickCard): [string, number, string] {
  return ["card-preview", card.cardId, card.cardName?.trim() || `Card ${card.cardId}`];
}

function DraftCardPreviewName({ card }: { card: DraftPickCard }) {
  const [isOpen, setIsOpen] = useState(false);
  const [popoverPlacement, setPopoverPlacement] = useState<PopoverPlacement>("right");
  const [popoverPosition, setPopoverPosition] = useState<FloatingPopoverPosition | null>(null);
  const wrapperRef = useRef<HTMLDivElement | null>(null);
  const knownName = card.cardName?.trim() ?? "";

  const previewQuery = useQuery({
    queryKey: draftCardPreviewQueryKey(card),
    queryFn: () => fetchCardPreview(card.cardId, knownName || undefined),
    enabled: card.cardId > 0 && (isOpen || knownName.length === 0),
    staleTime: 1000 * 60 * 60 * 24,
    gcTime: 1000 * 60 * 60 * 24,
    retry: 1,
  });

  const name = draftCardDisplayName(card, previewQuery.data?.name);
  const fallbackHref =
    knownName || previewQuery.data?.name?.trim()
      ? `https://scryfall.com/search?q=${encodeURIComponent(`!"${knownName || previewQuery.data?.name?.trim()}"`)}`
      : `https://scryfall.com/search?q=${encodeURIComponent(`arenaid:${card.cardId}`)}`;

  const updatePopoverPlacement = () => {
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
    const estimatedPopoverHeight = 468;
    const horizontalGap = 14;
    const viewportHeight = window.innerHeight || document.documentElement.clientHeight;
    const availableRight = viewportWidth - rect.right;
    const availableLeft = rect.left;
    const viewportPadding = 8;
    let placement: PopoverPlacement;

    if (availableRight >= popoverWidth + horizontalGap) {
      placement = "right";
    } else if (availableLeft >= popoverWidth + horizontalGap) {
      placement = "left";
    } else {
      placement = availableRight >= availableLeft ? "right" : "left";
    }

    const rawLeft = placement === "right" ? rect.right + horizontalGap : rect.left - popoverWidth - horizontalGap;
    const left = Math.max(viewportPadding, Math.min(rawLeft, viewportWidth - popoverWidth - viewportPadding));
    const rawTop = rect.top + rect.height / 2 - estimatedPopoverHeight / 2;
    const top = Math.max(viewportPadding, Math.min(rawTop, viewportHeight - estimatedPopoverHeight - viewportPadding));

    setPopoverPlacement(placement);
    setPopoverPosition({ top, left, width: popoverWidth });
  };

  const openPopover = () => {
    updatePopoverPlacement();
    setIsOpen(true);
  };

  useEffect(() => {
    if (!isOpen) {
      return;
    }
    const onViewportChange = () => updatePopoverPlacement();
    window.addEventListener("resize", onViewportChange);
    window.addEventListener("scroll", onViewportChange, true);
    return () => {
      window.removeEventListener("resize", onViewportChange);
      window.removeEventListener("scroll", onViewportChange, true);
    };
  }, [isOpen]);

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

      {isOpen && popoverPosition && typeof document !== "undefined"
        ? createPortal(
            <div
              className="card-preview-popover card-preview-popover-floating"
              role="tooltip"
              style={{ top: `${popoverPosition.top}px`, left: `${popoverPosition.left}px`, width: `${popoverPosition.width}px` }}
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

function DraftCardList({ cards }: { cards: DraftPickCard[] }) {
  if (cards.length === 0) {
    return <span className="draft-card-empty">-</span>;
  }

  return (
    <div className="draft-card-list">
      {cards.map((card, index) => (
        <DraftCardPreviewName card={card} key={`${card.cardId}-${index}`} />
      ))}
    </div>
  );
}

export function DraftDetailPage() {
  const params = useParams();
  const draftId = Number(params.draftId);

  const picksQuery = useQuery({
    queryKey: ["draft-picks", draftId],
    queryFn: () => api.draftPicks(draftId),
    enabled: Number.isFinite(draftId),
  });
  const sessionsQuery = useQuery({
    queryKey: ["drafts"],
    queryFn: api.drafts,
  });
  const session = useMemo(
    () => (sessionsQuery.data ?? []).find((row) => row.id === draftId) ?? null,
    [sessionsQuery.data, draftId],
  );

  const packs = useMemo<DraftPackDisplay[]>(() => {
    const rows = picksQuery.data ?? [];
    // Arena reports pack/pick numbers zero-based; show them one-based (and stay
    // a no-op if a source ever reports them one-based already).
    const packOffset = rows.some((pick) => pick.packNumber === 0) ? 1 : 0;
    const pickOffset = rows.some((pick) => pick.pickNumber === 0) ? 1 : 0;
    const map = new Map<number, DraftPickDisplay[]>();
    for (const pick of rows) {
      const existing = map.get(pick.packNumber) ?? [];
      existing.push({
        pickNumber: pick.pickNumber,
        displayPick: pick.pickNumber + pickOffset,
        pickedCards: normalizedDraftCards(pick.pickedCardIds, pick.pickedCards),
      });
      map.set(pick.packNumber, existing);
    }
    return [...map.entries()]
      .sort((a, b) => a[0] - b[0])
      .map(([packNumber, picks]) => ({
        packNumber,
        displayPack: packNumber + packOffset,
        picks: picks.sort((a, b) => a.pickNumber - b.pickNumber),
      }));
  }, [picksQuery.data]);

  if (!Number.isFinite(draftId)) return <StatusMessage tone="error">Invalid draft id.</StatusMessage>;
  if (picksQuery.isLoading) return <StatusMessage>Loading draft picks…</StatusMessage>;
  if (picksQuery.error) return <StatusMessage tone="error">{(picksQuery.error as Error).message}</StatusMessage>;

  const picks = picksQuery.data ?? [];

  return (
    <div className="stack-lg">
      <section className="panel decklist-panel">
        <div className="panel-head">
          <div>
            <h3>Draft Session #{draftId}</h3>
            {session ? <p>{session.eventName}</p> : null}
          </div>
          <Link className="text-link" to="/drafts">
            Back to drafts
          </Link>
        </div>

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
                          <DraftCardList cards={pick.pickedCards} />
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </article>
          ))}
        </div>
      </section>

      <DraftJourneyPanel picks={picks} />
      {session ? <DraftPoolPanel eventName={session.eventName} picks={picks} /> : null}
      <DraftPackReplayPanel picks={picks} />
      {session ? <LimitedMatchupsPanel setCode={parseEventName(session.eventName).setCode ?? ""} /> : null}
    </div>
  );
}
