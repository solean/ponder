import { describe, expect, test } from "bun:test";

import {
  buildGraphPoints,
  fillMissingRankClasses,
  rankPromotionsFor,
  rankStepIndex,
  type Ladder,
  type SeasonView,
} from "../src/lib/rankProgress";
import type { RankHistoryPoint, RankState } from "../src/lib/types";

function rankState(values: Partial<Omit<RankState, "rankClass">> & { rankClass?: string } = {}): RankState {
  return {
    seasonOrdinal: values.seasonOrdinal ?? null,
    rankClass: values.rankClass ?? "",
    level: values.level ?? null,
    step: values.step ?? null,
    percentile: values.percentile ?? null,
    leaderboardPlace: values.leaderboardPlace ?? null,
    matchesWon: values.matchesWon ?? null,
    matchesLost: values.matchesLost ?? null,
  };
}

function point(
  matchId: number,
  eventName: string,
  result: RankHistoryPoint["result"],
  constructed: RankState,
  limited: RankState,
): RankHistoryPoint {
  return {
    matchId,
    arenaMatchId: `match-${matchId}`,
    eventName,
    opponent: "Opponent",
    result,
    observedAt: "",
    endedAt: `2026-03-19T00:00:0${matchId}Z`,
    format: "",
    secondsCount: null,
    deckId: null,
    deckName: "",
    constructed,
    limited,
  };
}

function resultsFor(history: RankHistoryPoint[], ladder: Ladder, seasonView?: SeasonView): string[] {
  return (
    buildGraphPoints(history, ladder, seasonView)?.points.map(
      (entry) => `${entry.eventName}:${entry.result}`,
    ) ?? []
  );
}

