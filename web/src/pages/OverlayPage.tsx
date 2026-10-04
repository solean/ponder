import { useEffect, useId, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useQueries, useQuery } from "@tanstack/react-query";

import { ManaSymbol } from "../components/ManaSymbol";
import { CardPreviewName } from "../components/CardPreviewName";
import { api } from "../lib/api";
import { DEFAULT_OVERLAY_SETTINGS, overlayPanelWidth, overlayShortcutLabel } from "../lib/overlaySettings";
import { fetchCardPreview } from "../lib/scryfall";
import type { LiveMatch } from "../lib/types";

function cardName(card: { cardId: number; cardName?: string }): string {
  return card.cardName?.trim() || `Card ${card.cardId}`;
}

function compareCardNames(
  left: { cardId: number; cardName?: string },
  right: { cardId: number; cardName?: string },
): number {
  const byName = cardName(left).localeCompare(cardName(right), undefined, { sensitivity: "base" });
  return byName || left.cardId - right.cardId;
}

function useOrderedCards<T extends { cardId: number; cardName?: string }>(cards: T[]) {
  // Reuse the deck page's batched metadata requests and the hover preview cache.
  const previews = useQueries({
    queries: cards.map((card) => ({
      queryKey: ["card-preview", card.cardId, cardName(card)],
      queryFn: () => fetchCardPreview(card.cardId, card.cardName),
      enabled: card.cardId > 0,
      staleTime: 1000 * 60 * 60 * 24,
      gcTime: 1000 * 60 * 60 * 24,
      retry: 1,
    })),
  });

  return useMemo(() => {
    const ranked = cards.map((card, index) => {
      const metadata = previews[index]?.data;
      return {
        card,
        isLand: /\bland\b/i.test(metadata?.typeLine?.split(" // ")[0] ?? ""),
        manaCost: metadata?.manaCost ?? "",
        manaValue: metadata?.manaValue ?? Number.POSITIVE_INFINITY,
      };
    });
    ranked.sort((left, right) => {
      if (left.isLand !== right.isLand) return left.isLand ? 1 : -1;
      if (!left.isLand && left.manaValue !== right.manaValue) {
        return left.manaValue - right.manaValue;
      }
      return compareCardNames(left.card, right.card);
    });
    return ranked.map(({ card, isLand, manaCost }) => ({ ...card, isLand, manaCost }));
  }, [cards, previews]);
}

function OverlayManaCost({ cost }: { cost: string }) {
  if (!cost) return <span />;
  return (
    <span className="overlay-mana-cost" aria-label={`Mana cost ${cost}`}>
      {cost.split(/(\{[^}]+\})/).filter(Boolean).map((part, index) =>
        part.startsWith("{") ? <ManaSymbol key={index} token={part.slice(1, -1)} /> : <span key={index}>{part.trim()}</span>,
      )}
    </span>
  );
}

