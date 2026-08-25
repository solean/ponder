import type { CardDefinition, DeckProjectCard, DeckProjectSection } from "./types";

/** Splits "{1}{R}{U/P}" into ["1", "R", "U/P"]. */
export function parseManaCostSymbols(manaCost: string): string[] {
  const symbols: string[] = [];
  const pattern = /\{([^}]+)\}/g;
  let match: RegExpExecArray | null;
  while ((match = pattern.exec(manaCost)) !== null) {
    symbols.push(match[1]);
  }
  return symbols;
}

export function sectionCards(cards: DeckProjectCard[], section: DeckProjectSection): DeckProjectCard[] {
  return cards.filter((card) => card.section === section);
}

export function sectionTotal(cards: DeckProjectCard[], section: DeckProjectSection): number {
  return sectionCards(cards, section).reduce((sum, card) => sum + card.quantity, 0);
}

export function isLand(card: Pick<DeckProjectCard, "typeLine">): boolean {
  return /\bland\b/i.test(card.typeLine);
}

export function isBasicLand(card: Pick<DeckProjectCard, "typeLine" | "name">): boolean {
  if (/\bbasic land\b/i.test(card.typeLine)) return true;
  // Name fallback for cards whose type metadata is unresolved.
  if (card.typeLine.trim() === "") {
    return /^(plains|island|swamp|mountain|forest|wastes)$/i.test(card.name.trim());
  }
  return false;
}

export const MANA_CURVE_BUCKETS = ["0", "1", "2", "3", "4", "5", "6", "7+"] as const;

/** Main-deck mana curve over nonland cards, bucketed 0..6 and 7+. */
export function manaCurve(cards: DeckProjectCard[]): number[] {
  const buckets = new Array<number>(MANA_CURVE_BUCKETS.length).fill(0);
  for (const card of sectionCards(cards, "main")) {
    if (isLand(card) || card.manaValue == null) continue;
    const bucket = Math.min(Math.max(Math.round(card.manaValue), 0), MANA_CURVE_BUCKETS.length - 1);
    buckets[bucket] += card.quantity;
  }
  return buckets;
}

export const DECK_COLOR_ORDER = ["W", "U", "B", "R", "G"] as const;

/** Main-deck card counts per color (multicolor cards count in each color). */
export function colorCounts(cards: DeckProjectCard[]): Record<string, number> {
  const counts: Record<string, number> = {};
  for (const card of sectionCards(cards, "main")) {
    for (const color of card.colors) {
      counts[color] = (counts[color] ?? 0) + card.quantity;
    }
  }
  return counts;
}

/**
 * Advisory validation warnings. The builder never blocks saving or export;
 * these surface likely mistakes the way the research recommends.
 */
export function deckWarnings(name: string, cards: DeckProjectCard[]): string[] {
  const warnings: string[] = [];

  if (name.trim() === "") {
    warnings.push("Deck has no name.");
  }

  const mainTotal = sectionTotal(cards, "main");
  if (mainTotal > 0 && mainTotal < 60) {
    warnings.push(`Main deck has ${mainTotal} cards; most formats expect at least 60.`);
  }

  const sideboardTotal = sectionTotal(cards, "sideboard");
  if (sideboardTotal > 15) {
    warnings.push(`Sideboard has ${sideboardTotal} cards; most formats allow at most 15.`);
  }

  // All printings of the same logical card share the four-copy limit.
  const copiesByName = new Map<string, { count: number; label: string; basic: boolean }>();
  for (const card of cards) {
    const key = card.name.trim().toLowerCase() || `#${card.arenaId}`;
    const existing = copiesByName.get(key);
    if (existing) {
      existing.count += card.quantity;
    } else {
      copiesByName.set(key, {
        count: card.quantity,
        label: card.name.trim() || `card #${card.arenaId}`,
        basic: isBasicLand(card),
      });
    }
  }
  for (const entry of copiesByName.values()) {
    if (!entry.basic && entry.count > 4) {
      warnings.push(`More than 4 copies of ${entry.label} (${entry.count} across main and sideboard).`);
    }
  }

  for (const card of cards) {
    if (card.missing) {
      warnings.push(
        `${card.name.trim() || `Card #${card.arenaId}`} is no longer in the current Arena catalog; re-add it from search.`,
      );
    }
  }

  return warnings;
}

export function definitionToProjectCard(
  definition: CardDefinition,
  section: DeckProjectSection,
  quantity = 1,
): DeckProjectCard {
  return {
    section,
    arenaId: definition.arenaId,
    quantity,
    name: definition.name,
    setCode: definition.setCode,
    collectorNumber: definition.collectorNumber,
    rarity: definition.rarity,
    manaCost: definition.manaCost,
    manaValue: definition.manaValue,
    colors: definition.colors,
    typeLine: definition.typeLine,
    missing: false,
  };
}

/** Adds one copy, merging with an existing row for the same printing+section. */
export function addCard(
  cards: DeckProjectCard[],
  definition: CardDefinition,
  section: DeckProjectSection,
): DeckProjectCard[] {
  const index = cards.findIndex((card) => card.arenaId === definition.arenaId && card.section === section);
  if (index >= 0) {
    return cards.map((card, i) => (i === index ? { ...card, quantity: card.quantity + 1 } : card));
  }
  return [...cards, definitionToProjectCard(definition, section)];
}

/** Adjusts a row's quantity by delta, dropping the row at zero. */
export function adjustQuantity(
  cards: DeckProjectCard[],
  arenaId: number,
  section: DeckProjectSection,
  delta: number,
): DeckProjectCard[] {
  return cards.flatMap((card) => {
    if (card.arenaId !== arenaId || card.section !== section) return [card];
    const quantity = card.quantity + delta;
    if (quantity <= 0) return [];
    return [{ ...card, quantity }];
  });
}

export function removeCard(cards: DeckProjectCard[], arenaId: number, section: DeckProjectSection): DeckProjectCard[] {
  return cards.filter((card) => !(card.arenaId === arenaId && card.section === section));
}

/** Moves a whole row to the other section, merging quantities on collision. */
export function moveCard(cards: DeckProjectCard[], arenaId: number, section: DeckProjectSection): DeckProjectCard[] {
  const source = cards.find((card) => card.arenaId === arenaId && card.section === section);
  if (!source) return cards;
  const target: DeckProjectSection = section === "main" ? "sideboard" : "main";

  const withoutSource = cards.filter((card) => card !== source);
  const existingIndex = withoutSource.findIndex((card) => card.arenaId === arenaId && card.section === target);
  if (existingIndex >= 0) {
    return withoutSource.map((card, i) =>
      i === existingIndex ? { ...card, quantity: card.quantity + source.quantity } : card,
    );
  }
  return [...withoutSource, { ...source, section: target }];
}

/** Stable editor ordering: lands last, then by mana value, then name. */
export function sortProjectCards(cards: DeckProjectCard[]): DeckProjectCard[] {
  return [...cards].sort((a, b) => {
    const landDiff = Number(isLand(a)) - Number(isLand(b));
    if (landDiff !== 0) return landDiff;
    const mvA = a.manaValue ?? Number.POSITIVE_INFINITY;
    const mvB = b.manaValue ?? Number.POSITIVE_INFINITY;
    if (mvA !== mvB) return mvA - mvB;
    const nameDiff = a.name.localeCompare(b.name);
    if (nameDiff !== 0) return nameDiff;
    return a.arenaId - b.arenaId;
  });
}
