import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";

import { EventLabel } from "../components/EventLabel";
import { StatusMessage } from "../components/StatusMessage";
import { api } from "../lib/api";
import { formatGameFormat, pct } from "../lib/format";
import { useEventSets, type SetLookup } from "../lib/useEventSets";
import { useRowLink } from "../lib/useRowLink";
import type { DeckSummary } from "../lib/types";

type WinRateTone = "positive" | "negative" | "neutral";

function toneForWinRate(rate: number): WinRateTone {
  const displayed = Number((rate * 100).toFixed(1));
  return displayed > 50 ? "positive" : displayed < 50 ? "negative" : "neutral";
}

function DeckRow({ deck, setLookup }: { deck: DeckSummary; setLookup: SetLookup }) {
  const rowLink = useRowLink(`/decks/${deck.deckId}`);
  return (
    <tr {...rowLink}>
      <td>
        <Link to={`/decks/${deck.deckId}`} className="text-link">
          {deck.deckName || `Deck ${deck.deckId}`}
        </Link>
      </td>
      <td>{formatGameFormat(deck.format)}</td>
      <td>
        <EventLabel eventName={deck.eventName} lookup={setLookup} />
      </td>
      <td>{deck.matches}</td>
      <td>{deck.wins}</td>
      <td>{deck.losses}</td>
      <td>
        <strong className={`win-rate win-rate--${toneForWinRate(deck.winRate)}`}>
          {pct(deck.winRate)}
        </strong>
      </td>
    </tr>
  );
}

export function DecksPage() {
  const { data, isLoading, error } = useQuery({
    queryKey: ["decks"],
    queryFn: () => api.decks(),
  });
  const { lookup: setLookup } = useEventSets((data ?? []).map((deck) => deck.eventName));

  if (isLoading) return <StatusMessage>Loading decks…</StatusMessage>;
  if (error) return <StatusMessage tone="error">{(error as Error).message}</StatusMessage>;

  return (
    <section className="panel">
      <div className="panel-head">
        <h3>Decks</h3>
        <p>{data?.length ?? 0} decks</p>
      </div>

      <div className="table-wrap">
        <table className="data-table">
          <thead>
            <tr>
              <th>Deck</th>
              <th>Format</th>
              <th>Event</th>
              <th>Matches</th>
              <th>Wins</th>
              <th>Losses</th>
              <th>Win Rate</th>
            </tr>
          </thead>
          <tbody>
            {(data ?? []).map((deck) => (
              <DeckRow key={deck.deckId} deck={deck} setLookup={setLookup} />
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}
