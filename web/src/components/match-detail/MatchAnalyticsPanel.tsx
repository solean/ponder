import { useQueries } from "@tanstack/react-query";
import { useMemo } from "react";
import { cardDisplayName, cardFallbackHref } from "../../lib/replay";
import type { CardPreview } from "../../lib/scryfall";
import { fetchCardPreview } from "../../lib/scryfall";
import { arenaTurnToFullTurn } from "../../lib/turns";
import type {
  GameAnalytics,
  GameTurnStat,
  OpeningHandCard,
} from "../../lib/types";
import { ResultPill } from "../ResultPill";
import { StatusMessage } from "../StatusMessage";
import { cardPreviewQueryKey } from "../../lib/replay/cardPreviewQuery";

function confidenceLabel(confidence: string): string {
  switch (confidence) {
    case "exact":
      return "Logged";
    case "derived":
      return "Inferred";
    default:
      return "Unavailable";
  }
}

const SHAPE_CHART_LEFT = 30;

const SHAPE_CHART_TURN_W = 26;

const SHAPE_LIFE_TOP = 8;

const SHAPE_LIFE_H = 78;

const SHAPE_BARS_TOP = 96;

const SHAPE_BARS_H = 30;

const SHAPE_AXIS_Y = 138;

const SHAPE_CHART_H = 148;

type FullTurnShapeStat = Omit<GameTurnStat, "isPlayerTurn"> & {
  includesPlayerTurn: boolean;
  includesOpponentTurn: boolean;
};

function aggregateGameTurnStats(stats: GameTurnStat[]): FullTurnShapeStat[] {
  const byFullTurn = new Map<number, FullTurnShapeStat>();

  for (const stat of [...stats].sort(
    (left, right) => left.turnNumber - right.turnNumber,
  )) {
    const turnNumber = arenaTurnToFullTurn(stat.turnNumber);
    if (turnNumber <= 0) {
      continue;
    }

    const aggregate = byFullTurn.get(turnNumber) ?? {
      turnNumber,
      landsPlayed: 0,
      spellsCast: 0,
      includesPlayerTurn: false,
      includesOpponentTurn: false,
    };

    aggregate.landsPlayed += stat.landsPlayed;
    aggregate.spellsCast += stat.spellsCast;
    if (stat.isPlayerTurn === true) {
      aggregate.includesPlayerTurn = true;
    } else if (stat.isPlayerTurn === false) {
      aggregate.includesOpponentTurn = true;
    }

    // Each raw half-turn stores its final observed snapshot. Walking them in
    // Arena order lets the full turn carry the latest value that was actually
    // observed without replacing it with a missing value.
    if (stat.selfLife != null) {
      aggregate.selfLife = stat.selfLife;
    }
    if (stat.opponentLife != null) {
      aggregate.opponentLife = stat.opponentLife;
    }
    if (stat.selfHandSize != null) {
      aggregate.selfHandSize = stat.selfHandSize;
    }
    if (stat.landInHand != null) {
      aggregate.landInHand = stat.landInHand;
    }

    byFullTurn.set(turnNumber, aggregate);
  }

  return [...byFullTurn.values()].sort(
    (left, right) => left.turnNumber - right.turnNumber,
  );
}

function shapeTurnSummary(stat: FullTurnShapeStat, missed: boolean): string {
  const ownership =
    stat.includesPlayerTurn && stat.includesOpponentTurn
      ? " (you + opponent)"
      : stat.includesPlayerTurn
        ? " (you)"
        : stat.includesOpponentTurn
          ? " (opponent)"
          : "";
  const parts = [`Turn ${stat.turnNumber}${ownership}`];
  if (stat.selfLife != null || stat.opponentLife != null) {
    parts.push(`life ${stat.selfLife ?? "?"} vs ${stat.opponentLife ?? "?"}`);
  }
  parts.push(
    `${stat.landsPlayed} land${stat.landsPlayed === 1 ? "" : "s"}, ${stat.spellsCast} spell${stat.spellsCast === 1 ? "" : "s"}`,
  );
  if (stat.selfHandSize != null) {
    parts.push(`${stat.selfHandSize} in hand`);
  }
  if (missed) {
    parts.push("no land played while holding one (heuristic)");
  }
  return parts.join(" · ");
}

