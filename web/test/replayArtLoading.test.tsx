import { afterEach, beforeEach, describe, expect, spyOn, test } from "bun:test";
import { Window } from "happy-dom";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import * as scryfall from "../src/lib/scryfall";
import { useMatchReplayView } from "../src/lib/replay/useMatchReplayView";
import type { MatchCardPlay, MatchReplayFrameObject } from "../src/lib/types";

let root: Root;
let dom: Window;
let client: QueryClient;
let container: HTMLDivElement;
let fetchPreview: ReturnType<typeof spyOn<typeof scryfall, "fetchCardPreview">>;
let settle: (value: scryfall.CardPreview | null) => void;
let reject: (reason: Error) => void;
const originals = new Map<string, PropertyDescriptor | undefined>();

beforeEach(() => {
  dom = new Window();
  for (const [key, value] of Object.entries({ window: dom, document: dom.document, IS_REACT_ACT_ENVIRONMENT: true })) {
    originals.set(key, Object.getOwnPropertyDescriptor(globalThis, key));
    Object.defineProperty(globalThis, key, { value, configurable: true, writable: true });
  }
  client = new QueryClient({ defaultOptions: { queries: { retryDelay: 0, gcTime: 0 } } });
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
  fetchPreview = spyOn(scryfall, "fetchCardPreview").mockImplementation(() => new Promise((resolve, fail) => {
    settle = resolve;
    reject = fail;
  }));
});

afterEach(async () => {
  act(() => root.unmount());
  client.clear();
  fetchPreview.mockRestore();
  await dom.happyDOM.close();
  for (const [key, descriptor] of originals) {
    if (descriptor) Object.defineProperty(globalThis, key, descriptor);
    else Reflect.deleteProperty(globalThis, key);
  }
  originals.clear();
});

const object = (cardId: number): MatchReplayFrameObject => ({
  id: cardId + 1, frameId: 1, instanceId: cardId + 1, cardId,
  playerSide: cardId ? "self" : "opponent", zoneType: "hand",
  isToken: false, isTapped: false, hasSummoningSickness: false,
});
const play = (cardId: number): MatchCardPlay => ({
  id: cardId + 1, instanceId: cardId + 1, cardId,
  playerSide: "opponent", firstPublicZone: "hand",
});

for (const mode of ["frames", "observed"] as const) {
  describe(`${mode} replay art loading`, () => {
    let view: ReturnType<typeof useMatchReplayView>;
    function render(cardIds: number[]) {
      function Harness() {
        view = useMatchReplayView({
          timelineRows: mode === "observed" ? cardIds.map(play) : [],
          replayFrames: mode === "frames" ? [{ id: 1, objects: cardIds.map(object) }] : [],
          matchResult: "unknown", games: [], selectedTimelineGameNumber: null,
        });
        return <p>{view.isBoardCardPreviewLoading ? "Loading replay card art…" : "Ready"}</p>;
      }
      act(() => root.render(<QueryClientProvider client={client}><Harness /></QueryClientProvider>));
    }
    async function flush() {
      await act(async () => { await new Promise(resolve => setTimeout(resolve, 10)); });
    }

    test("hidden cards have no art requests or loading indicator", () => {
      render([0, -1]);
      expect(view.boardPreviewCards).toEqual([]);
      expect(fetchPreview).not.toHaveBeenCalled();
      expect(container.textContent).toBe("Ready");
    });
    test("loading clears when known art loads alongside a hidden card", async () => {
      render([0, 100, 100]);
      expect(container.textContent).toContain("Loading");
      expect(view.boardPreviewCards.map(card => card.cardId)).toEqual([100]);
      expect(fetchPreview).toHaveBeenCalledTimes(1);
      await act(async () => settle({ name: "Forest", imageUrl: "/forest.jpg" }));
      await flush();
      expect(container.textContent).toBe("Ready");
      expect(view.boardPreviewByCardID.get(100)?.name).toBe("Forest");
      expect(view.boardPreviewByCardID.get(0)).toBeUndefined();
    });
    test("unavailable art settles without a perpetual loading indicator", async () => {
      render([0, 100]);
      await act(async () => settle(null));
      await flush();
      expect(container.textContent).toBe("Ready");
      expect(view.boardPreviewByCardID.get(100)).toBeNull();
    });
    test("failed art clears loading after the retry finishes", async () => {
      render([0, 100]);
      await act(async () => reject(new Error("offline")));
      await flush();
      expect(fetchPreview).toHaveBeenCalledTimes(2);
      await act(async () => reject(new Error("still offline")));
      await flush();
      expect(container.textContent).toBe("Ready");
      expect(view.boardPreviewByCardID.get(100)).toBeNull();
    });
  });
}
