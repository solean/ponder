import type { ReactNode } from "react";

import { formatDateTime, formatDuration } from "../../lib/format";
import type { Match } from "../../lib/types";
import type { SetLookup } from "../../lib/useEventSets";
import { ContextualLink } from "../Breadcrumbs";
import { EventLabel } from "../EventLabel";
import { MatchDeckColors } from "../MatchDeckColors";
import { ResultPill } from "../ResultPill";
import { MatchTagsControl } from "../MatchTags";
import { SaveMatchButton } from "../SaveMatchButton";

/** One-line match header: result, opponent, the facts, then the section tabs. */
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
  return (
    <header className="match-detail-header">
      <div className="match-detail-header-title">
        <ResultPill result={match.result} />
        <h2>vs {match.opponent || "Unknown opponent"}</h2>
        <SaveMatchButton matchId={match.id} saved={match.saved ?? false} />
        <MatchDeckColors
          className="match-deck-colors-detail"
          deckColors={match.deckColors}
          deckColorsKnown={match.deckColorsKnown}
          opponentDeckColors={match.opponentDeckColors}
          opponentDeckColorsKnown={match.opponentDeckColorsKnown}
        />
      </div>
      <ul className="match-detail-header-meta" aria-label="Match overview">
        <li>
          <EventLabel eventName={match.eventName} lookup={setLookup} />
        </li>
        {match.deckId ? (
          <li>
            <ContextualLink className="text-link" to={`/decks/${match.deckId}`}>
              {match.deckName || `Deck ${match.deckId}`}
              {match.deckVersionNumber ? ` · v${match.deckVersionNumber}` : ""}
            </ContextualLink>
          </li>
        ) : null}
        <li>{formatDateTime(match.startedAt)}</li>
        {match.turnCount ? <li>{match.turnCount} turns</li> : null}
        {match.secondsCount ? <li>{formatDuration(match.secondsCount)}</li> : null}
        {match.winReason ? <li>{match.winReason}</li> : null}
      </ul>
      <MatchTagsControl matchId={match.id} tags={match.tags ?? []} className="match-detail-tags" />
      {children}
    </header>
  );
}
