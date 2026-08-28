export type CardRarity = "common" | "uncommon" | "rare" | "mythic";

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

type ScryfallImageURIs = {
  png?: string;
  large?: string;
  normal?: string;
  small?: string;
  art?: string;
  art_crop?: string;
};

type ScryfallCardFace = {
  image_uris?: ScryfallImageURIs | null;
  mana_cost?: string;
  type_line?: string;
  colors?: string[];
};

type ScryfallCard = {
  name?: string;
  scryfall_uri?: string;
  image_uris?: ScryfallImageURIs | null;
  card_faces?: ScryfallCardFace[] | null;
  mana_cost?: string;
  cmc?: number;
  type_line?: string;
  rarity?: string;
  colors?: string[];
};

const SCRYFALL_BASE_URL = "https://api.scryfall.com";
const SCRYFALL_MIN_REQUEST_INTERVAL_MS = 200;
const SCRYFALL_COLLECTION_MIN_REQUEST_INTERVAL_MS = 500;
const SCRYFALL_DEFAULT_COOLDOWN_MS = 60_000;
const SCRYFALL_COLLECTION_MAX_CARDS = 75;

type ScryfallCollection = {
  data?: ScryfallCard[];
  not_found?: Array<{ name?: string }>;
};

type PendingCardPreviewRequest = {
  cardID: number;
  cardName?: string;
  resolve: (preview: CardPreview | null) => void;
};

let scryfallRequestQueue: Promise<void> = Promise.resolve();
let nextScryfallRequestAt = 0;
let scryfallCooldownUntil = 0;
const pendingCardPreviewRequests: PendingCardPreviewRequest[] = [];
let cardPreviewBatchScheduled = false;

function retryAfterMilliseconds(value: string | null, now: number): number {
  const trimmed = value?.trim() ?? "";
  const seconds = Number(trimmed);
  if (trimmed && Number.isFinite(seconds) && seconds > 0) {
    return seconds * 1000;
  }

  const retryAt = Date.parse(trimmed);
  if (Number.isFinite(retryAt) && retryAt > now) {
    return retryAt - now;
  }
  return SCRYFALL_DEFAULT_COOLDOWN_MS;
}

function scheduleScryfallFetch(
  path: string,
  init: RequestInit = {},
  minRequestInterval = SCRYFALL_MIN_REQUEST_INTERVAL_MS,
): Promise<Response> {
  const request = scryfallRequestQueue.then(async () => {
    const now = Date.now();
    if (now < scryfallCooldownUntil) {
      throw new Error("Scryfall requests paused after rate limit");
    }

    const waitMilliseconds = Math.max(0, nextScryfallRequestAt - now);
    if (waitMilliseconds > 0) {
      const { promise, resolve } = Promise.withResolvers<void>();
      setTimeout(resolve, waitMilliseconds);
      await promise;
    }

    nextScryfallRequestAt = Date.now() + minRequestInterval;
    const headers = new Headers(init.headers);
    headers.set("Accept", "application/json");
    const response = await fetch(`${SCRYFALL_BASE_URL}${path}`, {
      ...init,
      headers,
    });
    if (response.status === 429) {
      const receivedAt = Date.now();
      scryfallCooldownUntil = receivedAt + retryAfterMilliseconds(response.headers.get("Retry-After"), receivedAt);
    }
    return response;
  });

  scryfallRequestQueue = request.then(
    () => undefined,
    () => undefined,
  );
  return request;
}

function pickImageURL(card: ScryfallCard): string {
  const root = card.image_uris ?? undefined;
  if (root) {
    const rootURL = root.normal ?? root.large ?? root.small ?? root.png;
    if (rootURL) {
      return rootURL;
    }
  }

  for (const face of card.card_faces ?? []) {
    const faceImage = face.image_uris ?? undefined;
    if (!faceImage) {
      continue;
    }
    const faceURL = faceImage.normal ?? faceImage.large ?? faceImage.small ?? faceImage.png;
    if (faceURL) {
      return faceURL;
    }
  }

  return "";
}