function GameShapeChart({ game }: { game: GameAnalytics }) {
  const stats = aggregateGameTurnStats(game.turnStats);
  const maxTurn = stats.reduce(
    (max, stat) => Math.max(max, stat.turnNumber),
    0,
  );
  if (maxTurn === 0) {
    return null;
  }
  const byTurn = new Map(stats.map((stat) => [stat.turnNumber, stat]));
  const missedTurns = new Set(
    game.flags
      .filter(
        (flag) => flag.flag === "missed_land_drop" && flag.turnNumber != null,
      )
      .map((flag) => arenaTurnToFullTurn(flag.turnNumber as number)),
  );

  const viewW = SHAPE_CHART_LEFT + maxTurn * SHAPE_CHART_TURN_W + 8;
  const xOf = (turn: number) =>
    SHAPE_CHART_LEFT + (turn - 0.5) * SHAPE_CHART_TURN_W;

  const lifeMax = Math.max(
    20,
    ...stats.flatMap((stat) => [stat.selfLife ?? 0, stat.opponentLife ?? 0]),
  );
  const yOfLife = (value: number) =>
    SHAPE_LIFE_TOP + (1 - value / lifeMax) * SHAPE_LIFE_H;
  const lifePoints = (side: "selfLife" | "opponentLife") =>
    stats
      .filter((stat) => stat[side] != null)
      .map(
        (stat) =>
          `${xOf(stat.turnNumber).toFixed(1)},${yOfLife(stat[side] as number).toFixed(1)}`,
      )
      .join(" ");

  const maxPlays = Math.max(
    1,
    ...stats.map((stat) => Math.max(stat.landsPlayed, stat.spellsCast)),
  );
  const barH = (count: number) => (count / maxPlays) * SHAPE_BARS_H;
  const turnLabelStep = maxTurn > 14 ? 2 : 1;

  return (
    <figure className="game-shape">
      <svg
        className="game-shape-chart"
        viewBox={`0 0 ${viewW} ${SHAPE_CHART_H}`}
        style={{ maxWidth: `${viewW * 2}px` }}
        role="img"
        aria-label={`Turn-by-turn life totals, land drops, and spells cast for game ${game.gameNumber}`}
      >
        <line
          className="game-shape-axis"
          x1={SHAPE_CHART_LEFT - 6}
          x2={viewW - 4}
          y1={SHAPE_LIFE_TOP + SHAPE_LIFE_H}
          y2={SHAPE_LIFE_TOP + SHAPE_LIFE_H}
        />
        <text
          className="game-shape-y-label"
          x={SHAPE_CHART_LEFT - 10}
          y={yOfLife(lifeMax) + 3}
        >
          {lifeMax}
        </text>
        <text
          className="game-shape-y-label"
          x={SHAPE_CHART_LEFT - 10}
          y={SHAPE_LIFE_TOP + SHAPE_LIFE_H + 3}
        >
          0
        </text>
        <polyline
          className="game-shape-life is-opponent"
          points={lifePoints("opponentLife")}
        />
        <polyline
          className="game-shape-life is-self"
          points={lifePoints("selfLife")}
        />
        {Array.from({ length: maxTurn }, (_, i) => i + 1).map((turn) => {
          const stat = byTurn.get(turn);
          const missed = missedTurns.has(turn);
          const x = xOf(turn);
          return (
            <g key={turn} className="game-shape-turn">
              <title>
                {stat
                  ? shapeTurnSummary(stat, missed)
                  : `Turn ${turn} · no data`}
              </title>
              <rect
                className="game-shape-hover"
                x={x - SHAPE_CHART_TURN_W / 2}
                y={0}
                width={SHAPE_CHART_TURN_W}
                height={SHAPE_CHART_H}
              />
              {stat && stat.selfLife != null ? (
                <circle
                  className="game-shape-dot is-self"
                  cx={x}
                  cy={yOfLife(stat.selfLife)}
                  r={2.4}
                />
              ) : null}
              {stat && stat.opponentLife != null ? (
                <circle
                  className="game-shape-dot is-opponent"
                  cx={x}
                  cy={yOfLife(stat.opponentLife)}
                  r={2.4}
                />
              ) : null}
              {stat && stat.landsPlayed > 0 ? (
                <rect
                  className="game-shape-bar is-land"
                  x={x - 9}
                  y={SHAPE_BARS_TOP + SHAPE_BARS_H - barH(stat.landsPlayed)}
                  width={8}
                  height={barH(stat.landsPlayed)}
                />
              ) : null}
              {stat && stat.spellsCast > 0 ? (
                <rect
                  className="game-shape-bar is-spell"
                  x={x + 1}
                  y={SHAPE_BARS_TOP + SHAPE_BARS_H - barH(stat.spellsCast)}
                  width={8}
                  height={barH(stat.spellsCast)}
                />
              ) : null}
              {missed ? (
                <path
                  className="game-shape-missed"
                  d={`M ${x - 4} ${SHAPE_AXIS_Y - 8} L ${x + 4} ${SHAPE_AXIS_Y - 8} L ${x} ${SHAPE_AXIS_Y - 2} Z`}
                />
              ) : null}
              {turn % turnLabelStep === 0 ? (
                <text
                  className={`game-shape-turn-label ${stat?.includesPlayerTurn ? "is-own" : ""}`}
                  x={x}
                  y={SHAPE_AXIS_Y + 8}
                >
                  {turn}
                </text>
              ) : null}
            </g>
          );
        })}
      </svg>
      <figcaption className="game-shape-legend">
        <span className="game-shape-legend-item is-self">You</span>
        <span className="game-shape-legend-item is-opponent">Opponent</span>
        <span className="game-shape-legend-item is-land">Lands</span>
        <span className="game-shape-legend-item is-spell">Spells</span>
        {missedTurns.size > 0 ? (
          <span className="game-shape-legend-item is-missed">
            Possible missed land drop
          </span>
        ) : null}
        <span className="game-shape-legend-note">
          Turns that include one of yours are numbered in bold.
        </span>
      </figcaption>
    </figure>
  );
}

