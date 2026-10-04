export type OverlaySettings = {
  showDeck: boolean;
  showOpponent: boolean;
  panelSize: "compact" | "default" | "large";
  opacity: number;
  cardPreviews: boolean;
  hoverDelayMs: number;
  shortcut: string;
};

export const DEFAULT_OVERLAY_SETTINGS: OverlaySettings = {
  showDeck: true,
  showOpponent: true,
  panelSize: "default",
  opacity: 0.9,
  cardPreviews: true,
  hoverDelayMs: 250,
  shortcut: "CmdOrCtrl+Shift+O",
};

export function overlayPanelWidth(size: OverlaySettings["panelSize"]): number {
  return { compact: 16, default: 18, large: 21 }[size];
}

export function overlayShortcutLabel(shortcut: string, mac = typeof navigator !== "undefined" && navigator.platform.startsWith("Mac")): string {
  return shortcut.split("+").map((key) => {
    if (key === "CmdOrCtrl") return mac ? "⌘" : "Ctrl";
    if (mac && key === "Shift") return "⇧";
    if (mac && key === "Alt") return "⌥";
    if (mac && key === "Ctrl") return "⌃";
    if (key === "Cmd" || key === "Super") return mac ? "⌘" : "Win";
    return key;
  }).join(mac ? "" : "+");
}
