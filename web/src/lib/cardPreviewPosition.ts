type AnchorRect = { top: number; right: number; left: number; height: number };

/** Fit the entire preview inside the viewport, preferring the anchor's right side. */
export function cardPreviewPosition(
  rect: AnchorRect,
  viewportWidth: number,
  viewportHeight: number,
  preferredWidth = 336,
  preferredHeight = 468,
) {
  const padding = 8;
  const gap = 14;
  const scale = Math.max(0, Math.min(1,
    (viewportWidth - padding * 2) / preferredWidth,
    (viewportHeight - padding * 2) / preferredHeight,
  ));
  const width = preferredWidth * scale;
  const height = preferredHeight * scale;
  const availableRight = viewportWidth - rect.right - padding;
  const availableLeft = rect.left - padding;
  const placeLeft = availableRight < width + gap && availableLeft > availableRight;
  const rawLeft = placeLeft ? rect.left - width - gap : rect.right + gap;
  const left = Math.max(padding, Math.min(rawLeft, viewportWidth - width - padding));
  const top = Math.max(padding, Math.min(
    rect.top + rect.height / 2 - height / 2,
    viewportHeight - height - padding,
  ));
  return { top, left, width, height };
}

export function floatingCardPreviewPosition(anchor: HTMLElement, width = 336, height = 468) {
  return cardPreviewPosition(
    anchor.getBoundingClientRect(),
    window.innerWidth || document.documentElement.clientWidth,
    window.innerHeight || document.documentElement.clientHeight,
    width,
    height,
  );
}