function OverlaySubmenu({ title, count, children }: { title: string; count: number; children: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);
  const flyoutRef = useRef<HTMLDivElement>(null);
  const id = useId();
  const [open, setOpen] = useState(false);
  const [position, setPosition] = useState({ left: 0, top: 0, width: 288 });

  useLayoutEffect(() => {
    if (!open) return;
    const reposition = () => {
      const trigger = ref.current?.getBoundingClientRect();
      const panel = ref.current?.closest(".overlay-panel")?.getBoundingClientRect();
      const flyout = flyoutRef.current;
      if (!trigger || !panel || !flyout) return;
      const width = Math.min(panel.width, window.innerWidth - panel.right);
      setPosition({
        left: panel.right,
        top: Math.max(0, Math.min(trigger.top, window.innerHeight - flyout.offsetHeight)),
        width,
      });
    };
    reposition();
    const observer = new ResizeObserver(reposition);
    if (flyoutRef.current) observer.observe(flyoutRef.current);
    const panel = ref.current?.closest(".overlay-panel");
    if (panel) observer.observe(panel);
    window.addEventListener("resize", reposition);
    window.addEventListener("scroll", reposition, true);
    return () => {
      observer.disconnect();
      window.removeEventListener("resize", reposition);
      window.removeEventListener("scroll", reposition, true);
    };
  }, [open]);

  useEffect(() => {
    const onPointer = (event: Event) => {
      const point = (event as CustomEvent<{ x: number; y: number } | null>).detail;
      const contains = (element: HTMLElement | null) => {
        const rect = element?.getBoundingClientRect();
        return Boolean(point && rect && point.x * window.innerWidth >= rect.left &&
          point.x * window.innerWidth <= rect.right + 1 && point.y * window.innerHeight >= rect.top &&
          point.y * window.innerHeight < rect.bottom);
      };
      setOpen(contains(ref.current) || contains(flyoutRef.current));
    };
    window.addEventListener("ponder:overlay-pointer", onPointer);
    return () => window.removeEventListener("ponder:overlay-pointer", onPointer);
  }, []);

  return (
    <div className="overlay-submenu" ref={ref}
      onMouseEnter={() => setOpen(true)} onMouseLeave={() => setOpen(false)}
      onKeyDown={(event) => { if (event.key === "Escape") setOpen(false); }}
      onBlur={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget) && !flyoutRef.current?.contains(event.relatedTarget)) setOpen(false);
      }}>
      <button type="button" aria-expanded={open} aria-controls={open ? id : undefined}
        onClick={() => setOpen((current) => !current)}>
        <span>{title}</span><span className="overlay-submenu-count">{count}</span><span aria-hidden="true">›</span>
      </button>
      {open && createPortal(
        <div id={id} ref={flyoutRef} className="overlay-panel overlay-submenu-flyout"
          role="region" aria-label={title} style={position}>
          <div className="overlay-submenu-heading">{title}<span>{count}</span></div>
          {children}
        </div>, document.body,
      )}
    </div>
  );
}

function DeckPanel({ live, hoveredCard, shortcut }: { live: LiveMatch; hoveredCard: string | null; shortcut: string }) {
  const cards = useOrderedCards(live.deck);
  const main = cards.filter((card) => card.section === "main" && !card.isLand);
  const lands = cards.filter((card) => card.section === "main" && card.isLand);
  const sideboard = cards.filter((card) => card.section === "sideboard");
  const libraryCount = live.libraryCount;
  const hasLibraryCount = libraryCount != null;
  const sourceLabel =
    live.deckSource === "submitted"
      ? `Game ${Math.max(live.gameNumber, 1)} submitted deck`
      : live.deckSource === "linked"
        ? "Linked deck estimate"
        : "Deck submission unavailable";

  const renderCards = (entries: typeof cards) => (
    <ul className="overlay-card-list">
      {entries.map((card) => {
        const name = cardName(card);
        const remainingLabel = card.remaining == null ? "Unknown" : `${card.remaining} of ${card.quantity}`;
        return (
          <li className={card.section === "main" && card.remaining === 0 ? "is-empty" : undefined} key={`${card.section}:${card.cardId}`} data-overlay-card={`deck:${card.section}:${card.cardId}`}>
            <span className="overlay-card-mark" aria-hidden="true" />
            <div className="overlay-card-name">
              <CardPreviewName cardId={card.cardId} cardName={card.cardName} label={<span>{name}</span>} passiveHover={hoveredCard === `deck:${card.section}:${card.cardId}`} />
            </div>
            <OverlayManaCost cost={card.manaCost} />
            <span className="overlay-card-count" aria-label={card.section === "sideboard" ? `${card.quantity} sideboard copies` : `${remainingLabel} copies left`}>
              <strong>{card.section === "sideboard" ? card.quantity : card.remaining ?? "—"}</strong>
              {card.section === "main" && <span>/{card.quantity}</span>}
            </span>
          </li>
        );
      })}
    </ul>
  );

  return (
    <aside className="overlay-panel overlay-panel-deck" aria-labelledby="overlay-deck-title">
      <header className="overlay-panel-head">
        <div className="overlay-panel-heading">
          <p className="overlay-eyebrow">Your deck</p>
          <h1 id="overlay-deck-title">{live.match.deckName?.trim() || "Current deck"}</h1>
          <p className="overlay-panel-meta">{sourceLabel}</p>
        </div>
        <div className="overlay-total" aria-label={hasLibraryCount ? `${libraryCount} cards left in deck` : "Cards left unknown"}>
          <strong>{hasLibraryCount ? libraryCount : "—"}</strong>
          <span>cards left</span>
        </div>
      </header>

      <div className="overlay-list-head" aria-hidden="true">
        <span>Card</span>
        <span>Left</span>
      </div>
      {cards.length > 0 ? (
        <div className="overlay-deck-sections">
          {renderCards(main)}
          {lands.length > 0 && <OverlaySubmenu title="Lands" count={lands.reduce((sum, card) => sum + card.quantity, 0)}>{renderCards(lands)}</OverlaySubmenu>}
          {sideboard.length > 0 && <OverlaySubmenu title="Sideboard" count={sideboard.reduce((sum, card) => sum + card.quantity, 0)}>{renderCards(sideboard)}</OverlaySubmenu>}
        </div>
      ) : (
        <p className="overlay-empty">Waiting for the submitted decklist.</p>
      )}
      <footer className="overlay-panel-foot">
        <span className={`overlay-state-dot ${hasLibraryCount ? "is-ready" : ""}`} aria-hidden="true" />
        {hasLibraryCount ? `${live.deckTotal - libraryCount} known outside the library` : "Waiting for full game state"}
      </footer>
      <div className="overlay-panel-foot">
        {overlayShortcutLabel(shortcut)} · hide/show overlay
      </div>
    </aside>
  );
}

