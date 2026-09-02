import { useEffect, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";

import { CardPreviewName } from "../components/CardPreviewName";
import { api } from "../lib/api";
import type { LiveDeckCard, LiveMatch, OpponentObservedCard } from "../lib/types";

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

function DeckPanel({ live }: { live: LiveMatch }) {
  const cards = useMemo(
    () =>
      [...live.deck].sort((left, right) => {
        const leftEmpty = left.remaining === 0;
        const rightEmpty = right.remaining === 0;
        if (leftEmpty !== rightEmpty) return leftEmpty ? 1 : -1;
        return compareCardNames(left, right);
      }),
    [live.deck],
  );
  const libraryCount = live.libraryCount;
  const hasLibraryCount = libraryCount != null;
  const sourceLabel =
    live.deckSource === "submitted"
      ? `Game ${Math.max(live.gameNumber, 1)} submitted deck`
      : live.deckSource === "linked"
        ? "Linked deck estimate"
        : "Deck submission unavailable";

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
        <ul className="overlay-card-list">
          {cards.map((card: LiveDeckCard) => {
            const name = cardName(card);
            const remainingLabel = card.remaining == null ? "Unknown" : `${card.remaining} of ${card.quantity}`;
            return (
              <li className={card.remaining === 0 ? "is-empty" : undefined} key={card.cardId}>
                <span className="overlay-card-mark" aria-hidden="true" />
                <div className="overlay-card-name">
                  <CardPreviewName cardId={card.cardId} cardName={card.cardName} label={<span>{name}</span>} />
                </div>
                <span className="overlay-card-count" aria-label={`${remainingLabel} copies left`}>
                  <strong>{card.remaining ?? "—"}</strong>
                  <span>/{card.quantity}</span>
                </span>
              </li>
            );
          })}
        </ul>
      ) : (
        <p className="overlay-empty">Waiting for the submitted decklist.</p>
      )}
      <footer className="overlay-panel-foot">
        <span className={`overlay-state-dot ${hasLibraryCount ? "is-ready" : ""}`} aria-hidden="true" />
        {hasLibraryCount ? `${live.deckTotal - libraryCount} known outside the library` : "Waiting for full game state"}
      </footer>
    </aside>
  );
}

function OpponentPanel({ live }: { live: LiveMatch }) {
  const cards = useMemo(
    () => [...live.opponentObservedCards].sort(compareCardNames),
    [live.opponentObservedCards],
  );
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
          {cards.map((card: OpponentObservedCard) => {
            const name = cardName(card);
            return (
              <li key={card.cardId}>
                <span className="overlay-card-mark" aria-hidden="true" />
                <div className="overlay-card-name">
                  <CardPreviewName cardId={card.cardId} cardName={card.cardName} label={<span>{name}</span>} />
                </div>
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
    <main className="overlay-hud" aria-label="Ponder game overlay">
      <DeckPanel live={live} />
      <OpponentPanel live={live} />
    </main>
  );
}
