import type { CardRarity } from "../lib/cardPreviews";

export const RARITY_ORDER: CardRarity[] = ["common", "uncommon", "rare", "mythic"];

export const RARITY_LABELS: Record<CardRarity, string> = {
  common: "Common",
  uncommon: "Uncommon",
  rare: "Rare",
  mythic: "Mythic rare",
};

export function RarityDot({ rarity }: { rarity?: CardRarity }) {
  if (!rarity) {
    return <span className="rarity-dot is-unknown" aria-hidden="true" />;
  }
  return <span className={`rarity-dot rarity-${rarity}`} role="img" aria-label={RARITY_LABELS[rarity]} title={RARITY_LABELS[rarity]} />;
}