function OpeningHandCardImage({
  card,
  preview,
  disposition,
}: {
  card: OpeningHandCard;
  preview: CardPreview | null;
  disposition: "kept" | "bottomed" | "returned" | "undecided";
}) {
  const name = preview?.name ?? cardDisplayName(card);
  const href = preview?.scryfallUrl ?? cardFallbackHref(card);

  return (
    <a
      className="opening-hand-card"
      href={href}
      target="_blank"
      rel="noreferrer"
      aria-label={`${name}, ${disposition}. Open on Scryfall`}
      title={`${name} • ${disposition}`}
    >
      {preview ? (
        <img
          src={preview.imageUrl}
          alt=""
          loading="lazy"
          decoding="async"
          width={488}
          height={680}
        />
      ) : (
        <span className="opening-hand-card-fallback" aria-hidden="true" />
      )}
    </a>
  );
}

export function MatchAnalyticsPanel({ games }: { games: GameAnalytics[] }) {
  const openingHandPreviewCards = useMemo<OpeningHandCard[]>(() => {
    const uniqueCards = new Map<number, OpeningHandCard>();
    for (const game of games) {
      for (const hand of game.openingHands) {
        for (const card of hand.cards) {
          if (!uniqueCards.has(card.cardId)) {
            uniqueCards.set(card.cardId, card);
          }
        }
      }
    }
    return Array.from(uniqueCards.values());
  }, [games]);
  const openingHandPreviewQueries = useQueries({
    queries: openingHandPreviewCards.map((card) => ({
      queryKey: cardPreviewQueryKey(card),
      queryFn: () => fetchCardPreview(card.cardId, card.cardName),
      enabled: card.cardId > 0,
      staleTime: 1000 * 60 * 60 * 24,
      gcTime: 1000 * 60 * 60 * 24,
      retry: 1,
    })),
  });
  const openingHandPreviewByCardID = useMemo(() => {
    const previews = new Map<number, CardPreview | null>();
    for (let index = 0; index < openingHandPreviewCards.length; index += 1) {
      previews.set(
        openingHandPreviewCards[index].cardId,
        openingHandPreviewQueries[index]?.data ?? null,
      );
    }
    return previews;
  }, [openingHandPreviewCards, openingHandPreviewQueries]);

  return (
    <section className="panel match-analytics-panel">
      {games.length === 0 ? (
        <StatusMessage>
          Arena did not leave enough replay or card-play data to reconstruct
          individual games.
        </StatusMessage>
      ) : (
        <div className="match-game-list">
          {games.map((game) => (
            <article className="match-game-card" key={game.id}>
              <header className="match-game-card-head">
                <div>
                  <p className="match-game-eyebrow">
                    Game {game.gameNumber.toLocaleString()}
                  </p>
                  <div className="match-game-result-line">
                    <ResultPill result={game.result} />
                    {game.winReason ? <span>{game.winReason}</span> : null}
                  </div>
                </div>
                <span
                  className={`analytics-confidence is-${game.resultConfidence}`}
                >
                  Result: {confidenceLabel(game.resultConfidence)}
                </span>
              </header>

              <dl
                className="match-game-stats"
                aria-label={`Game ${game.gameNumber} summary`}
              >
                <div>
                  <dt>Initiative</dt>
                  <dd>
                    {game.playDraw ? `On the ${game.playDraw}` : "Unknown"}
                  </dd>
                </div>
                <div>
                  <dt>Turns</dt>
                  <dd>{game.turnCount?.toLocaleString() ?? "—"}</dd>
                </div>
                <div>
                  <dt>Mulligans</dt>
                  <dd>{game.mulliganCount?.toLocaleString() ?? "—"}</dd>
                </div>
                <div>
                  <dt>Kept</dt>
                  <dd>
                    {game.keptHandSize != null
                      ? `${game.keptHandSize.toLocaleString()} cards`
                      : "—"}
                  </dd>
                </div>
                <div>
                  <dt>Life</dt>
                  <dd>
                    {game.openingLifeTotal != null ||
                    game.endingLifeTotal != null
                      ? `${game.openingLifeTotal ?? "?"} → ${game.endingLifeTotal ?? "?"}`
                      : "—"}
                    {game.minSelfLife != null &&
                    game.minSelfLife <
                      Math.min(
                        game.openingLifeTotal ?? Infinity,
                        game.endingLifeTotal ?? Infinity,
                      )
                      ? ` (low ${game.minSelfLife})`
                      : ""}
                  </dd>
                </div>
              </dl>

              {game.turnStats.length > 0 ? (
                <GameShapeChart game={game} />
              ) : null}
              {game.flags.length > 0 ? (
                <ul
                  className="game-shape-flags"
                  aria-label={`Review flags for game ${game.gameNumber}`}
                >
                  {game.flags.map((flag, index) => (
                    <li key={`${flag.flag}-${flag.turnNumber ?? index}`}>
                      <span className="game-shape-flag-turn">
                        {flag.turnNumber != null
                          ? `Turn ${arenaTurnToFullTurn(flag.turnNumber)}`
                          : "Game"}
                      </span>
                      {flag.detail || flag.flag}
                      <small>{flag.confidence}</small>
                    </li>
                  ))}
                </ul>
              ) : null}

              {game.openingHands.length > 0 ? (
                <div className="opening-hand-list">
                  {game.openingHands.map((hand) => (
                    <section className="opening-hand" key={hand.id}>
                      <div className="opening-hand-head">
                        <h4>
                          Hand {hand.attemptNumber.toLocaleString()} ·{" "}
                          {hand.decision === "keep"
                            ? "Kept"
                            : hand.decision === "mulligan"
                              ? "Mulliganed"
                              : "Never decided"}
                        </h4>
                        <span>
                          {hand.offeredHandSize.toLocaleString()} offered
                        </span>
                      </div>
                      <ul
                        className="opening-hand-cards"
                        aria-label={`Cards in opening hand attempt ${hand.attemptNumber}`}
                      >
                        {hand.cards.flatMap((card, cardIndex) =>
                          Array.from(
                            { length: Math.max(0, Math.floor(card.quantity)) },
                            (_, copyIndex) => {
                              const disposition = card.kept
                                ? "kept"
                                : hand.decision === "keep"
                                  ? "bottomed"
                                  : hand.decision === "mulligan"
                                    ? "returned"
                                    : "undecided";
                              return (
                                <li
                                  className={`is-${disposition}`}
                                  key={`${card.cardId}-${cardIndex}-${copyIndex}`}
                                >
                                  <OpeningHandCardImage
                                    card={card}
                                    preview={
                                      openingHandPreviewByCardID.get(
                                        card.cardId,
                                      ) ?? null
                                    }
                                    disposition={disposition}
                                  />
                                </li>
                              );
                            },
                          ),
                        )}
                      </ul>
                    </section>
                  ))}
                </div>
              ) : (
                <p className="match-game-empty">
                  Opening hand was not present in this replay.
                </p>
              )}
            </article>
          ))}
        </div>
      )}

      <p className="analytics-method-note">
        Logged results come directly from GRE game state. Opening-hand
        sequencing and play/draw are inferred from observed replay transitions.
        Turn-by-turn lands and spells classify observed card plays by type line
        and first public zone; missed-land-drop flags are heuristic prompts for
        replay review, not judgments.
      </p>
    </section>
  );
}
