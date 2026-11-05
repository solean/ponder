const percent = new Intl.NumberFormat("en", { style: "percent", maximumFractionDigits: 1 });

export function nextDrawOdds(remaining: number | undefined, library: number | undefined): string | undefined {
  if (library == null || remaining == null || !Number.isSafeInteger(library) || !Number.isSafeInteger(remaining)
    || library <= 0 || remaining < 0 || remaining > library) return undefined;
  return percent.format(remaining / library);
}

export function frontFaceIsLand(typeLine?: string): boolean | undefined {
  const front = typeLine?.split("//")[0].trim();
  return front ? /\bland\b/i.test(front) : undefined;
}

type DrawCard = { section: string; remaining?: number; isLand?: boolean };

export function landDrawOdds(cards: readonly DrawCard[], library: number | undefined): string | undefined {
  let lands = 0;
  let total = 0;
  for (const { section, remaining, isLand } of cards) {
    if (section !== "main" || remaining === 0) continue;
    if (isLand == null || remaining == null || !Number.isSafeInteger(remaining) || remaining < 0) return undefined;
    total += remaining;
    if (isLand) lands += remaining;
  }
  return total === library ? nextDrawOdds(lands, library) : undefined;
}
