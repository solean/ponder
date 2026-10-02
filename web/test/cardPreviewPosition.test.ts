import { describe, expect, test } from "bun:test";
import { cardPreviewPosition } from "../src/lib/cardPreviewPosition";

describe("card preview viewport placement", () => {
  test("moves bottom-row previews up so the full card fits", () => {
    const position = cardPreviewPosition({ left: 100, right: 240, top: 740, height: 24 }, 1200, 800);
    expect(position.top + position.height).toBe(792);
    expect(position.left).toBe(254);
  });

  test("flips previews left at the right edge and clamps at the top", () => {
    const position = cardPreviewPosition({ left: 1100, right: 1190, top: 0, height: 24 }, 1200, 800);
    expect(position.left + position.width).toBe(1086);
    expect(position.top).toBe(8);
  });

  test("fits all edges and preserves proportions on narrow or short viewports", () => {
    for (const [viewportWidth, viewportHeight] of [[320, 800], [1200, 300], [240, 240]]) {
      for (const [width, height] of [[336, 468], [360, 503]]) {
        for (const top of [0, viewportHeight - 24]) {
          const position = cardPreviewPosition(
            { left: viewportWidth - 100, right: viewportWidth, top, height: 24 },
            viewportWidth, viewportHeight, width, height,
          );
          expect(position.left).toBeGreaterThanOrEqual(8);
          expect(position.top).toBeGreaterThanOrEqual(8);
          expect(position.left + position.width).toBeLessThanOrEqual(viewportWidth - 8);
          expect(position.top + position.height).toBeLessThanOrEqual(viewportHeight - 8);
          expect(position.width / position.height).toBeCloseTo(width / height);
        }
      }
    }
  });
});
