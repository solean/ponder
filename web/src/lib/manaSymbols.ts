const MANA_SYMBOL_ASSET_ALIASES: Readonly<Record<string, string>> = {
  "½": "HALF",
  "∞": "INFINITY",
};

export function manaSymbolAssetName(token: string): string | null {
  const normalized = token.trim().toUpperCase().replace(/\s+/g, "");
  const assetName = MANA_SYMBOL_ASSET_ALIASES[normalized] ?? normalized.replace(/\//g, "");

  return /^[A-Z0-9]+$/.test(assetName) ? assetName : null;
}
