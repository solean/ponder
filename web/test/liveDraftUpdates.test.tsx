import { afterEach, beforeEach, expect, spyOn, test } from "bun:test";
import { Window } from "happy-dom";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { DraftCardBadges } from "../src/components/DraftCardBadges";
import { api } from "../src/lib/api";
import { DEFAULT_OVERLAY_SETTINGS } from "../src/lib/overlaySettings";
import type { LiveDraft } from "../src/lib/types";
import { useLiveDraft } from "../src/lib/useLiveDraft";

const pack: LiveDraft = {
  sessionId: 1, eventName: "PremierDraft_FRA", packNumber: 1, pickNumber: 1, waiting: false,
  pack: [{ cardId: 1, cardName: "First" }], displayPack: [{ cardId: 1, cardName: "First" }],
};
let dom: Window;
let root: Root;
let container: HTMLDivElement;
let client: QueryClient;
let fetchDraft: ReturnType<typeof spyOn<typeof api, "liveDraft">>;
let requests: { signal?: AbortSignal; resolve: (data: { draft: LiveDraft | null }) => void }[];
const originals = new Map<string, PropertyDescriptor | undefined>();

beforeEach(() => {
  dom = new Window();
  for (const [key, value] of Object.entries({ window: dom, document: dom.document, IS_REACT_ACT_ENVIRONMENT: true })) {
    originals.set(key, Object.getOwnPropertyDescriptor(globalThis, key));
    Object.defineProperty(globalThis, key, { value, configurable: true, writable: true });
  }
  client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity, staleTime: Infinity } } });
  client.setQueryData(["card-stats", pack.eventName], {
    status: "ok", meanGihWinRate: 0.55, cards: [{ arenaId: 1, gihWinRate: 0.6,
      gihGameCount: 1000, gameCount: 2000, gpWinRate: 0.55, iwd: 0.05, ata: 3, alsa: 4 }],
  });
  requests = [];
  fetchDraft = spyOn(api, "liveDraft").mockImplementation(signal => new Promise(resolve => {
    requests.push({ signal, resolve });
  }));
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
});

afterEach(async () => {
  act(() => root.unmount());
  client.clear();
  fetchDraft.mockRestore();
  await dom.happyDOM.close();
  for (const [key, descriptor] of originals) {
    if (descriptor) Object.defineProperty(globalThis, key, descriptor);
    else Reflect.deleteProperty(globalThis, key);
  }
  originals.clear();
});

function Harness() {
  const { data } = useLiveDraft();
  return data?.draft ? <DraftCardBadges draft={data.draft} settings={DEFAULT_OVERLAY_SETTINGS} /> : null;
}
function render() {
  act(() => root.render(<QueryClientProvider client={client}><Harness /></QueryClientProvider>));
}
async function flush() {
  await act(async () => { await new Promise(resolve => setTimeout(resolve, 10)); });
}
async function notify(draft: LiveDraft | null) {
  act(() => { window.dispatchEvent(new dom.CustomEvent("ponder:draft-changed", { detail: draft }) as unknown as Event); });
  await flush();
}

test("a pick clears badges before HTTP completes and cancels an older response", async () => {
  client.setQueryData(["live-draft"], { draft: pack });
  render();
  expect(container.querySelectorAll(".draft-card-badge")).toHaveLength(1);
  // A poll took a snapshot of the pack before the pick was committed.
  void client.invalidateQueries({ queryKey: ["live-draft"] });
  expect(requests).toHaveLength(1);
  const old = requests[0];
  const waiting = { ...pack, waiting: true, pack: [], displayPack: undefined };
  await notify(waiting);
  expect(old.signal?.aborted).toBe(true);
  expect(requests).toHaveLength(2);
  expect(container.querySelectorAll(".draft-card-badge")).toHaveLength(0);
  await act(async () => old.resolve({ draft: pack }));
  await flush();
  expect(client.getQueryData(["live-draft"])).toEqual({ draft: waiting });
  expect(container.querySelectorAll(".draft-card-badge")).toHaveLength(0);
});

test("the next pack shows badges only after its own display metadata arrives", async () => {
  client.setQueryData(["live-draft"], { draft: pack });
  render();
  const next = { ...pack, pickNumber: 2, displayPack: undefined };
  await notify(next);
  expect(container.querySelectorAll(".draft-card-badge")).toHaveLength(0);
  await act(async () => requests[0].resolve({ draft: { ...next, displayPack: pack.displayPack } }));
  await flush();
  expect(container.querySelectorAll(".draft-card-badge")).toHaveLength(1);
  await notify(null);
  expect(container.querySelectorAll(".draft-card-badge")).toHaveLength(0);
});

test("draft completion cancels even the initial fetch and removes its listener on unmount", async () => {
  render();
  const old = requests[0];
  await notify(null);
  expect(old.signal?.aborted).toBe(true);
  await act(async () => old.resolve({ draft: pack }));
  await flush();
  expect(client.getQueryData(["live-draft"])).toEqual({ draft: null });
  act(() => root.render(null));
  const count = requests.length;
  await notify(pack);
  expect(requests).toHaveLength(count);
  expect(client.getQueryData(["live-draft"])).toEqual({ draft: null });
});
