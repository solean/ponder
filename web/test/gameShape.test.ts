import { describe, expect, test } from "bun:test";

import { capGameLengths, curveTicks } from "../src/lib/gameShape";
import type { AnalyticsBucket } from "../src/lib/types";

function bucket(key: number, wins: number, losses: number, unknownResults = 0): AnalyticsBucket {
  return { key, record: { games: wins + losses, wins, losses, draws: 0 }, unknownResults };
}

describe("capGameLengths", () => {
  test("merges every bucket at or past the cap into one", () => {
    const result = capGameLengths([bucket(16, 4, 3), bucket(14, 9, 10, 1), bucket(15, 7, 3), bucket(19, 1, 0, 2)]);
    expect(result).toEqual([
      bucket(14, 9, 10, 1),
      { key: 15, record: { games: 18, wins: 12, losses: 6, draws: 0 }, unknownResults: 2 },
    ]);
  });

  test("leaves short game lists alone", () => {
    expect(capGameLengths([bucket(7, 1, 1), bucket(5, 2, 0)])).toEqual([bucket(5, 2, 0), bucket(7, 1, 1)]);
  });
});

describe("curveTicks", () => {
  test("uses half steps for small averages", () => {
    expect(curveTicks(1.1)).toEqual([0, 0.5, 1, 1.5]);
  });

  test("uses double steps for land counts", () => {
    expect(curveTicks(7.6)).toEqual([0, 2, 4, 6, 8]);
  });

  test("keeps an exact top tick", () => {
    expect(curveTicks(4)).toEqual([0, 1, 2, 3, 4]);
  });
});
