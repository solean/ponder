import { afterEach, beforeEach, expect, mock, spyOn, test } from "bun:test";
import { Window } from "happy-dom";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
mock.module("../src/components/ManaSymbol", () => ({ ManaSymbol: () => null }));
const { MatchDetailPage } = await import("../src/pages/MatchDetailPage");
const { api } = await import("../src/lib/api");
let root: Root, dom: Window, container: HTMLDivElement, client: QueryClient;
let resolveReplay: (data: { id: number; gameNumber: number; turnNumber: number }[]) => void;
let spies: { mockRestore: () => void }[];
const originals = new Map<string, PropertyDescriptor | undefined>();
beforeEach(() => {
 dom = new Window({ url: "http://localhost/matches/55" });
 for (const [key, value] of Object.entries({ window: dom, document: dom.document, HTMLElement: dom.HTMLElement, Node: dom.Node, requestAnimationFrame: dom.requestAnimationFrame.bind(dom), cancelAnimationFrame: dom.cancelAnimationFrame.bind(dom), IS_REACT_ACT_ENVIRONMENT: true })) {
  originals.set(key, Object.getOwnPropertyDescriptor(globalThis, key));
  Object.defineProperty(globalThis, key, { value, configurable: true, writable: true });
 }
 client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } });
 container = document.createElement("div"); document.body.append(container); root = createRoot(container);
 spies = [
  spyOn(api, "matchDetail").mockResolvedValue({ match: { id: 55, bestOf: "bo3", result: "unknown", eventName: "", startedAt: "2026-10-23T00:00:00Z" }, opponentObservedCards: [], cardPlays: [], games: [], coverage: {} } as never),
  spyOn(api, "matchTimeline").mockResolvedValue([]),
  spyOn(api, "matchReplayStatus").mockResolvedValue({ revision: "one", complete: true }),
  spyOn(api, "matchReplay").mockImplementation(() => new Promise(resolve => { resolveReplay = resolve; })),
 ];
});
afterEach(async () => {
 act(() => root.unmount()); client.clear(); for (const spy of spies) spy.mockRestore(); await dom.happyDOM.close();
 for (const [key, value] of originals) { if (value) Object.defineProperty(globalThis, key, value); else Reflect.deleteProperty(globalThis, key); } originals.clear();
});
async function flush() { await act(async () => { await new Promise(resolve => setTimeout(resolve, 10)); }); }
async function render(search: string) {
 act(() => root.render(<QueryClientProvider client={client}><MemoryRouter initialEntries={[`/matches/55${search}`]}><Routes><Route path="matches/:matchId" element={<MatchDetailPage />} /></Routes></MemoryRouter></QueryClientProvider>));
 await flush(); await flush();
 expect(container.querySelector('[role="slider"]')).toBeNull();
 await act(async () => resolveReplay([{ id: 101, gameNumber: 1, turnNumber: 1 }, { id: 201, gameNumber: 2, turnNumber: 1 }, { id: 202, gameNumber: 2, turnNumber: 1 }].map(frame => ({ ...frame, changes: [{ instanceId: frame.id, cardId: 0, action: "attack", playerSide: "self" as const, isToken: false }] }))));
 await flush();
}
test("saved game 2 survives empty initial queries and async replay arrival", async () => {
 window.localStorage.setItem("ponder.replay.game.55", "2");
 await render("");
 expect(container.querySelector('[aria-label="Replay game selector"] [role="tab"][aria-selected="true"]')?.textContent).toBe("Game 2");
 expect(container.querySelector('[role="slider"]')?.getAttribute("aria-valuenow")).toBe("2");
});
test("chosen position survives returning from analytics and switching games", async () => {
 window.localStorage.setItem("ponder.replay.game.55", "2");
 await render("");
 act(() => container.querySelectorAll<HTMLButtonElement>(".match-replay-movelist-beat")[0]!.click());
 const section = (name: string) => [...container.querySelectorAll<HTMLButtonElement>('[role="tab"]')].find(button => button.textContent === name)!;
 act(() => section("Game Analytics").click());
 act(() => section("Replay").click());
 expect(container.querySelector('[role="slider"]')?.getAttribute("aria-valuenow")).toBe("1");
 const game = (number: number) => [...container.querySelectorAll<HTMLButtonElement>('[aria-label="Replay game selector"] button')].find(button => button.textContent === `Game ${number}`)!;
 act(() => game(1).click());
 act(() => game(2).click());
 expect(container.querySelector('[role="slider"]')?.getAttribute("aria-valuenow")).toBe("1");
});

test("missing locally saved game is not normalized to the first available game", async () => {
 window.localStorage.setItem("ponder.replay.game.55", "99");
 await render("");
 expect(container.textContent).toContain("no longer available");
 expect(container.querySelector('[role="slider"]')).toBeNull();
});


test("early first sightings cannot persist fallback mode before full replay arrives", async () => {
 spyOn(api, "matchTimeline").mockResolvedValue([{ id: 11, gameNumber: 1, turnNumber: 1, cardId: 0, playerSide: "self", firstPublicZone: "battlefield" }] as never);
 await render("");
 expect(window.localStorage.getItem("ponder.replay.kind.55.1")).toBe("frame");
 expect(container.querySelector('[role="slider"]')).not.toBeNull();
});

test("unavailable locally saved recording kind requires explicit recovery", async () => {
 window.localStorage.setItem("ponder.replay.kind.55.1", "observed");
 await render("");
 expect(container.textContent).toContain("no longer available");
 expect(container.querySelector('[role="slider"]')).toBeNull();
 act(() => [...container.querySelectorAll<HTMLAnchorElement>("a")].find(link => link.textContent === "Start from the first game")!.click());
 await flush(); await flush();
 expect(container.querySelector('[role="slider"]')).not.toBeNull();
 expect(window.localStorage.getItem("ponder.replay.kind.55.1")).toBe("frame");
});


test("explicit missing-frame recovery persists when returning from analytics", async () => {
 window.localStorage.setItem("ponder.replay.game.55", "2");
 window.localStorage.setItem("ponder.replay.position.55.2.frame", JSON.stringify({ frameId: 9999, speed: 1 }));
 await render("");
 expect(container.textContent).toContain("position is no longer available");
 act(() => [...container.querySelectorAll<HTMLButtonElement>("button")].find(button => button.textContent === "Start from the first step")!.click());
 const section = (name: string) => [...container.querySelectorAll<HTMLButtonElement>('[role="tab"]')].find(button => button.textContent === name)!;
 act(() => section("Game Analytics").click());
 act(() => section("Replay").click());
 expect(container.querySelector('[role="slider"]')?.getAttribute("aria-valuenow")).toBe("1");
 expect(container.textContent).not.toContain("position is no longer available");
});
