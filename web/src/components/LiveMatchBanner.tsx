import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";

import { api } from "../lib/api";
import { formatOdds, pNextDraw, pWithin } from "../lib/drawOdds";
import { useEventSets } from "../lib/useEventSets";
import { CardPreviewName } from "./CardPreviewName";
import { EventLabel } from "./EventLabel";

const TOP_CARDS = 8;
const MAX_REVEALED = 12;
const MINIMIZED_STORAGE_KEY = "mtgdata.liveBannerMinimized";

function readStoredMinimized(): boolean {
  if (typeof window === "undefined") return false;
  try {
    return window.localStorage.getItem(MINIMIZED_STORAGE_KEY) === "true";
  } catch {
    return false;
  }
}

function cardLabel(quantity: number, name: string | undefined, cardId: number): string {
  const prefix = quantity > 1 ? `${quantity}× ` : "";
  return `${prefix}${name || `#${cardId}`}`;
}

/**
 * Global "now playing" banner. Polls /api/live (fast while a match is live, slow
 * when idle, paused when the tab is hidden) and renders the opponent, revealed
 * cards, your deck, game/turn state, and estimated draw odds. Renders nothing
 * when no match is in progress.
 */
export function LiveMatchBanner() {
  const queryClient = useQueryClient();
  const { data } = useQuery({
    queryKey: ["live"],
    queryFn: api.live,
    refetchInterval: (query) => (query.state.data?.live ? 2000 : 5000),
    refetchIntervalInBackground: false,
  });

  const live = data?.live ?? null;
  const liveMatchId = live?.match.id ?? null;

  // When a match starts or ends, nudge the rest of the app to refresh so the
  // Matches/Overview views reflect it without a manual reload.
  const previousMatchId = useRef<number | null>(null);
  useEffect(() => {
    if (previousMatchId.current !== liveMatchId) {
      previousMatchId.current = liveMatchId;
      queryClient.invalidateQueries({ queryKey: ["matches"] });
      queryClient.invalidateQueries({ queryKey: ["overview"] });
    }
  }, [liveMatchId, queryClient]);

  const { lookup } = useEventSets(live ? [live.match.eventName] : []);

  const [minimized, setMinimized] = useState(readStoredMinimized);
  useEffect(() => {
    try {
      window.localStorage.setItem(MINIMIZED_STORAGE_KEY, String(minimized));
    } catch {
      // Ignore storage failures; the in-memory state still works.
    }
  }, [minimized]);

  if (!live) return null;

  const { match, libraryEstimate } = live;
  // Default to empty arrays: early in a game there are no revealed cards yet,
  // and a match may have no decklist linked.
  const opponentObservedCards = live.opponentObservedCards ?? [];
  const deck = live.deck ?? [];
  const landCount = live.landCount ?? 0;
  const topCards = [...deck].sort((a, b) => b.quantity - a.quantity).slice(0, TOP_CARDS);

  return (
    <section className={`live-banner ${minimized ? "is-minimized" : ""}`} aria-label="Live match">
      <div
        className="live-banner-head"
        role="button"
        tabIndex={0}
        aria-expanded={!minimized}
        aria-label={minimized ? "Expand live match banner" : "Minimize live match banner"}
        title={minimized ? "Expand" : "Minimize"}
        onClick={() => setMinimized((value) => !value)}
        onKeyDown={(event) => {
          if (event.key === "Enter" || event.key === " ") {
            event.preventDefault();
            setMinimized((value) => !value);
          }
        }}
      >
        <span className="live-badge">Live</span>
        <span className="live-vs">
          vs <strong>{match.opponent || "Unknown"}</strong>
        </span>
        <EventLabel eventName={match.eventName} lookup={lookup} />
        <span className="live-progress">
          Game {Math.max(live.gameNumber, 1)} · Turn {Math.max(live.turnNumber, 0)}
        </span>
        {match.deckName ? <span className="live-deck">{match.deckName}</span> : null}
        <span className="live-minimize" aria-hidden="true">
          {minimized ? "+" : "–"}
        </span>
      </div>

      {minimized ? null : (
      <div className="live-banner-body">
        <div className="live-col">
          <h4>Opponent revealed</h4>
          {opponentObservedCards.length === 0 ? (
            <p className="live-empty">Nothing revealed yet</p>
          ) : (
            <ul className="live-card-list">
              {opponentObservedCards.slice(0, MAX_REVEALED).map((card) => (
                <li key={card.cardId}>
                  <CardPreviewName
                    cardId={card.cardId}
                    cardName={card.cardName}
                    label={cardLabel(card.quantity, card.cardName, card.cardId)}
                  />
                </li>
              ))}
            </ul>
          )}
        </div>

        <div className="live-col">
          <h4>
            Draw odds <span className="live-est">(est.)</span>
          </h4>
          {deck.length === 0 ? (
            <p className="live-empty">No decklist linked</p>
          ) : (
            <>
              <p className="live-library">≈ {libraryEstimate} cards left in library</p>
              <table className="live-odds">
                <thead>
                  <tr>
                    <th>Card</th>
                    <th>Next</th>
                    <th>+3</th>
                  </tr>
                </thead>
                <tbody>
                  {landCount > 0 ? (
                    <tr className="live-odds-land">
                      <td>Any land ({landCount})</td>
                      <td>{formatOdds(pNextDraw(landCount, libraryEstimate))}</td>
                      <td>{formatOdds(pWithin(landCount, libraryEstimate, 3))}</td>
                    </tr>
                  ) : null}
                  {topCards.map((card) => (
                    <tr key={card.cardId}>
                      <td>
                        <CardPreviewName
                          cardId={card.cardId}
                          cardName={card.cardName}
                          label={cardLabel(card.quantity, card.cardName, card.cardId)}
                        />
                      </td>
                      <td>{formatOdds(pNextDraw(card.quantity, libraryEstimate))}</td>
                      <td>{formatOdds(pWithin(card.quantity, libraryEstimate, 3))}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </>
          )}
        </div>
      </div>
      )}
    </section>
  );
}
