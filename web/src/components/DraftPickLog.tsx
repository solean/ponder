import { useId, useMemo } from "react";

import { CardPreviewName } from "./CardPreviewName";
import { StatusMessage } from "./StatusMessage";
import { draftPickLogPacks } from "../lib/draftReport";
import type { DraftPick, DraftPickCard } from "../lib/types";

function DraftCardList({ cards }: { cards: DraftPickCard[] }) {
  if (cards.length === 0) {
    return <span className="draft-card-empty">Selection unavailable</span>;
  }

  return (
    <div className="draft-card-list">
      {cards.map((card, index) => (
        <CardPreviewName
          cardId={card.cardId}
          cardName={card.cardName}
          key={`${card.cardId}-${index}`}
          resolveName
        />
      ))}
    </div>
  );
}

export function DraftPickLog({ picks }: { picks: DraftPick[] }) {
  const headingID = useId();
  const packs = useMemo(() => draftPickLogPacks(picks), [picks]);

  return (
    <section className="panel draft-pick-log" aria-labelledby={headingID}>
      <div className="panel-head">
        <div>
          <h3 id={headingID}>Pick Log</h3>
          <p>Every recorded selection, grouped by pack</p>
        </div>
      </div>

      {packs.length === 0 ? (
        <StatusMessage>No picks were recorded for this draft.</StatusMessage>
      ) : (
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
      )}
    </section>
  );
}