function pickArtCropURL(card: ScryfallCard): string {
  const rootArtCrop = card.image_uris?.art ?? card.image_uris?.art_crop;
  if (rootArtCrop) {
    return rootArtCrop;
  }

  for (const face of card.card_faces ?? []) {
    const faceArtCrop = face.image_uris?.art ?? face.image_uris?.art_crop;
    if (faceArtCrop) {
      return faceArtCrop;
    }
  }

  return "";
}

function pickManaCost(card: ScryfallCard): string {
  const rootCost = card.mana_cost?.trim();
  if (rootCost) {
    return rootCost;
  }

  const faceCosts = (card.card_faces ?? [])
    .map((face) => face.mana_cost?.trim() ?? "")
    .filter((cost) => cost.length > 0);
  if (faceCosts.length === 0) {
    return "";
  }
  return faceCosts.join(" // ");
}

function pickTypeLine(card: ScryfallCard): string {
  const rootType = card.type_line?.trim();
  if (rootType) {
    return rootType;
  }

  const faceTypes = (card.card_faces ?? [])
    .map((face) => face.type_line?.trim() ?? "")
    .filter((value) => value.length > 0);
  if (faceTypes.length === 0) {
    return "";
  }
  return faceTypes.join(" // ");
}

const COLOR_ORDER = ["W", "U", "B", "R", "G"];

function pickColors(card: ScryfallCard): string[] | undefined {
  const seen = new Set<string>();
  const add = (colors?: string[]) => {
    for (const color of colors ?? []) {
      const normalized = color.trim().toUpperCase();
      if (COLOR_ORDER.includes(normalized)) {
        seen.add(normalized);
      }
    }
  };
  add(card.colors);
  for (const face of card.card_faces ?? []) {
    add(face.colors);
  }
  if (seen.size === 0) {
    return undefined;
  }
  return COLOR_ORDER.filter((color) => seen.has(color));
}

function normalizeRarity(value?: string): CardRarity | undefined {
  switch (value?.trim().toLowerCase()) {
    case "common":
      return "common";
    case "uncommon":
      return "uncommon";
    case "rare":
      return "rare";
    case "mythic":
      return "mythic";
    default:
      return undefined;
  }
}

function cardPreviewFromScryfall(card: ScryfallCard, cardID: number, cardName?: string): CardPreview | null {
  const imageURL = pickImageURL(card);
  if (!imageURL) {
    return null;
  }

  return {
    name: card.name?.trim() || cardName?.trim() || `Card ${cardID}`,
    imageUrl: imageURL,
    artCropUrl: pickArtCropURL(card) || undefined,
    scryfallUrl: card.scryfall_uri,
    manaCost: pickManaCost(card),
    manaValue: typeof card.cmc === "number" && Number.isFinite(card.cmc) ? card.cmc : undefined,
    typeLine: pickTypeLine(card),
    rarity: normalizeRarity(card.rarity),
    colors: pickColors(card),
  };
}

function normalizedCollectionName(name?: string): string {
  return name?.trim().toLowerCase() ?? "";
}

async function fetchScryfallCollection(names: string[]): Promise<Array<ScryfallCard | null>> {
  const response = await scheduleScryfallFetch(
    "/cards/collection",
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        identifiers: names.map((name) => ({ name })),
      }),
    },
    SCRYFALL_COLLECTION_MIN_REQUEST_INTERVAL_MS,
  );
  if (!response.ok) {
    throw new Error(`Scryfall collection lookup failed (${response.status})`);
  }

  const collection = (await response.json()) as ScryfallCollection;
  const missingCounts = new Map<string, number>();
  for (const identifier of collection.not_found ?? []) {
    const name = normalizedCollectionName(identifier.name);
    missingCounts.set(name, (missingCounts.get(name) ?? 0) + 1);
  }

  const cards = collection.data ?? [];
  let cardIndex = 0;
  return names.map((name) => {
    const normalizedName = normalizedCollectionName(name);
    const missingCount = missingCounts.get(normalizedName) ?? 0;
    if (missingCount > 0) {
      missingCounts.set(normalizedName, missingCount - 1);
      return null;
    }

    const card = cards[cardIndex] ?? null;
    cardIndex += 1;
    return card;
  });
}