describe("buildGraphPoints", () => {
  test("excludes limited-only snapshots from the constructed chart", () => {
    const history: RankHistoryPoint[] = [
      point(
        1,
        "Traditional_Ladder",
        "loss",
        rankState({ seasonOrdinal: 12, rankClass: "Silver", level: 4, step: 2, matchesWon: 1, matchesLost: 1 }),
        rankState(),
      ),
      point(
        2,
        "QuickDraft_TST_20260319",
        "win",
        rankState({ seasonOrdinal: 12, rankClass: "Silver", level: 4, step: 2, matchesWon: 1, matchesLost: 1 }),
        rankState({ seasonOrdinal: 12, rankClass: "Bronze", level: 4, step: 1, matchesWon: 1, matchesLost: 0 }),
      ),
      point(
        3,
        "Traditional_Ladder",
        "win",
        rankState({ seasonOrdinal: 12, rankClass: "Silver", level: 4, step: 3, matchesWon: 2, matchesLost: 1 }),
        rankState({ seasonOrdinal: 12, rankClass: "Bronze", level: 4, step: 1, matchesWon: 1, matchesLost: 0 }),
      ),
    ];

    expect(resultsFor(history, "constructed")).toEqual([
      "Traditional_Ladder:loss",
      "Traditional_Ladder:win",
    ]);
  });

  test("excludes constructed-only snapshots from the limited chart", () => {
    const history: RankHistoryPoint[] = [
      point(
        1,
        "QuickDraft_TST_20260319",
        "loss",
        rankState(),
        rankState({ seasonOrdinal: 18, rankClass: "Bronze", level: 4, step: 0, matchesWon: 0, matchesLost: 1 }),
      ),
      point(
        2,
        "Traditional_Ladder",
        "win",
        rankState({ seasonOrdinal: 18, rankClass: "Gold", level: 2, step: 1, matchesWon: 1, matchesLost: 0 }),
        rankState({ seasonOrdinal: 18, rankClass: "Bronze", level: 4, step: 0, matchesWon: 0, matchesLost: 1 }),
      ),
      point(
        3,
        "QuickDraft_TST_20260319",
        "win",
        rankState({ seasonOrdinal: 18, rankClass: "Gold", level: 2, step: 1, matchesWon: 1, matchesLost: 0 }),
        rankState({ seasonOrdinal: 18, rankClass: "Bronze", level: 3, step: 1, matchesWon: 1, matchesLost: 1 }),
      ),
    ];

    expect(resultsFor(history, "limited")).toEqual([
      "QuickDraft_TST_20260319:loss",
      "QuickDraft_TST_20260319:win",
    ]);
  });

  test("returns only the current season by default", () => {
    const history: RankHistoryPoint[] = [
      point(
        1,
        "Traditional_Ladder",
        "win",
        rankState({ seasonOrdinal: 11, rankClass: "Bronze", level: 4, step: 1, matchesWon: 1, matchesLost: 0 }),
        rankState(),
      ),
      point(
        2,
        "Traditional_Ladder",
        "win",
        rankState({ seasonOrdinal: 11, rankClass: "Bronze", level: 3, step: 0, matchesWon: 2, matchesLost: 0 }),
        rankState(),
      ),
      point(
        3,
        "Traditional_Ladder",
        "loss",
        rankState({ seasonOrdinal: 12, rankClass: "Bronze", level: 4, step: 0, matchesWon: 0, matchesLost: 1 }),
        rankState(),
      ),
      point(
        4,
        "Traditional_Ladder",
        "win",
        rankState({ seasonOrdinal: 12, rankClass: "Bronze", level: 3, step: 1, matchesWon: 1, matchesLost: 1 }),
        rankState(),
      ),
    ];

    const series = buildGraphPoints(history, "constructed");

    expect(series?.seasonOrdinal).toBe(12);
    expect(series?.seasonOrdinals).toEqual([11, 12]);
    expect(resultsFor(history, "constructed")).toEqual([
      "Traditional_Ladder:loss",
      "Traditional_Ladder:win",
    ]);
  });

  test("can return the previous season only", () => {
    const history: RankHistoryPoint[] = [
      point(
        1,
        "Traditional_Ladder",
        "win",
        rankState({ seasonOrdinal: 11, rankClass: "Bronze", level: 4, step: 1, matchesWon: 1, matchesLost: 0 }),
        rankState(),
      ),
      point(
        2,
        "Traditional_Ladder",
        "win",
        rankState({ seasonOrdinal: 11, rankClass: "Bronze", level: 3, step: 0, matchesWon: 2, matchesLost: 0 }),
        rankState(),
      ),
      point(
        3,
        "Traditional_Ladder",
        "loss",
        rankState({ seasonOrdinal: 12, rankClass: "Bronze", level: 4, step: 0, matchesWon: 0, matchesLost: 1 }),
        rankState(),
      ),
      point(
        4,
        "Traditional_Ladder",
        "win",
        rankState({ seasonOrdinal: 12, rankClass: "Bronze", level: 3, step: 1, matchesWon: 1, matchesLost: 1 }),
        rankState(),
      ),
    ];

    const series = buildGraphPoints(history, "constructed", "previous");

    expect(series?.seasonOrdinal).toBe(11);
    expect(series?.latestState.matchesWon).toBe(2);
    expect(series?.record).toEqual({ wins: 2, losses: 0 });
    expect(resultsFor(history, "constructed", "previous")).toEqual([
      "Traditional_Ladder:win",
      "Traditional_Ladder:win",
    ]);
  });

  test("can return all seasons on one timeline", () => {
    const history: RankHistoryPoint[] = [
      point(
        1,
        "Traditional_Ladder",
        "win",
        rankState({ seasonOrdinal: 11, rankClass: "Bronze", level: 4, step: 1, matchesWon: 1, matchesLost: 0 }),
        rankState(),
      ),
      point(
        2,
        "Traditional_Ladder",
        "win",
        rankState({ seasonOrdinal: 11, rankClass: "Bronze", level: 3, step: 0, matchesWon: 2, matchesLost: 0 }),
        rankState(),
      ),
      point(
        3,
        "Traditional_Ladder",
        "loss",
        rankState({ seasonOrdinal: 12, rankClass: "Bronze", level: 4, step: 0, matchesWon: 0, matchesLost: 1 }),
        rankState(),
      ),
      point(
        4,
        "Traditional_Ladder",
        "win",
        rankState({ seasonOrdinal: 12, rankClass: "Bronze", level: 3, step: 1, matchesWon: 1, matchesLost: 1 }),
        rankState(),
      ),
    ];

    const series = buildGraphPoints(history, "constructed", "all");

    expect(series?.seasonOrdinal).toBeNull();
    expect(series?.seasonOrdinals).toEqual([11, 12]);
    expect(series?.record).toEqual({ wins: 3, losses: 1 });
    expect(series?.points.map((entry) => entry.seasonOrdinal)).toEqual([11, 11, 12, 12]);
    expect(resultsFor(history, "constructed", "all")).toEqual([
      "Traditional_Ladder:win",
      "Traditional_Ladder:win",
      "Traditional_Ladder:loss",
      "Traditional_Ladder:win",
    ]);
  });

  test("plots Mythic percentile and numbered leaderboard progress", () => {
    const history: RankHistoryPoint[] = [
      point(
        1,
        "Traditional_Ladder",
        "win",
        rankState({
          seasonOrdinal: 91,
          level: 0,
          percentile: 97,
          matchesWon: 1,
          matchesLost: 0,
        }),
        rankState(),
      ),
      point(
        2,
        "Traditional_Ladder",
        "loss",
        rankState({
          seasonOrdinal: 91,
          level: 0,
          percentile: 99,
          matchesWon: 1,
          matchesLost: 1,
        }),
        rankState(),
      ),
      point(
        3,
        "Traditional_Ladder",
        "win",
        rankState({
          seasonOrdinal: 91,
          level: 0,
          percentile: 99,
          leaderboardPlace: 1500,
          matchesWon: 2,
          matchesLost: 1,
        }),
        rankState(),
      ),
      point(
        4,
        "Traditional_Ladder",
        "win",
        rankState({
          seasonOrdinal: 91,
          level: 0,
          percentile: 99,
          leaderboardPlace: 3,
          matchesWon: 3,
          matchesLost: 1,
        }),
        rankState(),
      ),
    ];

    const series = buildGraphPoints(history, "constructed");
    expect(series?.points.map((entry) => entry.rankLabel)).toEqual([
      "Mythic 97%",
      "Mythic 99%",
      "Mythic #1,500",
      "Mythic #3",
    ]);

    const scores = series?.points.map((entry) => entry.score) ?? [];
    expect(scores).toHaveLength(4);
    for (let index = 1; index < scores.length; index += 1) {
      expect(scores[index]).toBeGreaterThan(scores[index - 1]);
    }
    expect(fillMissingRankClasses(history)[0].constructed.rankClass).toBe("Mythic");
  });

  test("identifies major-rank increases without treating sub-tiers or season resets as promotions", () => {
    const history: RankHistoryPoint[] = [
      point(
        1,
        "Traditional_Ladder",
        "win",
        rankState({ seasonOrdinal: 90, rankClass: "Bronze", level: 1, step: 5 }),
        rankState(),
      ),
      point(
        2,
        "Traditional_Ladder",
        "win",
        rankState({ seasonOrdinal: 90, rankClass: "Silver", level: 4, step: 1 }),
        rankState(),
      ),
      point(
        3,
        "Traditional_Ladder",
        "win",
        rankState({ seasonOrdinal: 90, rankClass: "Silver", level: 3, step: 1 }),
        rankState(),
      ),
      point(
        4,
        "Traditional_Ladder",
        "loss",
        rankState({ seasonOrdinal: 91, rankClass: "Bronze", level: 4, step: 0 }),
        rankState(),
      ),
      point(
        5,
        "Traditional_Ladder",
        "win",
        rankState({ seasonOrdinal: 91, rankClass: "Silver", level: 4, step: 1 }),
        rankState(),
      ),
      point(
        6,
        "Traditional_Ladder",
        "win",
        rankState({ seasonOrdinal: 91, rankClass: "Diamond", level: 1, step: 5 }),
        rankState(),
      ),
      point(
        7,
        "Traditional_Ladder",
        "win",
        rankState({ seasonOrdinal: 91, level: 0, percentile: 91 }),
        rankState(),
      ),
    ];

    const series = buildGraphPoints(fillMissingRankClasses(history), "constructed", "all");
    expect(series).not.toBeNull();
    expect(rankPromotionsFor(series!.points, "constructed").map((entry) => entry.rankLabel)).toEqual([
      "Silver 4",
      "Silver 4",
      "Diamond 1",
      "Mythic 91%",
    ]);
  });

  test("does not invent promotions for a season whose rank classes cannot be resolved", () => {
    const history: RankHistoryPoint[] = [
      point(
        1,
        "Traditional_Ladder",
        "win",
        rankState({ seasonOrdinal: 91, level: 1, step: 5 }),
        rankState(),
      ),
      point(
        2,
        "Traditional_Ladder",
        "win",
        rankState({ seasonOrdinal: 91, level: 4, step: 1 }),
        rankState(),
      ),
    ];

    const series = buildGraphPoints(fillMissingRankClasses(history), "constructed");
    expect(series).not.toBeNull();
    expect(rankPromotionsFor(series!.points, "constructed")).toEqual([]);
  });

  test("returns null for previous season when only one season exists", () => {
    const history: RankHistoryPoint[] = [
      point(
        1,
        "Traditional_Ladder",
        "win",
        rankState({ seasonOrdinal: 12, rankClass: "Bronze", level: 4, step: 1, matchesWon: 1, matchesLost: 0 }),
        rankState(),
      ),
    ];

    expect(buildGraphPoints(history, "constructed", "previous")).toBeNull();
  });
});

