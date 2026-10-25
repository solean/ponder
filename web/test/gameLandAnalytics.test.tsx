import { expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { GameLandAnalytics } from "../src/components/match-detail/GameLandAnalytics";
import type { GameAnalytics, GameLandTurn } from "../src/lib/types";
const turn = (values: Partial<GameLandTurn>): GameLandTurn => ({ turnNumber: 1, landsDrawn: 0, spellsDrawn: 0, unknownDrawn: 0, landsPlayed: 0, landDropStatus: "unobserved", ...values });
const game = (turns?: GameLandTurn[]) => ({ gameNumber: 1, flags: [{ flag: "missed_land_drop", detail: "No land played while holding one" }], landAnalytics: turns ? { turns } : undefined }) as GameAnalytics;

test("land analytics separates recorded draws from unknown types and played cards", () => {
  const html = renderToStaticMarkup(<GameLandAnalytics game={game([
    turn({ landsDrawn: 2, spellsDrawn: 3, unknownDrawn: 1, landsPlayed: 1, landDropStatus: "land_played", isPlayerTurn: true }),
    turn({ turnNumber: 3, landDropStatus: "no_land_available", isPlayerTurn: true }),
    turn({ turnNumber: 5, landDropStatus: "incomplete", isPlayerTurn: true }),
  ])} />);
  expect(html).toContain("2 lands drawn · 3 spells drawn · 1 draws with unknown type");
  expect(html).toContain("Missed land drops with no land available: 1 (2 own turns assessed)");
  expect(html).toContain("Incomplete turn");
  expect(html).toContain("Missed · no land available");
  expect(html).not.toContain("holding one");
  expect(html).toContain("opening hands, mulligans and other hand arrivals are excluded");
});

test("missing and incomplete replay coverage never read as zero missed drops", () => {
  expect(renderToStaticMarkup(<GameLandAnalytics game={game()} />)).toContain("Replay data is unavailable");
  const html = renderToStaticMarkup(<GameLandAnalytics game={game([turn({ landDropStatus: "incomplete" })])} />);
  expect(html).toContain("No complete own turns with enough information");
  expect(html).not.toContain("Missed land drops with no land available: 0");
});