function OpponentPanel({ live, hoveredCard }: { live: LiveMatch; hoveredCard: string | null }) {
  const cards = useOrderedCards(live.opponentObservedCards);
  const observedCopies = cards.reduce((total, card) => total + card.quantity, 0);

  return (
    <aside className="overlay-panel overlay-panel-opponent" aria-labelledby="overlay-opponent-title">
      <header className="overlay-panel-head">
        <div className="overlay-panel-heading">
          <p className="overlay-eyebrow">Opponent seen</p>
          <h2 id="overlay-opponent-title">{live.match.opponent?.trim() || "Unknown opponent"}</h2>
          <p className="overlay-panel-meta">Public cards from this match</p>
        </div>
        <div className="overlay-total" aria-label={`${observedCopies} opponent cards seen`}>
          <strong>{observedCopies}</strong>
          <span>seen</span>
        </div>
      </header>

      <div className="overlay-list-head" aria-hidden="true">
        <span>Card</span>
        <span>Seen</span>
      </div>
      {cards.length > 0 ? (
        <ul className="overlay-card-list">
          {cards.map((card) => {
            const name = cardName(card);
            return (
              <li key={card.cardId} data-overlay-card={`opponent:${card.cardId}`}>
                <span className="overlay-card-mark" aria-hidden="true" />
                <div className="overlay-card-name">
                  <CardPreviewName cardId={card.cardId} cardName={card.cardName} label={<span>{name}</span>} passiveHover={hoveredCard === `opponent:${card.cardId}`} />
                </div>
                <OverlayManaCost cost={card.manaCost} />
                <span className="overlay-card-count" aria-label={`${card.quantity} copies seen`}>
                  <strong>{card.quantity}</strong>
                </span>
              </li>
            );
          })}
        </ul>
      ) : (
        <p className="overlay-empty">No opponent cards revealed yet.</p>
      )}
      <footer className="overlay-panel-foot">
        <span className="overlay-state-dot is-ready" aria-hidden="true" />
        Public information only
      </footer>
    </aside>
  );
}

