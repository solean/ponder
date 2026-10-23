import type { GameAnalytics, GameLandTurn } from "../../lib/types";
import { arenaTurnToFullTurn } from "../../lib/turns";

const availabilityLabels: Record<GameLandTurn["landDropStatus"], string> = {
  no_land_available: "Missed · no land available",
  land_played: "Land played",
  land_available: "Land available",
  opponent_turn: "—",
  incomplete: "Incomplete turn",
  unobserved: "Insufficient data",
};

export function GameLandAnalytics({ game }: { game: GameAnalytics }) {
  const turns = game.landAnalytics?.turns ?? [];
  const totals = turns.reduce((sum, turn) => ({
    lands: sum.lands + turn.landsDrawn,
    spells: sum.spells + turn.spellsDrawn,
    unknown: sum.unknown + turn.unknownDrawn,
    misses: sum.misses + Number(turn.landDropStatus === "no_land_available"),
    judged: sum.judged + Number(
      ["no_land_available", "land_played", "land_available"].includes(turn.landDropStatus),
    ),
  }), { lands: 0, spells: 0, unknown: 0, misses: 0, judged: 0 });

  return (
    <section className="game-land-analytics" aria-label={`Land analytics for game ${game.gameNumber}`}>
      <h4>Draws and land availability</h4>
      {turns.length === 0 ? (
        <p className="match-game-empty">Replay data is unavailable for land analytics.</p>
      ) : (
        <>
          <p>
            Recorded draws: {totals.lands} lands drawn · {totals.spells} spells drawn
            {totals.unknown > 0 ? ` · ${totals.unknown} draws with unknown type` : ""}
          </p>
          <p>
            {totals.judged > 0
              ? `Missed land drops with no land available: ${totals.misses} (${totals.judged} own turns assessed).`
              : "No complete own turns with enough information to assess land availability."}
          </p>
          <div className="game-land-table-wrap">
            <table className="game-land-table">
              <thead>
                <tr>
                  <th>Turn</th><th>Lands drawn</th><th>Spells drawn</th>
                  <th>Unknown type</th><th>Land availability</th>
                </tr>
              </thead>
              <tbody>
                {turns.map(turn => (
                  <tr key={turn.turnNumber}>
                    <th scope="row">
                      {arenaTurnToFullTurn(turn.turnNumber)}
                      {turn.isPlayerTurn === true ? " · You" : turn.isPlayerTurn === false ? " · Opponent" : ""}
                    </th>
                    <td>{turn.landsDrawn}</td><td>{turn.spellsDrawn}</td>
                    <td>{turn.unknownDrawn || "—"}</td>
                    <td>{availabilityLabels[turn.landDropStatus]}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}
      <p className="analytics-method-note">
        Draw counts include only logged gameplay draws; opening hands, mulligans and other hand arrivals are excluded.
        Unknown card types stay separate. Missed drops require a complete own turn, a recorded main phase and
        no land available in the observed hand. Partial turns, replay gaps and missing ownership or card types
        are excluded. Empty-hand snapshots without membership evidence are also excluded.
      </p>
    </section>
  );
}
