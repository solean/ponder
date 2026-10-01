import { formatDateTime, formatDuration } from "../../lib/format";
import type { Match } from "../../lib/types";
import type { SetLookup } from "../../lib/useEventSets";
import { ContextualLink } from "../Breadcrumbs";
import { EventLabel } from "../EventLabel";
import { MatchDeckColors } from "../MatchDeckColors";
import { ResultPill } from "../ResultPill";

export function MatchOverviewPanel({
  match,
  setLookup,
}: {
  match: Match;
  setLookup: SetLookup;
}) {
  return (
    <section className="panel match-detail-overview-panel">
      <div className="panel-head">
        <h3>Match #{match.id}</h3>
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
              <ContextualLink
                className="text-link"
                to={`/decks/${match.deckId}`}
              >
                {match.deckName || `Deck ${match.deckId}`}
                {match.deckVersionNumber
                  ? ` · v${match.deckVersionNumber}`
                  : ""}
              </ContextualLink>
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
  );
}
