import { afterEach, beforeEach, expect, mock, test } from "bun:test";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Window } from "happy-dom";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { MemoryRouter, Route, Routes } from "react-router-dom";

mock.module("../src/components/ManaSymbol", () => ({ ManaSymbol: () => null }));

// Every API request returns an empty list except the paths a test makes fail.
// Stubbing fetch (not the api module) keeps the stub out of other test files.
const failures = new Map<string, { status: number; error: string }>();
const requests: string[] = [];
// When set, failing requests wait for this before answering.
let gate: Promise<void> | null = null;
const originalFetch = globalThis.fetch;
const stubFetch = (async (input: RequestInfo | URL) => {
  const path = new URL(String(input), "http://localhost").pathname;
  requests.push(path);
  const failure = failures.get(path);
  if (failure) {
    if (gate) await gate;
    return Response.json({ error: failure.error }, { status: failure.status });
  }
  return Response.json([]);
}) as typeof fetch;
const { DecksPage } = await import("../src/pages/DecksPage");
const { DeckDetailPage } = await import("../src/pages/DeckDetailPage");
const { DeckMatchupsPanel } = await import("../src/components/MatchupPanels");
const { DraftPoolPanel } = await import("../src/components/DraftPoolPanel");

let dom: Window;
let root: Root;
let container: HTMLDivElement;
const originals = new Map<string, PropertyDescriptor | undefined>();

beforeEach(() => {
  dom = new Window({ url: "http://localhost/" });
  for (const [key, value] of Object.entries({ window: dom, document: dom.document, navigator: dom.navigator, HTMLElement: dom.HTMLElement, Node: dom.Node, localStorage: dom.localStorage, getComputedStyle: dom.getComputedStyle.bind(dom), requestAnimationFrame: dom.requestAnimationFrame.bind(dom), cancelAnimationFrame: dom.cancelAnimationFrame.bind(dom), IS_REACT_ACT_ENVIRONMENT: true })) {
    originals.set(key, Object.getOwnPropertyDescriptor(globalThis, key));
    Object.defineProperty(globalThis, key, { value, configurable: true, writable: true });
  }
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
  failures.clear();
  requests.length = 0;
  gate = null;
  globalThis.fetch = stubFetch;
});

afterEach(async () => {
  act(() => root.unmount());
  await dom.happyDOM.close();
  for (const [key, descriptor] of originals) {
    if (descriptor) Object.defineProperty(globalThis, key, descriptor);
    else Reflect.deleteProperty(globalThis, key);
  }
  originals.clear();
  globalThis.fetch = originalFetch;
});

async function renderAt(path: string, routePath: string, element: React.ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    root.render(
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={[path]}>
          <Routes>
            <Route path={routePath} element={element} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    );
  });
  // Let the failed query settle and re-render.
  for (let i = 0; i < 5 && !container.querySelector('[role="alert"]'); i++) {
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 10)); });
  }
}

test("a page that fails to load keeps its header and offers Try again", async () => {
  failures.set("/api/decks", { status: 500, error: "Couldn't load decks." });
  await renderAt("/decks", "/decks", <DecksPage />);

  expect(container.querySelector(".page-header-title")?.textContent).toBe("Decks");
  const alert = container.querySelector('[role="alert"]');
  expect(alert?.className).toContain("error-state--page");
  expect(alert?.textContent).toContain("Couldn't load decks.");
  expect(Array.from(container.querySelectorAll("button")).map((b) => b.textContent)).toContain("Try again");
});

test("a deck the API doesn't have says so and links back to the list", async () => {
  failures.set("/api/decks/999999999", { status: 404, error: "deck not found" });
  await renderAt("/decks/999999999", "/decks/:deckId", <DeckDetailPage />);

  const alert = container.querySelector('[role="alert"]');
  expect(alert?.textContent).toContain("This deck doesn't exist.");
  expect(alert?.textContent).toContain("Ponder has no record of deck #999999999.");
  expect(alert?.textContent).not.toContain("deck not found");
  expect(container.querySelector(".error-state__actions a")?.getAttribute("href")).toBe("/decks");
});

test("a deck ID that isn't a number gets the same message", async () => {
  await renderAt("/decks/abc", "/decks/:deckId", <DeckDetailPage />);

  expect(container.querySelector('[role="alert"]')?.textContent).toContain("This deck doesn't exist.");
});

test("Try again keeps the error on screen and counts the tries", async () => {
  failures.set("/api/decks", { status: 500, error: "Couldn't load decks." });
  await renderAt("/decks", "/decks", <DecksPage />);
  expect(container.textContent).not.toContain("Still failing");

  // Hold the retry's response so the in-between state can be checked.
  let release = () => {};
  gate = new Promise((resolve) => { release = resolve; });
  const tryAgain = () => Array.from(container.querySelectorAll("button")).find((b) => b.textContent?.includes("Try again"));
  await act(async () => tryAgain()?.click());
  // React Query delivers state changes on a setTimeout(0) scheduler.
  await act(async () => { await new Promise((resolve) => setTimeout(resolve, 10)); });

  // React Query v5 resets a query with no data to pending while it refetches.
  // The page must keep showing the error, not flip back to its loading state.
  expect(container.querySelector(".loading-page")).toBeNull();
  expect(container.querySelector(".page-header-title")?.textContent).toBe("Decks");
  expect(container.querySelector('[role="alert"] button')?.textContent).toBe("Trying again…");

  await act(async () => { release(); await new Promise((resolve) => setTimeout(resolve, 20)); });
  expect(tryAgain()?.textContent).toBe("Try again");
  expect(container.textContent).toContain("Still failing after 2 tries.");
  expect(requests.filter((path) => path === "/api/decks")).toHaveLength(2);
});

test("a panel that fails keeps its heading and shows the panel-size error", async () => {
  failures.set("/api/decks/7/matchups", { status: 500, error: "Couldn't load matchups." });
  await renderAt("/decks/7", "/decks/:deckId", <DeckMatchupsPanel deckId={7} />);

  expect(container.querySelector(".panel-head h3")?.textContent).toBe("Matchups");
  const alert = container.querySelector('[role="alert"]');
  expect(alert?.className).toContain("error-state--panel");
  expect(alert?.textContent).toContain("Couldn't load matchups.");
});

test("the draft pool says the deck failed to load, not that Arena never sent one", async () => {
  failures.set("/api/decks", { status: 500, error: "Couldn't load decks." });
  const pick = { id: 1, packNumber: 1, pickNumber: 1, pickedCardIds: "101", packCardIds: "101", pickTs: "2026-11-01T00:00:00Z" };
  await renderAt("/drafts/1", "/drafts/:draftId", <DraftPoolPanel eventName="PremierDraft_TST" picks={[pick]} />);

  expect(container.querySelector('[role="alert"]')?.textContent).toContain("Couldn't load the submitted deck.");
  expect(container.textContent).not.toContain("has not reported a submitted deck");
});
