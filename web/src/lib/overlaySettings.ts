export type OverlaySettings = {
  draftCardBadges: boolean;
  draftBadgeLeft: number;
  draftBadgeTop: number;
  draftBadgeWidth: number;
  draftBadgeHeight: number;
  draftBadgeGapX: number;
  draftBadgeGapY: number;
  draftBadgeColumns: number;
  showDeck: boolean;
  showOpponent: boolean;
  showDraft: boolean;
  panelSize: "compact" | "default" | "large";
  opacity: number;
  cardPreviews: boolean;
  hoverDelayMs: number;
  shortcut: string;
};

export const DEFAULT_OVERLAY_SETTINGS: OverlaySettings = {
  draftCardBadges: true,
  draftBadgeLeft: 18.7,
  draftBadgeTop: 28.5,
  draftBadgeWidth: 7.9,
  draftBadgeHeight: 31.6,
  draftBadgeGapX: 1.3,
  draftBadgeGapY: 1.6,
  draftBadgeColumns: 8,
  showDeck: true,
  showOpponent: true,
  showDraft: true,
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
