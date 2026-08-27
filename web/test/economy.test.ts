import { describe, expect, test } from "bun:test";

import { eventEntryLabel, eventRewardLabel } from "../src/lib/economy";
import type { EventRunEconomy } from "../src/lib/types";

function makeRun(overrides: Partial<EventRunEconomy> = {}): EventRunEconomy {
  return {
    id: 12,
    eventName: "PremierDraft_HOB_20260811",
    eventType: "premier_draft",
    setCode: "HOB",
    status: "claimed",
    startedAt: "2026-08-26T21:55:23Z",
    endedAt: "2026-08-26T23:18:57Z",
    wins: 6,
    losses: 3,
    entryCurrencyType: "Gem",
    entryCurrencyPaid: 1500,
    entryGold: 0,
    entryGems: -1500,
    rewardGold: 0,
    rewardGems: 1800,
    rewardBoosters: [{ setCode: "HOB", count: 5 }],
    rewardCards: 0,
    rewardVaultProgress: 0,
    netGold: 0,
    netGems: 300,
    linkConfidence: "inferred",
    ...overrides,
  };
}

describe("event economy labels", () => {
  test("formats draft gem cost and winnings", () => {
    const run = makeRun();
    expect(eventEntryLabel(run)).toBe("−1,500 gems");
    expect(eventRewardLabel(run)).toBe("+1,800 gems · 5 packs");
  });

  test("formats gold entries and mixed currency winnings", () => {
    const run = makeRun({
      entryCurrencyType: "Gold",
      entryCurrencyPaid: 10000,
      entryGold: -10000,
      entryGems: 0,
      rewardGold: 500,
      rewardGems: 250,
      rewardBoosters: [],
    });
    expect(eventEntryLabel(run)).toBe("−10,000 gold");
    expect(eventRewardLabel(run)).toBe("+500 gold · +250 gems");
  });

  test("distinguishes token entries from missing rewards", () => {
    const run = makeRun({
      entryCurrencyType: "DraftToken",
      entryCurrencyPaid: 1,
      entryGems: 0,
      rewardGems: 0,
      rewardBoosters: [],
      linkConfidence: "none",
    });
    expect(eventEntryLabel(run)).toBe("Draft Token");
    expect(eventRewardLabel(run)).toBe("not captured");
  });
});
