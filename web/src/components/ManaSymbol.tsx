import { useState } from "react";

import { manaSymbolAssetName } from "../lib/manaSymbols";

const MANA_SYMBOL_ASSETS = import.meta.glob<string>(
  "../assets/mana-symbols/*.svg",
  { eager: true, import: "default", query: "?url&no-inline" },
);

function manaSymbolURL(token: string): string | undefined {
  const assetName = manaSymbolAssetName(token);
  if (!assetName) {
    return undefined;
  }

  return MANA_SYMBOL_ASSETS[`../assets/mana-symbols/${assetName}.svg`];
}

export function ManaSymbol({ token }: { token: string }) {
  const [failedURL, setFailedURL] = useState<string | null>(null);
  const assetURL = manaSymbolURL(token);
  const label = `{${token}}`;

  if (!assetURL || failedURL === assetURL) {
    return (
      <code className="mana-symbol-fallback" aria-label={label}>
        {label}
      </code>
    );
  }

  return (
    <img
      className="mana-symbol-icon"
      src={assetURL}
      alt={label}
      loading="lazy"
      decoding="async"
      onError={() => setFailedURL(assetURL)}
    />
  );
}
