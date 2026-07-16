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
};

const SCRYFALL_BASE_URL = "https://api.scryfall.com";

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

async function fetchScryfallCard(path: string): Promise<ScryfallCard | null> {
  const response = await fetch(`${SCRYFALL_BASE_URL}${path}`, {
    headers: {
      Accept: "application/json",
    },
  });
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

export async function fetchCardPreview(cardID: number, cardName?: string): Promise<CardPreview | null> {
  if (!Number.isFinite(cardID) || cardID <= 0) {
    return null;
  }

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

  if (!card) {
    return null;
  }

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
  };
}