async function fetchScryfallCard(path: string): Promise<ScryfallCard | null> {
  const response = await scheduleScryfallFetch(path);
  if (response.status === 404) {
    return null;
  }
  if (!response.ok) {
    throw new Error(`Scryfall lookup failed (${response.status})`);
  }
  return (await response.json()) as ScryfallCard;
}

async function fetchByName(name: string): Promise<ScryfallCard | null> {
  const trimmedName = name.trim();
  if (!trimmedName) {
    return null;
  }
  const encoded = encodeURIComponent(trimmedName);
  const exact = await fetchScryfallCard(`/cards/named?exact=${encoded}`);
  if (exact) {
    return exact;
  }
  return fetchScryfallCard(`/cards/named?fuzzy=${encoded}`);
}

async function fetchCardPreviewIndividually(cardID: number, cardName?: string): Promise<CardPreview | null> {
  let card: ScryfallCard | null = null;
  try {
    card = await fetchScryfallCard(`/cards/arena/${cardID}`);
  } catch {
    card = null;
  }

  if (!card && cardName) {
    try {
      card = await fetchByName(cardName);
    } catch {
      card = null;
    }
  }

  return card ? cardPreviewFromScryfall(card, cardID, cardName) : null;
}

async function resolveCardPreviewBatch(requests: PendingCardPreviewRequest[]): Promise<void> {
  if (requests.length === 1) {
    const request = requests[0];
    request.resolve(await fetchCardPreviewIndividually(request.cardID, request.cardName));
    return;
  }

  const previews = new Map<PendingCardPreviewRequest, CardPreview | null>();
  const namedRequests = requests.filter((request) => normalizedCollectionName(request.cardName) !== "");
  if (namedRequests.length > 0) {
    try {
      const cards = await fetchScryfallCollection(namedRequests.map((request) => request.cardName!.trim()));
      for (let index = 0; index < namedRequests.length; index += 1) {
        const card = cards[index];
        if (card) {
          const request = namedRequests[index];
          previews.set(request, cardPreviewFromScryfall(card, request.cardID, request.cardName));
        }
      }
    } catch {
      // Fall through to the Arena-ID lookup for this batch.
    }
  }

  const unresolvedRequests = requests.filter((request) => !previews.has(request));
  const unresolvedPreviews = await Promise.all(
    unresolvedRequests.map((request) => fetchCardPreviewIndividually(request.cardID, request.cardName)),
  );
  for (let index = 0; index < unresolvedRequests.length; index += 1) {
    previews.set(unresolvedRequests[index], unresolvedPreviews[index]);
  }

  for (const request of requests) {
    request.resolve(previews.get(request) ?? null);
  }
}

function scheduleCardPreviewBatch(): void {
  if (cardPreviewBatchScheduled) {
    return;
  }
  cardPreviewBatchScheduled = true;
  queueMicrotask(() => {
    cardPreviewBatchScheduled = false;
    const requests = pendingCardPreviewRequests.splice(0, SCRYFALL_COLLECTION_MAX_CARDS);
    void resolveCardPreviewBatch(requests)
      .catch(() => {
        for (const request of requests) {
          request.resolve(null);
        }
      })
      .finally(() => {
        if (pendingCardPreviewRequests.length > 0) {
          scheduleCardPreviewBatch();
        }
      });
  });
}

export function fetchCardPreview(cardID: number, cardName?: string): Promise<CardPreview | null> {
  if (!Number.isFinite(cardID) || cardID <= 0) {
    return Promise.resolve(null);
  }

  const { promise, resolve } = Promise.withResolvers<CardPreview | null>();
  pendingCardPreviewRequests.push({ cardID, cardName, resolve });
  scheduleCardPreviewBatch();
  return promise;
}
