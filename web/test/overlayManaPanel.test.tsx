import { expect, mock, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
mock.module("../src/components/ManaSymbol", () => ({ ManaSymbol: ({ token }: { token: string }) => <span>{token}</span> }));
const { OverlayManaPanel } = await import("../src/components/OverlayManaPanel");

test("mana summary waits for missing state and hides with zero lands", () => {
  const waiting = renderToStaticMarkup(<OverlayManaPanel />);
  expect(waiting).toContain("White: unknown");
  expect(waiting).not.toContain("colors overlap");
  const empty = renderToStaticMarkup(<OverlayManaPanel mana={{ landsKnown: true, lands: 0, available: 0, byColor: {}, floating: 0, poolKnown: true, unresolved: 0 }} />);
  expect(empty).toBe("");
});

test("mana summary labels estimates and per-color counts", () => {
  const summary = renderToStaticMarkup(<OverlayManaPanel mana={{ landsKnown: true, lands: 3, available: 4, byColor: { W: 2, U: 3, C: 1 }, floating: 1, poolKnown: true, unresolved: 1 }} />);
  expect(summary).toContain("Available · est.");
  expect(summary).toContain("White: 2");
  expect(summary).toContain("Blue: 3");
  expect(summary).toContain("Colorless: 1");
  const unknownPool = renderToStaticMarkup(<OverlayManaPanel mana={{ landsKnown: true, lands: 3, available: 3, byColor: {}, floating: 0, poolKnown: false, unresolved: 0 }} />);
  expect(unknownPool).toContain("<strong>—</strong><span>In pool</span>");
});
