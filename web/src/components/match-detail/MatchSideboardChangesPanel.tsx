import { sideboardChangeCardTotal } from "../../lib/replay";
import type {
  GameSideboardChanges,
  SideboardChangeCard,
} from "../../lib/types";
import { CardPreviewName } from "./CardPreviews";

export function MatchSideboardChangesPanel({
  gameNumber,
  changes,
}: {
  gameNumber: number;
  changes?: GameSideboardChanges;
}) {
  if (gameNumber <= 1) return null;

  const cardsIn = changes ? sideboardChangeCardTotal(changes.cardsIn) : 0;
  const cardsOut = changes ? sideboardChangeCardTotal(changes.cardsOut) : 0;
  const hasChanges = cardsIn > 0 || cardsOut > 0;

  const renderCardGroup = (
    label: "Cards in" | "Cards out",
    cards: SideboardChangeCard[],
    total: number,
  ) => (
    <section
      className={`match-sideboard-change-group is-${label === "Cards in" ? "in" : "out"}`}
      aria-label={`${label}, ${total} card${total === 1 ? "" : "s"}`}
    >
      <div className="match-sideboard-change-group-head">
        <h5>{label}</h5>
        <span>
          {total} card{total === 1 ? "" : "s"}
        </span>
      </div>
      <ul>
        {cards.map((card) => (
          <li key={card.cardId}>
            <span className="match-sideboard-change-quantity">
              {card.quantity}×
            </span>
            <CardPreviewName card={card} />
          </li>
        ))}
      </ul>
    </section>
  );

  return (
    <section
      className={`match-sideboard-changes ${changes ? "is-captured" : "is-unavailable"}`}
      aria-label={`Your sideboard changes for game ${gameNumber}`}
    >
      <div className="match-sideboard-changes-head">
        <div>
          <p className="match-sideboard-changes-label">Your sideboard</p>
          <h4>Changes for Game {gameNumber}</h4>
        </div>
        <p className="match-sideboard-changes-transition">
          Game {gameNumber - 1} → Game {gameNumber}
        </p>
      </div>

      {!changes ? (
        <p className="match-sideboard-changes-empty">
          Sideboard data was not captured for this game.
        </p>
      ) : !hasChanges ? (
        <p className="match-sideboard-changes-empty is-no-change">
          No sideboard changes were made.
        </p>
      ) : (
        <div className="match-sideboard-change-grid">
          {renderCardGroup("Cards in", changes.cardsIn, cardsIn)}
          {renderCardGroup("Cards out", changes.cardsOut, cardsOut)}
        </div>
      )}
    </section>
  );
}
