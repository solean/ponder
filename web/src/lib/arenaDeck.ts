export type ArenaDeckCard = {
  section: string;
  cardName?: string;
  quantity: number;
};

const ARENA_SECTION_ORDER = ["companion", "command", "main", "sideboard"] as const;

const ARENA_SECTION_HEADINGS: Record<(typeof ARENA_SECTION_ORDER)[number], string> = {
  companion: "Companion",
  command: "Commander",
  main: "Deck",
  sideboard: "Sideboard",
};

export function formatArenaDeck(cards: readonly ArenaDeckCard[]): string {
  const grouped = new Map<string, Array<{ cardName: string; quantity: number }>>();

  for (const card of cards) {
    const section = card.section.trim().toLowerCase();
    if (!ARENA_SECTION_ORDER.includes(section as (typeof ARENA_SECTION_ORDER)[number])) {
      throw new Error(`Unsupported Arena deck section: ${card.section || "(empty)"}`);
    }

    const cardName = card.cardName?.trim();
    if (!cardName) {
      throw new Error("Every exported card must have a name");
    }
    if (!Number.isInteger(card.quantity) || card.quantity <= 0) {
      throw new Error(`Invalid quantity for ${cardName}`);
    }

    const sectionCards = grouped.get(section) ?? [];
    sectionCards.push({ cardName, quantity: card.quantity });
    grouped.set(section, sectionCards);
  }

  return ARENA_SECTION_ORDER.flatMap((section) => {
    const sectionCards = grouped.get(section);
    if (!sectionCards?.length) {
      return [];
    }

    const lines = sectionCards
      .sort((a, b) => a.cardName.localeCompare(b.cardName, undefined, { sensitivity: "base" }))
      .map((card) => `${card.quantity} ${card.cardName}`);
    return [`${ARENA_SECTION_HEADINGS[section]}\n${lines.join("\n")}`];
  }).join("\n\n");
}
