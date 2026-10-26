import { describe, expect, test } from "bun:test";

import { deckVersionChangeTotals, deckVersionChanges } from "../src/lib/deckVersionChanges";
import type { DeckCard, DeckVersion } from "../src/lib/types";

const PLAINS = 90187;
const SWAMP = 90189;
const FUTURESCRIBE = 106364;
const THEORIX_CHARM = 106396;
const RATTLER = 106290;

function version(versionNumber: number, cards: Array<[string, number, number]>): DeckVersion {
  return {
    id: versionNumber,
    versionNumber,
    cardsHash: `hash-${versionNumber}`,
    cards: cards.map(([section, cardId, quantity]): DeckCard => ({ section, cardId, quantity })),
  };
}

function summarize(next: DeckVersion, previous: DeckVersion) {
  return deckVersionChanges(next, previous).map(({ section, cardId, quantityDelta, movedQuantity }) => ({
    section,
    cardId,
    quantityDelta,
    movedQuantity,
  }));
}

describe("deckVersionChanges", () => {
  test("folds main↔sideboard swaps into one main-deck change each", () => {
    const v1 = version(1, [
      ["main", FUTURESCRIBE, 1],
      ["main", THEORIX_CHARM, 2],
      ["main", PLAINS, 3],
      ["main", SWAMP, 5],
      ["sideboard", THEORIX_CHARM, 1],
      ["sideboard", RATTLER, 1],
    ]);
    const v2 = version(2, [
      ["main", THEORIX_CHARM, 3],
      ["main", PLAINS, 2],
      ["main", SWAMP, 6],
      ["sideboard", FUTURESCRIBE, 1],
      ["sideboard", RATTLER, 1],
    ]);

    const changes = deckVersionChanges(v2, v1);
    expect(changes).toHaveLength(4);
    expect(changes.every((change) => change.section === "main")).toBe(true);
    expect(changes.find((change) => change.cardId === THEORIX_CHARM)).toMatchObject({
      quantityDelta: 1,
      movedQuantity: 1,
    });
    expect(changes.find((change) => change.cardId === FUTURESCRIBE)).toMatchObject({
      quantityDelta: -1,
      movedQuantity: 1,
    });
    expect(changes.find((change) => change.cardId === SWAMP)).toMatchObject({ quantityDelta: 1, movedQuantity: 0 });
    expect(changes.find((change) => change.cardId === PLAINS)).toMatchObject({ quantityDelta: -1, movedQuantity: 0 });
    expect(deckVersionChangeTotals(changes)).toEqual({ additions: 2, removals: 2 });
  });

  test("keeps the unmatched part of a partial move", () => {
    const v1 = version(1, [
      ["main", THEORIX_CHARM, 1],
      ["sideboard", THEORIX_CHARM, 1],
    ]);
    const v2 = version(2, [["main", THEORIX_CHARM, 3]]);

    expect(summarize(v2, v1)).toEqual([
      { section: "main", cardId: THEORIX_CHARM, quantityDelta: 2, movedQuantity: 1 },
    ]);
  });

  test("leaves the sideboard surplus when more moved out than came in", () => {
    const v1 = version(1, [
      ["main", THEORIX_CHARM, 1],
      ["sideboard", THEORIX_CHARM, 3],
    ]);
    const v2 = version(2, [["main", THEORIX_CHARM, 2]]);

    expect(summarize(v2, v1)).toEqual([
      { section: "main", cardId: THEORIX_CHARM, quantityDelta: 1, movedQuantity: 1 },
      { section: "sideboard", cardId: THEORIX_CHARM, quantityDelta: -2, movedQuantity: 0 },
    ]);
  });

  test("reports main-deck-only changes like basic land swaps unchanged", () => {
    const v1 = version(1, [
      ["main", PLAINS, 3],
      ["main", SWAMP, 5],
    ]);
    const v2 = version(2, [
      ["main", PLAINS, 2],
      ["main", SWAMP, 6],
    ]);

    expect(summarize(v2, v1)).toEqual([
      { section: "main", cardId: PLAINS, quantityDelta: -1, movedQuantity: 0 },
      { section: "main", cardId: SWAMP, quantityDelta: 1, movedQuantity: 0 },
    ]);
  });

  test("reports sideboard-only changes", () => {
    const v1 = version(1, [["main", SWAMP, 6]]);
    const v2 = version(2, [
      ["main", SWAMP, 6],
      ["sideboard", RATTLER, 1],
    ]);

    const changes = deckVersionChanges(v2, v1);
    expect(summarize(v2, v1)).toEqual([
      { section: "sideboard", cardId: RATTLER, quantityDelta: 1, movedQuantity: 0 },
    ]);
    expect(deckVersionChangeTotals(changes)).toEqual({ additions: 1, removals: 0 });
  });

  test("does not pair same-direction main and sideboard changes", () => {
    const v1 = version(1, []);
    const v2 = version(2, [
      ["main", THEORIX_CHARM, 1],
      ["sideboard", THEORIX_CHARM, 1],
    ]);

    expect(summarize(v2, v1)).toEqual([
      { section: "main", cardId: THEORIX_CHARM, quantityDelta: 1, movedQuantity: 0 },
      { section: "sideboard", cardId: THEORIX_CHARM, quantityDelta: 1, movedQuantity: 0 },
    ]);
  });
});
