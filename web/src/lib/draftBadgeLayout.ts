import type { OverlaySettings } from "./overlaySettings";

/** Percent coordinates within the game window. Keep every duplicate occurrence. */
export function draftBadgePosition(index: number, settings: OverlaySettings) {
  const column = index % settings.draftBadgeColumns;
  const row = Math.floor(index / settings.draftBadgeColumns);
  return {
    left: settings.draftBadgeLeft + column * (settings.draftBadgeWidth + settings.draftBadgeGapX) + settings.draftBadgeWidth / 2,
    top: settings.draftBadgeTop + row * (settings.draftBadgeHeight + settings.draftBadgeGapY) + settings.draftBadgeHeight * 0.88,
  };
}