describe("rankStepIndex", () => {
  test("counts absolute steps from the bottom of the ladder", () => {
    // Constructed: Spark (5/level) then Bronze (6/level). Silver 4 step 0 sits
    // above 4 Spark levels and 4 Bronze levels.
    const silverFloor = rankState({ seasonOrdinal: 12, rankClass: "Silver", level: 4, step: 0 });
    expect(rankStepIndex(silverFloor, "constructed")).toBe(4 * 5 + 4 * 6);

    const silverStep = rankState({ seasonOrdinal: 12, rankClass: "Silver", level: 3, step: 2 });
    expect(rankStepIndex(silverStep, "constructed")).toBe(4 * 5 + 4 * 6 + 6 + 2);
  });

  test("returns null without a known rank class", () => {
    expect(
      rankStepIndex(rankState({ seasonOrdinal: 12, level: 3, step: 2 }), "constructed"),
    ).toBeNull();
  });

  test("caps Mythic at the top of the ladder", () => {
    const mythic = rankState({ seasonOrdinal: 12, rankClass: "Mythic", level: 1, step: 0 });
    const diamondTop = rankState({ seasonOrdinal: 12, rankClass: "Diamond", level: 1, step: 6 });
    const mythicIndex = rankStepIndex(mythic, "constructed");
    const diamondIndex = rankStepIndex(diamondTop, "constructed");
    expect(mythicIndex).not.toBeNull();
    expect(diamondIndex).not.toBeNull();
    expect(mythicIndex!).toBe(diamondIndex!);
  });
});

