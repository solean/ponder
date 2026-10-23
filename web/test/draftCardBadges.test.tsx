import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { DraftCardBadges } from "../src/components/DraftCardBadges";
import { DEFAULT_OVERLAY_SETTINGS } from "../src/lib/overlaySettings";
import { draftBadgePosition } from "../src/lib/draftBadgeLayout";
import type { LiveDraft } from "../src/lib/types";

const draft: LiveDraft = {
  sessionId: 1, eventName: "PremierDraft_FRA", packNumber: 1, pickNumber: 2, waiting: false,
  pack: [{ cardId: 2, cardName: "Second" }, { cardId: 1, cardName: "First" }, { cardId: 1, cardName: "First" }],
  displayPack: [{ cardId: 1, cardName: "First" }, { cardId: 1, cardName: "First" }, { cardId: 2, cardName: "Second" }],
};
function render(value: LiveDraft, status = "ok") {
  const client = new QueryClient();
  client.setQueryData(["card-stats", value.eventName], {
    status, meanGihWinRate: 0.55, cards: [{ arenaId: 1, gihWinRate: 0.6, gihGameCount: 1000, gameCount: 2000,
      gpWinRate: 0.55, iwd: 0.05, ata: 3, alsa: 4 }],
  });
  const html = renderToStaticMarkup(<QueryClientProvider client={client}>
    <DraftCardBadges draft={value} settings={DEFAULT_OVERLAY_SETTINGS} />
  </QueryClientProvider>);
  client.clear();
  return html;
}

describe("on-card draft ratings", () => {
  test("uses display order, keeps duplicates, and marks missing ratings", () => {
    const html = render(draft);
    expect(html.match(/aria-label="First: 60.0% GIH win rate"/g)?.length).toBe(2);
    expect(html.indexOf('aria-label="First:')).toBeLessThan(html.indexOf('aria-label="Second:'));
    expect(html).toContain('Second: no GIH win rate available');
  });
  test("hides badges while waiting or without complete display metadata/ratings", () => {
    expect(render({ ...draft, waiting: true })).toBe("");
    expect(render({ ...draft, displayPack: undefined })).toBe("");
    expect(render({ ...draft, displayPack: draft.displayPack!.slice(1) })).toBe("");
    expect(render(draft, "unsupported")).toBe("");
  });
  test("wraps into the calibrated second row without recentering short packs", () => {
    const first = draftBadgePosition(0, DEFAULT_OVERLAY_SETTINGS);
    const ninth = draftBadgePosition(8, DEFAULT_OVERLAY_SETTINGS);
    expect(ninth.left).toBe(first.left);
    expect(ninth.top - first.top).toBeCloseTo(18.6);
    expect(draftBadgePosition(7, DEFAULT_OVERLAY_SETTINGS).left).toBeLessThan(100);
  });
  test("places all thirteen badges inside the cards in the full-window screenshot", () => {
    // Card rectangles measured in the user's 2048 × 1154 reference rendering.
    // The first reference was cropped and must not define viewport percentages.
    for (let index = 0; index < 13; index++) {
      const { left, top } = draftBadgePosition(index, DEFAULT_OVERLAY_SETTINGS);
      const cardLeft = 350 + (index % 8) * 171;
      const cardTop = 186 + Math.floor(index / 8) * 214;
      expect(left * 2048 / 100).toBeGreaterThan(cardLeft + 20);
      expect(left * 2048 / 100).toBeLessThan(cardLeft + 146 - 20);
      expect(top * 1154 / 100).toBeGreaterThan(cardTop + 150);
      expect(top * 1154 / 100).toBeLessThan(cardTop + 204 - 10);
    }
  });
});
