export function selectScrubberLabels(labels: { index: number; width: number }[], lastIndex: number, containerWidth: number, gap = 10): Map<number, number> {
  const selected = new Map<number, number>();
  let right = -gap;
  for (const label of labels) {
    if (containerWidth <= 0 || label.width > containerWidth) continue;
    const anchor = lastIndex > 0 ? label.index / lastIndex * containerWidth : 0;
    const left = Math.max(0, Math.min(anchor, containerWidth - label.width));
    if (left < right + gap) continue;
    selected.set(label.index, left);
    right = left + label.width;
  }
  return selected;
}
export function scrubberTurnLabel(turn: number, playDraw?: "play" | "draw" | ""): string {
  if (turn > 0 && playDraw) {
    const isSelf = (turn % 2 === 1) === (playDraw === "play");
    return `T${Math.ceil(turn / 2)} ${isSelf ? "You" : "Opp"}`;
  }
  return turn > 0 ? `T${Math.ceil(turn / 2)}${turn % 2 ? "a" : "b"}` : "Pre";
}
