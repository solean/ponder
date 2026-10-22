import { afterEach, beforeEach, describe, expect, mock, spyOn, test } from "bun:test";
import { Window } from "happy-dom";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
// Vite's asset glob is unavailable in Bun; mana art is outside transport behavior.
mock.module("../src/components/ManaSymbol", () => ({ ManaSymbol: () => null }));
const { MatchReplayFrameBoard } = await import("../src/components/match-detail/MatchReplayFrameBoard");
const { MatchTimelineBoard } = await import("../src/components/match-detail/MatchTimelineBoard");

let root: Root;
let container: HTMLDivElement;
let dom: Window;
let timers: Map<number, { callback: () => void; delay: number }>;
let restoreTimers: () => void;
const originals = new Map<string, PropertyDescriptor | undefined>();

beforeEach(() => {
  dom = new Window();
  const globals = {
    window: dom, document: dom.document, HTMLElement: dom.HTMLElement,
    Node: dom.Node, requestAnimationFrame: dom.requestAnimationFrame.bind(dom),
    cancelAnimationFrame: dom.cancelAnimationFrame.bind(dom), IS_REACT_ACT_ENVIRONMENT: true,
  };
  for (const [key, value] of Object.entries(globals)) {
    originals.set(key, Object.getOwnPropertyDescriptor(globalThis, key));
    Object.defineProperty(globalThis, key, { value, configurable: true, writable: true });
  }
  timers = new Map();
  let nextID = 0;
  const set = spyOn(window, "setTimeout").mockImplementation((callback, delay) => {
    const id = ++nextID;
    timers.set(id, { callback: callback as () => void, delay: delay ?? 0 });
    return id;
  });
  const clear = spyOn(window, "clearTimeout").mockImplementation((id) => {
    timers.delete(id!);
  });
  restoreTimers = () => { set.mockRestore(); clear.mockRestore(); };
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
});

afterEach(async () => {
  act(() => root.unmount());
  restoreTimers();
  await dom.happyDOM.close();
  for (const [key, descriptor] of originals) {
    if (descriptor) Object.defineProperty(globalThis, key, descriptor);
    else Reflect.deleteProperty(globalThis, key);
  }
  originals.clear();
});

function transport() {
  return container.querySelector<HTMLButtonElement>(".match-replay-button.is-primary")!;
}
function position() {
  return Number(container.querySelector('[role="slider"]')?.getAttribute("aria-valuenow"));
}
function tick() {
  expect(timers.size).toBe(1);
  const [id, timer] = [...timers][0]!;
  timers.delete(id);
  act(() => timer.callback());
}

for (const mode of ["frames", "observed"] as const) {
  describe(`${mode} replay transport`, () => {
    function render(length: number) {
      const rows = Array.from({ length }, (_, i) => ({ id: i + 1, turnNumber: i + 1 }));
      act(() => root.render(mode === "frames"
        ? <MatchReplayFrameBoard gameNumber={1} frames={rows} gameSummary={null} previewByCardID={new Map()} />
        : <MatchTimelineBoard gameNumber={1} plays={rows.map(row => ({ ...row, instanceId: row.id, cardId: 0, playerSide: "unknown", firstPublicZone: "hand" }))} previewByCardID={new Map()} />));
    }

    test("Replay restarts a completed game, advances, pauses/resumes, and stops at the end", () => {
      render(3);
      expect(position()).toBe(3);
      expect(transport().textContent).toContain("Replay");
      act(() => transport().click());
      expect(position()).toBe(1);
      expect(transport().textContent).toContain("Pause");
      expect([...timers.values()][0]!.delay).toBe(1200);
      tick();
      expect(position()).toBe(2);
      act(() => transport().click());
      expect(transport().textContent).toContain("Play");
      expect(position()).toBe(2);
      expect(timers.size).toBe(0);
      act(() => transport().click());
      expect(position()).toBe(2);
      tick();
      expect(position()).toBe(3);
      expect(transport().textContent).toContain("Replay");
      expect(timers.size).toBe(0);
    });

    test("Space restarts at the selected speed", () => {
      render(3);
      const speed = [...container.querySelectorAll<HTMLButtonElement>("button")].find(b => b.textContent === "4×")!;
      act(() => speed.click());
      act(() => document.body.dispatchEvent(new dom.KeyboardEvent("keydown", { key: " ", bubbles: true }) as unknown as Event));
      expect(position()).toBe(1);
      expect([...timers.values()][0]!.delay).toBe(300);
      tick();
      expect(position()).toBe(2);
    });

    test("one step cannot start playback via button or shortcut", () => {
      render(1);
      expect(transport().disabled).toBe(true);
      act(() => transport().click());
      act(() => document.body.dispatchEvent(new dom.KeyboardEvent("keydown", { key: " ", bubbles: true }) as unknown as Event));
      expect(position()).toBe(1);
      expect(timers.size).toBe(0);
    });

    test("empty replay has no transport or timer", () => {
      render(0);
      expect(transport()).toBeNull();
      expect(timers.size).toBe(0);
    });
  });
}

test("decisions-only playback skips routine frames while original steps remain available", () => {
  const frames = [
    { id: 1, turnNumber: 1 },
    { id: 2, turnNumber: 1, changes: [{ instanceId: 9, cardId: 9, cardName: "Forest", action: "tap", playerSide: "self" as const, isToken: false }] },
    { id: 3, turnNumber: 1 },
  ];
  act(() => root.render(<MatchReplayFrameBoard gameNumber={1} frames={frames} gameSummary={null} previewByCardID={new Map()} />));
  act(() => container.querySelector<HTMLInputElement>(".match-replay-decision-mode input")!.click());
  act(() => transport().click());
  expect(position()).toBe(1);
  tick();
  expect(position()).toBe(3);
  const routine = [...container.querySelectorAll<HTMLButtonElement>(".match-replay-movelist-beat")].find(button => button.textContent?.includes("Forest"))!;
  act(() => routine.click());
  expect(position()).toBe(2);
});
