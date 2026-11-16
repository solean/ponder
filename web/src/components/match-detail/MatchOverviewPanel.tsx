import type { ReactNode } from "react";

import { formatDateTime, formatDuration, formatShortDateTime } from "../../lib/format";
import type { Match } from "../../lib/types";
import type { SetLookup } from "../../lib/useEventSets";
import { ContextualLink } from "../Breadcrumbs";
import { EventLabel } from "../EventLabel";
import { DeckColorIdentity } from "../MatchDeckColors";
import { ResultPill } from "../ResultPill";
import { MatchTagsControl } from "../MatchTags";
import { SaveMatchButton } from "../SaveMatchButton";

/** How the match ended, when that says more than "the game was played out". */
function endReasonLabel(reason?: string | null): string | null {
  switch (reason) {
    case "Concede":
      return "Conceded";
    case "Timeout":
      return "Timed out";
    case "Game":
    case "":
    case null:
    case undefined:
      return null;
    default:
      return reason;
  }
}

/** One-line match header: result, opponent, actions, the facts, then the section tabs. */
export function MatchOverviewPanel({
  match,
  setLookup,
  children,
}: {
  match: Match;
  setLookup: SetLookup;
  /** Section tabs, shown at the right end of the header. */
  children?: ReactNode;
}) {
  const endReason = endReasonLabel(match.winReason);
  const deckLabel = `${match.deckName || `Deck ${match.deckId}`}${
    match.deckVersionNumber ? ` v${match.deckVersionNumber}` : ""
  }`;
  const showColors = match.deckColorsKnown || match.opponentDeckColorsKnown;
  return (
    <header className="match-detail-header">
      <div className="match-detail-header-title">
        <ResultPill result={match.result} />
        <h2>vs {match.opponent || "Unknown opponent"}</h2>
        <SaveMatchButton matchId={match.id} saved={match.saved ?? false} />
        <MatchTagsControl matchId={match.id} tags={match.tags ?? []} className="match-detail-tags" />
      </div>
      <ul className="match-detail-header-meta" aria-label="Match overview">
        {showColors ? (
          <li className="match-detail-header-colors">
            <span className="visually-hidden">Your colors: </span>
            <DeckColorIdentity colors={match.deckColors} known={match.deckColorsKnown} />
            <span className="match-detail-header-vs">vs</span>
            <span className="visually-hidden">opponent colors: </span>
            <DeckColorIdentity colors={match.opponentDeckColors} known={match.opponentDeckColorsKnown} />
          </li>
        ) : null}
        <li className="is-shrinkable">
          <EventLabel eventName={match.eventName} lookup={setLookup} />
        </li>
        {match.deckId ? (
          <li className="is-shrinkable">
            <ContextualLink className="match-detail-header-link" to={`/decks/${match.deckId}`} title={deckLabel}>
              {deckLabel}
            </ContextualLink>
          </li>
        ) : null}
        <li title={formatDateTime(match.startedAt)}>{formatShortDateTime(match.startedAt)}</li>
        {match.turnCount ? <li>{match.turnCount} turns</li> : null}
        {match.secondsCount ? <li>{formatDuration(match.secondsCount)}</li> : null}
        {endReason ? <li>{endReason}</li> : null}
      </ul>
      {children}
    </header>
  );
}
