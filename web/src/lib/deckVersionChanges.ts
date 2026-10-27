import type { DeckCard, DeckVersion } from "./types";

/**
 * Card-level changes between two deck versions.
 *
 * Arena stores the main deck and sideboard as separate sections, so moving a
 * card between them reads as a removal in one and an addition in the other.
 * Those mirrored sideboard deltas are folded into the main-deck change (as
 * `movedQuantity`) so a swap counts once. In draft the sideboard is the rest of
 * the pool, which makes nearly every edit a swap.
 */

export type DeckVersionChange = {
  section: string;
  cardId: number;
  cardName?: string;
  quantityDelta: number;
  /** Copies of a main-deck change that came from (or went to) the sideboard. */
  movedQuantity: number;
};

const MAIN = "main";
const SIDEBOARD = "sideboard";

export function deckVersionChanges(version: DeckVersion, previousVersion: DeckVersion): DeckVersionChange[] {
  const changes = new Map<string, DeckVersionChange>();
  const key = (card: Pick<DeckCard, "section" | "cardId">) => `${card.section}:${card.cardId}`;

  for (const card of previousVersion.cards) {
    changes.set(key(card), {
      section: card.section,
      cardId: card.cardId,
      cardName: card.cardName,
      quantityDelta: -card.quantity,
      movedQuantity: 0,
    });
  }

  for (const card of version.cards) {
    const existing = changes.get(key(card));
    changes.set(key(card), {
      section: card.section,
      cardId: card.cardId,
      cardName: card.cardName || existing?.cardName,
      quantityDelta: (existing?.quantityDelta ?? 0) + card.quantity,
      movedQuantity: 0,
    });
  }

  for (const main of changes.values()) {
    if (main.section !== MAIN) {
      continue;
    }
    const side = changes.get(key({ section: SIDEBOARD, cardId: main.cardId }));
    if (!side || Math.sign(side.quantityDelta) !== -Math.sign(main.quantityDelta)) {
      continue;
    }
    const moved = Math.min(Math.abs(main.quantityDelta), Math.abs(side.quantityDelta));
    main.movedQuantity = moved;
    side.quantityDelta += Math.sign(main.quantityDelta) * moved;
  }

  return Array.from(changes.values())
    .filter((change) => change.quantityDelta !== 0)
    .sort((a, b) => {
      if (a.section !== b.section) {
        return a.section.localeCompare(b.section);
      }
      return displayName(a).localeCompare(displayName(b), undefined, { sensitivity: "base" });
    });
}

/** Total copies added and removed, with main↔sideboard moves counted once. */
export function deckVersionChangeTotals(changes: DeckVersionChange[]): { additions: number; removals: number } {
  let additions = 0;
  let removals = 0;
  for (const change of changes) {
    if (change.quantityDelta > 0) {
      additions += change.quantityDelta;
    } else {
      removals -= change.quantityDelta;
    }
  }
  return { additions, removals };
}

function displayName(change: DeckVersionChange): string {
  return change.cardName?.trim() || `Card ${change.cardId}`;
}
