import { useQueries } from "@tanstack/react-query";
import { useMemo } from "react";
import { cardPreviewQueryKey } from "../../lib/replay/cardPreviewQuery";
import type { CardRarity } from "../../lib/scryfall";
import { fetchCardPreview } from "../../lib/scryfall";
import type { OpponentObservedCard } from "../../lib/types";
import { RarityDot } from "../RarityDot";
import { StatusMessage } from "../StatusMessage";
import { CardPreviewName, ManaCostDisplay } from "./CardPreviews";

type OpponentDeckCard = {
  cardId: number;
  cardName?: string;
  quantity: number;
};

type OpponentCardCategory =
  | "creatures"
  | "planeswalkers"
  | "instants"
  | "sorceries"
  | "artifacts"
  | "enchantments"
  | "battles"
  | "lands"
  | "other";

const OPPONENT_CATEGORY_LABELS: Record<OpponentCardCategory, string> = {
  creatures: "Creatures",
  planeswalkers: "Planeswalkers",
  instants: "Instants",
  sorceries: "Sorceries",
  artifacts: "Artifacts",
  enchantments: "Enchantments",
  battles: "Battles",
  lands: "Lands",
  other: "Other",
};

const OPPONENT_CATEGORY_ORDER = Object.keys(
  OPPONENT_CATEGORY_LABELS,
) as OpponentCardCategory[];

// classifyOpponentCard buckets an observed card by its Scryfall type line.
// Land wins over every other type (a creature-land counts toward lands for a
// deck read), then permanents, then the spell split. Cards without a resolved
// type line fall to "other" until their preview loads.
function classifyOpponentCard(typeLine: string): OpponentCardCategory {
  const lower = typeLine.toLowerCase();
  if (!lower) return "other";
  if (lower.includes("land")) return "lands";
  if (lower.includes("creature")) return "creatures";
  if (lower.includes("planeswalker")) return "planeswalkers";
  if (lower.includes("instant")) return "instants";
  if (lower.includes("sorcery")) return "sorceries";
  if (lower.includes("battle")) return "battles";
  if (lower.includes("artifact")) return "artifacts";
  if (lower.includes("enchantment")) return "enchantments";
  return "other";
}

type OpponentCardGroup = {
  category: OpponentCardCategory;
  label: string;
  cards: OpponentDeckCard[];
  distinct: number;
};

// groupOpponentCardsByType splits the observed cards into type sections in a
// fixed order, preserving the incoming (quantity-desc) card order within each.
function groupOpponentCardsByType(
  cards: OpponentDeckCard[],
  typeLines: Map<number, string>,
): OpponentCardGroup[] {
  const byCategory = new Map<OpponentCardCategory, OpponentDeckCard[]>();
  for (const card of cards) {
    const category = classifyOpponentCard(typeLines.get(card.cardId) ?? "");
    const bucket = byCategory.get(category) ?? [];
    bucket.push(card);
    byCategory.set(category, bucket);
  }
  return OPPONENT_CATEGORY_ORDER.flatMap((category) => {
    const bucket = byCategory.get(category);
    if (!bucket || bucket.length === 0) return [];
    return [
      {
        category,
        label: OPPONENT_CATEGORY_LABELS[category],
        cards: bucket,
        distinct: bucket.length,
      },
    ];
  });
}

export function useOpponentCardsMetadata(
  opponentObservedCards: OpponentObservedCard[],
) {
  const opponentCards = useMemo<OpponentDeckCard[]>(() => {
    return opponentObservedCards.map((card) => ({
      cardId: card.cardId,
      cardName: card.cardName,
      quantity: card.quantity,
    }));
  }, [opponentObservedCards]);

  const opponentCardPreviewQueries = useQueries({
    queries: opponentCards.map((card) => ({
      queryKey: cardPreviewQueryKey(card),
      queryFn: () => fetchCardPreview(card.cardId, card.cardName),
      enabled: card.cardId > 0,
      staleTime: 1000 * 60 * 60 * 24,
      gcTime: 1000 * 60 * 60 * 24,
      retry: 1,
    })),
  });

  const opponentManaCostsByCardID = useMemo(() => {
    const out = new Map<number, string>();
    for (let i = 0; i < opponentCards.length; i += 1) {
      const card = opponentCards[i];
      const preview = opponentCardPreviewQueries[i]?.data;
      out.set(card.cardId, preview?.manaCost?.trim() ?? "");
    }
    return out;
  }, [opponentCards, opponentCardPreviewQueries]);

  const opponentTypeLinesByCardID = useMemo(() => {
    const out = new Map<number, string>();
    for (let i = 0; i < opponentCards.length; i += 1) {
      const card = opponentCards[i];
      const preview = opponentCardPreviewQueries[i]?.data;
      out.set(card.cardId, preview?.typeLine?.trim() ?? "");
    }
    return out;
  }, [opponentCards, opponentCardPreviewQueries]);

  const opponentRaritiesByCardID = useMemo(() => {
    const out = new Map<number, CardRarity | undefined>();
    for (let i = 0; i < opponentCards.length; i += 1) {
      const card = opponentCards[i];
      out.set(card.cardId, opponentCardPreviewQueries[i]?.data?.rarity);
    }
    return out;
  }, [opponentCards, opponentCardPreviewQueries]);

  const opponentCardGroups = useMemo(
    () => groupOpponentCardsByType(opponentCards, opponentTypeLinesByCardID),
    [opponentCards, opponentTypeLinesByCardID],
  );

  const isOpponentCardMetadataLoading = opponentCardPreviewQueries.some(
    (previewQuery) => previewQuery.isPending,
  );
  return {
    opponentObservedCards,
    opponentCardGroups,
    opponentManaCostsByCardID,
    opponentRaritiesByCardID,
    isOpponentCardMetadataLoading,
  };
}

export function OpponentCardsPanel({
  cards,
}: {
  cards: ReturnType<typeof useOpponentCardsMetadata>;
}) {
  const {
    opponentObservedCards,
    opponentCardGroups,
    opponentManaCostsByCardID,
    opponentRaritiesByCardID,
    isOpponentCardMetadataLoading,
  } = cards;
  return (
    <section className="panel">
      <div className="panel-head">
        <h3>Observed Opponent Cards</h3>
        <p>{opponentObservedCards.length} unique cards</p>
      </div>
      {opponentObservedCards.length === 0 ? (
        <StatusMessage>
          No public opponent cards observed for this match yet.
        </StatusMessage>
      ) : (
        <div className="grid-cards">
          {opponentCardGroups.map((group) => (
            <article className="deck-card" key={group.category}>
              <h4>
                {group.label} ({group.distinct})
              </h4>
              <ul>
                {group.cards.map((card) => (
                  <li key={card.cardId}>
                    <span className="deck-card-qty">{card.quantity}x</span>
                    <CardPreviewName card={card} />
                    <span className="deck-card-mana">
                      <ManaCostDisplay
                        manaCost={
                          opponentManaCostsByCardID.get(card.cardId) ?? ""
                        }
                      />
                      <RarityDot
                        rarity={opponentRaritiesByCardID.get(card.cardId)}
                      />
                    </span>
                  </li>
                ))}
              </ul>
            </article>
          ))}
        </div>
      )}
      {isOpponentCardMetadataLoading ? (
        <StatusMessage>Loading card previews and mana details…</StatusMessage>
      ) : null}
      <p className="analytics-method-note">
        Copies show the most seen at once during the match — a lower bound on
        how many the deck runs, not a full decklist.
      </p>
    </section>
  );
}