describe("fillMissingRankClasses", () => {
  test("carries known classes forward and advances on promotion", () => {
    const history: RankHistoryPoint[] = [
      point(
        1,
        "Ladder",
        "win",
        rankState({ seasonOrdinal: 12, rankClass: "Silver", level: 1, step: 5, matchesWon: 1, matchesLost: 0 }),
        rankState(),
      ),
      point(
        2,
        "Ladder",
        "win",
        // Promotion: level 1 -> level 4 with no class reported.
        rankState({ seasonOrdinal: 12, level: 4, step: 1, matchesWon: 2, matchesLost: 0 }),
        rankState(),
      ),
      point(
        3,
        "Ladder",
        "loss",
        rankState({ seasonOrdinal: 12, level: 4, step: 0, matchesWon: 2, matchesLost: 1 }),
        rankState(),
      ),
    ];

    const filled = fillMissingRankClasses(history);
    expect(filled[1].constructed.rankClass).toBe("Gold");
    expect(filled[2].constructed.rankClass).toBe("Gold");
    // Input is untouched.
    expect(history[1].constructed.rankClass).toBe("");
  });

  test("backfills the season prefix before the first explicit class", () => {
    const history: RankHistoryPoint[] = [
      point(
        1,
        "Ladder",
        "win",
        rankState({ seasonOrdinal: 12, level: 1, step: 5, matchesWon: 1, matchesLost: 0 }),
        rankState(),
      ),
      point(
        2,
        "Ladder",
        "win",
        // Promotion boundary; the explicit class arrives after it.
        rankState({ seasonOrdinal: 12, level: 4, step: 1, matchesWon: 2, matchesLost: 0 }),
        rankState(),
      ),
      point(
        3,
        "Ladder",
        "win",
        rankState({ seasonOrdinal: 12, rankClass: "Gold", level: 4, step: 2, matchesWon: 3, matchesLost: 0 }),
        rankState(),
      ),
    ];

    const filled = fillMissingRankClasses(history);
    expect(filled[0].constructed.rankClass).toBe("Silver");
    expect(filled[1].constructed.rankClass).toBe("Gold");
  });

  test("leaves classes empty when a season has no explicit class", () => {
    const history: RankHistoryPoint[] = [
      point(
        1,
        "Ladder",
        "win",
        rankState({ seasonOrdinal: 12, level: 3, step: 2, matchesWon: 1, matchesLost: 0 }),
        rankState(),
      ),
    ];

    expect(fillMissingRankClasses(history)[0].constructed.rankClass).toBe("");
  });
});
