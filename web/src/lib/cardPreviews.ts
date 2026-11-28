import { requestJSON } from "./apiError";

export type CardRarity = "common" | "uncommon" | "rare" | "mythic";

/**
 * What card hovers and art tiles show. The backend keeps a stored copy of
 * every Arena card from Scryfall, so previews survive restarts and work
 * offline; images still load from Scryfall's servers.
 */
export type CardPreview = {
  name: string;
  imageUrl: string;
  artCropUrl?: string;
  scryfallUrl?: string;
  manaCost?: string;
  manaValue?: number;
  typeLine?: string;
  rarity?: CardRarity;
  colors?: string[];
};

const API_BASE = import.meta.env.VITE_API_BASE ?? "";
/** Previews asked for in the same tick go to the backend together. */
const CARD_PREVIEW_BATCH_MAX = 200;

type PendingCardPreviewRequest = {
  cardID: number;
  cardName?: string;
  resolve: (preview: CardPreview | null) => void;
  reject: (error: unknown) => void;
};

const pendingCardPreviewRequests: PendingCardPreviewRequest[] = [];
let cardPreviewBatchScheduled = false;

async function resolveCardPreviewBatch(requests: PendingCardPreviewRequest[]): Promise<void> {
  const response = await requestJSON<{ previews?: Array<CardPreview | null> }>(`${API_BASE}/api/cards/previews`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      cards: requests.map((request) => ({ cardId: request.cardID, name: request.cardName?.trim() ?? "" })),
    }),
  });
  requests.forEach((request, index) => {
    request.resolve(response.previews?.[index] ?? null);
  });
}

function scheduleCardPreviewBatch(): void {
  if (cardPreviewBatchScheduled) {
    return;
  }
  cardPreviewBatchScheduled = true;
  queueMicrotask(() => {
    cardPreviewBatchScheduled = false;
    const requests = pendingCardPreviewRequests.splice(0, CARD_PREVIEW_BATCH_MAX);
    void resolveCardPreviewBatch(requests)
      .catch((error: unknown) => {
        for (const request of requests) {
          request.reject(error);
        }
      })
      .finally(() => {
        if (pendingCardPreviewRequests.length > 0) {
          scheduleCardPreviewBatch();
        }
      });
  });
}

/**
 * Resolves to the card's preview, or null when no source knows the card.
 * Rejects when the backend can't be reached, so React Query can retry.
 */
export function fetchCardPreview(cardID: number, cardName?: string): Promise<CardPreview | null> {
  // Settings samples have a card name but no Arena printing ID.
  if (!Number.isFinite(cardID) || cardID < 0 || (cardID === 0 && !cardName?.trim())) {
    return Promise.resolve(null);
  }

  const { promise, resolve, reject } = Promise.withResolvers<CardPreview | null>();
  pendingCardPreviewRequests.push({ cardID, cardName, resolve, reject });
  scheduleCardPreviewBatch();
  return promise;
}
