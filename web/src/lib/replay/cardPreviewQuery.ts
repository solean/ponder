import { cardDisplayName, type PreviewCard } from "./index";

export function cardPreviewQueryKey(
  card: PreviewCard,
): [string, number, string] {
  return ["card-preview", card.cardId, cardDisplayName(card)];
}