export function OverlayPage() {
  const hudRef = useRef<HTMLElement | null>(null);
  const [hoveredCard, setHoveredCard] = useState<string | null>(null);
  const settingsQuery = useQuery({
    queryKey: ["overlay-settings"],
    queryFn: api.overlaySettings,
    refetchInterval: 2000,
    refetchIntervalInBackground: true,
  });
  const settings = settingsQuery.data ?? DEFAULT_OVERLAY_SETTINGS;
  const [previewCard, setPreviewCard] = useState<string | null>(null);

  useEffect(() => {
    setPreviewCard(null);
    if (!settings.cardPreviews || !hoveredCard) return;
    const timer = window.setTimeout(() => setPreviewCard(hoveredCard), settings.hoverDelayMs);
    return () => window.clearTimeout(timer);
  }, [hoveredCard, settings.cardPreviews, settings.hoverDelayMs]);
  const activePreview = settings.cardPreviews && previewCard === hoveredCard ? previewCard : null;

  useLayoutEffect(() => {
    // Root variables also reach the portaled Lands and Sideboard flyouts.
    const root = document.documentElement;
    const values: Record<string, string> = {
      "--overlay-panel-width": `${overlayPanelWidth(settings.panelSize)}rem`,
      "--overlay-opacity": String(settings.opacity),
      "--overlay-card-font-size": settings.panelSize === "large" ? "0.875rem" : settings.panelSize === "compact" ? "0.6875rem" : "0.75rem",
      "--overlay-row-height": settings.panelSize === "large" ? "2.3rem" : settings.panelSize === "compact" ? "1.75rem" : "2rem",
    };
    const previous = Object.keys(values).map((key) => [key, root.style.getPropertyValue(key)]);
    for (const [key, value] of Object.entries(values)) root.style.setProperty(key, value);
    return () => {
      for (const [key, value] of previous) {
        if (value) root.style.setProperty(key, value);
        else root.style.removeProperty(key);
      }
    };
  }, [settings.panelSize, settings.opacity]);


  useEffect(() => {
    const updateHover = (event: Event) => {
      const point = (event as CustomEvent<{ x: number; y: number } | null>).detail;
      let next: string | null = null;
      if (point && hudRef.current) {
        const x = point.x * window.innerWidth;
        const y = point.y * window.innerHeight;
        // The native overlay remains click-through. Hit-test whole visible rows
        // so names, counts, and row padding all reveal the same card preview.
        for (const row of document.querySelectorAll<HTMLElement>(".overlay-hud [data-overlay-card], .overlay-submenu-flyout [data-overlay-card]")) {
          const list = row.parentElement;
          if (!list) continue;
          const rect = row.getBoundingClientRect();
          const clip = list.getBoundingClientRect();
          // Nested menus can be clipped by the scrolling deck section or panel.
          for (let ancestor = list.parentElement; ancestor && ancestor !== hudRef.current && ancestor !== document.body; ancestor = ancestor.parentElement) {
            const bounds = ancestor.getBoundingClientRect();
            const bottom = Math.min(clip.bottom, bounds.bottom);
            clip.y = Math.max(clip.top, bounds.top);
            clip.height = Math.max(0, bottom - clip.top);
          }
          if (x >= Math.max(rect.left, clip.left) && x < Math.min(rect.right, clip.right) &&
              y >= Math.max(rect.top, clip.top) && y < Math.min(rect.bottom, clip.bottom)) {
            next = row.dataset.overlayCard ?? null;
            break;
          }
        }
      }
      setHoveredCard((current) => current === next ? current : next);
    };
    window.addEventListener("ponder:overlay-pointer", updateHover);
    return () => window.removeEventListener("ponder:overlay-pointer", updateHover);
  }, []);

  useEffect(() => {
    const root = document.documentElement;
    const previousTitle = document.title;
    root.classList.add("overlay-document");
    document.title = "Ponder Overlay";
    return () => {
      root.classList.remove("overlay-document");
      document.title = previousTitle;
    };
  }, []);

  const liveQuery = useQuery({
    queryKey: ["live"],
    queryFn: api.live,
    refetchInterval: (query) => (query.state.data?.live ? 2000 : 5000),
    refetchIntervalInBackground: true,
  });

  // The native window is ordered out whenever Arena is not frontmost, which
  // can throttle or suspend this webview's timers; refetch on the way back in.
  // The listener needs a stable identity to be removable.
  const refetchLive = liveQuery.refetch;
  const refetchSettings = settingsQuery.refetch;
  useEffect(() => {
    const onShown = () => { void refetchLive(); void refetchSettings(); };
    window.addEventListener("ponder:overlay-shown", onShown);
    return () => window.removeEventListener("ponder:overlay-shown", onShown);
  }, [refetchLive, refetchSettings]);
  const live = liveQuery.data?.live ?? null;

  if (liveQuery.isError) {
    return (
      <main className="overlay-hud overlay-hud-status" aria-live="polite">
        <p>Live game data unavailable</p>
      </main>
    );
  }
  if (!live) {
    return <main className="overlay-hud" aria-label="Ponder game overlay" />;
  }

  return (
    <main className="overlay-hud" aria-label="Ponder game overlay" ref={hudRef}>
      {settings.showDeck && <DeckPanel key={`${live.match.id}:${live.gameNumber}`} live={live} hoveredCard={activePreview} shortcut={settings.shortcut} />}
      {settings.showOpponent && <OpponentPanel live={live} hoveredCard={activePreview} />}
    </main>
  );
}
