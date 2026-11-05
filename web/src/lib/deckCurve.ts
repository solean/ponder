/** Mana values 0–5 get their own column; 6 collects everything at 6 or more. */
export const CURVE_TOP_BUCKET = 6;

export type CurveCard = {
  cardId: number;
  name: string;
  copies: number;
  manaValue: number | null;
  typeLine?: string;
  colors?: string[];
};

export type CurveColorKey = "W" | "U" | "B" | "R" | "G" | "M" | "C";

export type CurveEntry = CurveCard & {
  manaValue: number;
  creature: boolean;
  /** The card's main type for display, e.g. "Creature" or "Instant". */
  primaryType: string;
  /** Mono-colored cards keep their color; "M" is multicolor, "C" colorless. */
  colorKey: CurveColorKey;
};

export type CurveBucket = {
  /** 0–6, where 6 means 6 or more. */
  bucket: number;
  count: number;
  creatureCount: number;
  /** Creatures first, then by name. */
  cards: CurveEntry[];
};

export type DeckCurve = {
  buckets: CurveBucket[];
  /** Nonland copies with a known mana value. */
  total: number;
  creatureCount: number;
  averageManaValue: number | null;
  /** The bucket holding the most copies (the cheapest one on a tie), or null when empty. */
  peakBucket: number | null;
  /** Copies at mana value 5 or more. */
  topEndCount: number;
  /** Nonland copies still missing a mana value (metadata not loaded). */
  unknownCount: number;
};

const COLOR_KEYS = new Set(["W", "U", "B", "R", "G"]);
// Checked in order, so an "Artifact Creature" reads as a creature.
const PRIMARY_TYPES = ["Creature", "Planeswalker", "Battle", "Instant", "Sorcery", "Enchantment", "Artifact"];

export function isLandType(typeLine?: string): boolean {
  return typeLine?.toLowerCase().includes("land") ?? false;
}

export function primaryCardType(typeLine?: string): string {
  const front = (typeLine ?? "").split("//")[0].split("—")[0];
  const words = front.trim().split(/\s+/).filter(Boolean);
  const match = PRIMARY_TYPES.find((type) => words.includes(type));
  return match ?? words[words.length - 1] ?? "Card";
}

export function curveColorKey(colors?: string[]): CurveColorKey {
  const known = (colors ?? []).filter((color) => COLOR_KEYS.has(color));
  if (known.length > 1) return "M";
  if (known.length === 1) return known[0] as CurveColorKey;
  return "C";
}

export function curveBucketLabel(bucket: number): string {
  return bucket >= CURVE_TOP_BUCKET ? `${CURVE_TOP_BUCKET}+` : String(bucket);
}

/** The nonland part of a deck as a mana curve: copies per mana value, with the cards in each. */
export function deckCurve(cards: CurveCard[]): DeckCurve {
  const buckets: CurveBucket[] = Array.from({ length: CURVE_TOP_BUCKET + 1 }, (_, bucket) => ({
    bucket,
    count: 0,
    creatureCount: 0,
    cards: [],
  }));
  let total = 0;
  let manaValueSum = 0;
  let creatureCount = 0;
  let unknownCount = 0;

  for (const card of cards) {
    if (card.copies <= 0 || isLandType(card.typeLine)) continue;
    if (card.manaValue == null || !Number.isFinite(card.manaValue)) {
      unknownCount += card.copies;
      continue;
    }
    const primaryType = primaryCardType(card.typeLine);
    const creature = primaryType === "Creature";
    const bucket = buckets[Math.min(CURVE_TOP_BUCKET, Math.max(0, Math.floor(card.manaValue)))];
    bucket.cards.push({ ...card, manaValue: card.manaValue, creature, primaryType, colorKey: curveColorKey(card.colors) });
    bucket.count += card.copies;
    total += card.copies;
    manaValueSum += card.manaValue * card.copies;
    if (creature) {
      bucket.creatureCount += card.copies;
      creatureCount += card.copies;
    }
  }

  let peakBucket: number | null = null;
  for (const bucket of buckets) {
    bucket.cards.sort((left, right) => Number(right.creature) - Number(left.creature) || left.name.localeCompare(right.name));
    if (bucket.count > 0 && (peakBucket == null || bucket.count > buckets[peakBucket].count)) {
      peakBucket = bucket.bucket;
    }
  }

  return {
    buckets,
    total,
    creatureCount,
    averageManaValue: total > 0 ? manaValueSum / total : null,
    peakBucket,
    topEndCount: buckets.filter((bucket) => bucket.bucket >= 5).reduce((sum, bucket) => sum + bucket.count, 0),
    unknownCount,
  };
}
