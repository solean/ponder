import { expect, mock, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
mock.module("../src/components/ManaSymbol", () => ({ ManaSymbol: ({ token }: { token: string }) => <span>{token}</span> }));
const { OverlayManaPanel } = await import("../src/components/OverlayManaPanel");

test("mana summary distinguishes missing state from an empty battlefield", () => {
  const waiting = renderToStaticMarkup(<OverlayManaPanel />);
  expect(waiting).toContain("Waiting for game state");
  expect(waiting).toContain("White: unknown");
  const empty = renderToStaticMarkup(<OverlayManaPanel mana={{ landsKnown: true, lands: 0, available: 0, byColor: {}, floating: 0, poolKnown: true, unresolved: 0 }} />);
  expect(empty).toContain("White: 0");
  expect(empty).not.toContain("Waiting for game state");
});

test("mana summary labels estimates, overlapping colors and incomplete sources", () => {
  const summary = renderToStaticMarkup(<OverlayManaPanel mana={{ landsKnown: true, lands: 3, available: 4, byColor: { W: 2, U: 3, C: 1 }, floating: 1, poolKnown: true, unresolved: 1 }} />);
  expect(summary).toContain("Available · est.");
  expect(summary).toContain("White: 2");
  expect(summary).toContain("Blue: 3");
  expect(summary).toContain("Colorless: 1");
  expect(summary).toContain("1 conditional or unknown");
  expect(summary).toContain("colors overlap");
  const unknownPool = renderToStaticMarkup(<OverlayManaPanel mana={{ landsKnown: true, lands: 3, available: 3, byColor: {}, floating: 0, poolKnown: false, unresolved: 0 }} />);
  expect(unknownPool).toContain("Pool unknown");
});
