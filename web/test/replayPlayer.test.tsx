import { MemoryRouter, useLocation } from "react-router-dom";
import { ReplaySavedBookmarks } from "../src/components/match-detail/ReplaySavedBookmarks";
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

test("copies stay visible and labeled in replay cards and observed replays", async () => {
  const { MatchReplayObjectCard } = await import("../src/components/match-detail/CardPreviews");
  const object = {
    id: 1, frameId: 1, instanceId: 236, cardId: 106300, cardName: "Omit Variables",
    playerSide: "self" as const, zoneType: "stack", isCopy: true, castConfirmed: true,
    isToken: false, isTapped: false, hasSummoningSickness: false,
  };
  act(() => root.render(<MatchReplayObjectCard object={object} preview={null} size="stack" />));
  expect(container.querySelector(".match-replay-card-chip")?.textContent).toContain("Copy");
  expect(container.textContent).toContain("Omit Variables");
  act(() => root.render(<MatchTimelineBoard gameNumber={1} plays={[{ ...object, firstPublicZone: "stack" }]} previewByCardID={new Map()} />));
  expect(container.querySelector(".match-replay-card-chip")?.textContent).toContain("Copy");
  expect(container.textContent).toContain("Cast a copy");
});

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

test("expanding full event details preserves playback and selected original frame", () => {
  const changes = ["Alpha", "Beta"].map((cardName, index) => ({ instanceId: index + 1, cardId: index + 1, cardName, playerSide: "self" as const, action: "attack", isToken: false }));
  act(() => root.render(<MatchReplayFrameBoard gameNumber={1} frames={[{ id: 1, turnNumber: 1, changes }, { id: 2, turnNumber: 1 }]} gameSummary={null} previewByCardID={new Map()} />));
  act(() => transport().click());
  expect(position()).toBe(1);
  const details = container.querySelector<HTMLDetailsElement>(".match-replay-event-details")!;
  act(() => details.querySelector("summary")!.click());
  expect(details.open).toBe(true);
  expect(details.textContent).toContain("Alpha");
  expect(details.textContent).toContain("Beta");
  expect(position()).toBe(1);
  expect(transport().textContent).toContain("Pause");
  expect(timers.size).toBe(1);
});

test("opponent hand renders only identities known in the selected snapshot", async () => {
  const { MatchReplayFrameSideSummary } = await import("../src/components/match-detail/ReplayZones");
  const base = { id: 1, frameId: 1, instanceId: 1, cardId: 100, cardName: "Known Island", playerSide: "opponent" as const, zoneType: "hand", isToken: false, isTapped: false, hasSummoningSickness: false };
  const renderHand = (visibility: string, cardId = 100) => act(() => root.render(
    <MatchReplayFrameSideSummary side="opponent" objects={[{ ...base, visibility, cardId }]} variant="rail" previewByCardID={new Map()} />,
  ));
  renderHand("public");
  expect(container.querySelector('a[aria-label="Open Known Island on Scryfall"]')).not.toBeNull();
  renderHand("known");
  expect(container.querySelector('a[aria-label="Open Known Island on Scryfall"]')).not.toBeNull();
  renderHand("private");
  expect(container.querySelector("a")).toBeNull();
  expect(container.querySelector('[aria-label="Face-down card"]')).not.toBeNull();
  renderHand("private", 0);
  expect(container.textContent).not.toContain("Known Island");
  expect(container.querySelector('[aria-label="Face-down card"]')).not.toBeNull();
});


for (const mode of ["frames", "observed"] as const) {
  for (const zone of ["graveyard", "exile"] as const) {
    for (const speed of [1, 4]) {
      test(`${mode} ${zone} inspection pauses at ${speed}× until explicitly resumed`, () => {
        const rows = Array.from({ length: 3 }, (_, i) => ({ id: i + 1, turnNumber: i + 1 }));
        const object = { id: 1, frameId: 1, instanceId: 1, cardId: 100, cardName: "Inspected card", playerSide: "self" as const, zoneType: zone, isToken: false, isTapped: false, hasSummoningSickness: false };
        act(() => root.render(mode === "frames"
          ? <MatchReplayFrameBoard gameNumber={1} frames={rows.map(row => ({ ...row, objects: [object] }))} gameSummary={null} previewByCardID={new Map()} />
          : <MatchTimelineBoard gameNumber={1} plays={rows.map(row => ({ ...row, instanceId: row.id, cardId: 100, cardName: "Inspected card", playerSide: "self", firstPublicZone: zone }))} previewByCardID={new Map()} />));
        const speedButton = [...container.querySelectorAll<HTMLButtonElement>("button")].find(button => button.textContent === `${speed}×`)!;
        act(() => speedButton.click());
        act(() => transport().click());
        tick();
        expect(position()).toBe(2);
        expect([...timers.values()][0]!.delay).toBe(1200 / speed);
        const zoneButton = [...container.querySelectorAll<HTMLButtonElement>('button[aria-haspopup="dialog"]')].find(button => button.getAttribute("aria-label")?.includes(`You ${zone}`))!;
        act(() => zoneButton.click());
        expect(document.querySelector('[role="dialog"]')).not.toBeNull();
        const inspectedText = document.querySelector('[role="dialog"]')?.textContent;
        expect(inspectedText).toContain(mode === "frames" ? "this step" : "in this game");
        expect(position()).toBe(2);
        expect(transport().textContent).toContain("Play");
        expect(timers.size).toBe(0);
        for (const key of ["ArrowRight", " ", "Home", "End"]) {
          act(() => document.body.dispatchEvent(new dom.KeyboardEvent("keydown", { key, bubbles: true }) as unknown as Event));
        }
        expect(position()).toBe(2);
        expect(timers.size).toBe(0);
        expect(document.querySelector('[role="dialog"]')?.textContent).toBe(inspectedText);
        act(() => document.querySelector<HTMLButtonElement>(".match-replay-zone-dialog-close")!.click());
        expect(document.querySelector('[role="dialog"]')).toBeNull();
        expect(position()).toBe(2);
        expect(timers.size).toBe(0);
        act(() => transport().click());
        expect([...timers.values()][0]!.delay).toBe(1200 / speed);
        tick();
        expect(position()).toBe(3);
      });
    }
  }
}
function renderSavedReplay(game: number, ids: number[], bookmarkFrameId?: number) {
  act(() => root.render(<MatchReplayFrameBoard key={game} matchId={55} gameNumber={game} frames={ids.map(id => ({ id, turnNumber: 1 }))} gameSummary={null} previewByCardID={new Map()} bookmarkFrameId={bookmarkFrameId} />));
}
function seekReplayStep(index: number) {
  act(() => container.querySelectorAll<HTMLButtonElement>(".match-replay-movelist-beat")[index]!.click());
}

test("local position and speed survive game switches and section remounts", () => {
  renderSavedReplay(1, [11, 12, 13]);
  seekReplayStep(1);
  act(() => [...container.querySelectorAll<HTMLButtonElement>("button")].find(button => button.textContent === "2×")!.click());
  renderSavedReplay(2, [21, 22]);
  expect(position()).toBe(2);
  renderSavedReplay(1, [11, 12, 13]);
  expect(position()).toBe(2);
  expect(container.querySelector('.match-replay-speed-button[aria-pressed="true"]')?.textContent).toBe("2×");
  act(() => root.render(<div>Analytics</div>));
  renderSavedReplay(1, [11, 12, 13]);
  expect(position()).toBe(2);
  expect(transport().textContent).toContain("Play");
});

test("saved identity survives live append and insertion before current step", () => {
  renderSavedReplay(1, [11, 12, 13]);
  seekReplayStep(1);
  renderSavedReplay(1, [11, 12, 13, 14]);
  expect(position()).toBe(2);
  renderSavedReplay(1, [10, 11, 12, 13, 14]);
  expect(position()).toBe(3);
  expect(container.querySelector('.match-replay-bookmark a')?.getAttribute("href")).toContain("replayFrame=12");
});

test("bookmark overrides saved position and opens paused at exact stable frame", () => {
  window.localStorage.setItem("ponder.replay.position.55.1.frame", JSON.stringify({ frameId: 13, speed: 4 }));
  renderSavedReplay(1, [11, 12, 13], 12);
  expect(position()).toBe(2);
  expect(transport().textContent).toContain("Play");
  expect(timers.size).toBe(0);
});

test("deleted bookmark shows an explicit message instead of an unrelated board", () => {
  renderSavedReplay(1, [11, 12, 13], 999);
  expect(container.textContent).toContain("no longer available");
  expect(container.querySelector('[role="slider"]')).toBeNull();
  act(() => [...container.querySelectorAll<HTMLButtonElement>("button")].find(button => button.textContent?.includes("Start from"))!.click());
  expect(position()).toBe(1);
});

test("missing saved identity remains unavailable after live frames arrive", () => {
  window.localStorage.setItem("ponder.replay.position.55.1.frame", JSON.stringify({ frameId: 99, speed: 1 }));
  renderSavedReplay(1, [11, 12]);
  renderSavedReplay(1, [11, 12, 13]);
  expect(container.textContent).toContain("no longer available");
  expect(container.querySelector('[role="slider"]')).toBeNull();
});

test("observed fallback stores its own stable action identity and speed", () => {
  const render = (game: number) => act(() => root.render(<MatchTimelineBoard key={game} matchId={55} gameNumber={game} plays={[1, 2, 3].map(id => ({ id: game * 10 + id, instanceId: id, cardId: 0, playerSide: "unknown", firstPublicZone: "hand", turnNumber: 1 }))} previewByCardID={new Map()} />));
  render(1);
  act(() => document.body.dispatchEvent(new dom.KeyboardEvent("keydown", { key: "Home", bubbles: true }) as unknown as Event));
  render(2);
  render(1);
  expect(position()).toBe(1);
});

test("save bookmark records stable identity locally and does not change playback", () => {
  renderSavedReplay(1, [11, 12, 13]);
  act(() => transport().click());
  act(() => [...container.querySelectorAll<HTMLButtonElement>("button")].find(button => button.textContent === "Save bookmark")!.click());
  expect(position()).toBe(1);
  expect(transport().textContent).toContain("Pause");
  const saved = JSON.parse(window.localStorage.getItem("ponder.replay.bookmarks.55")!);
  expect(saved).toEqual([{ matchId: 55, game: 1, frameId: 11, kind: "frame" }]);
  expect(container.textContent).toContain("saved on this device");
});

function BookmarkLocation() {
 const location = useLocation();
 return <output>{location.pathname}{location.search}</output>;
}
test("saved bookmark list opens a private replay using the app router", () => {
 window.localStorage.setItem("ponder.replay.bookmarks.55", JSON.stringify([{ matchId: 55, game: 2, frameId: 987, kind: "frame" }]));
 act(() => root.render(<MemoryRouter initialEntries={["/matches/55"]}><ReplaySavedBookmarks matchId={55} /><BookmarkLocation /></MemoryRouter>));
 const open = [...container.querySelectorAll<HTMLButtonElement>("button")].find(button => button.textContent?.includes("Frame 987"))!;
 act(() => open.click());
 expect(container.querySelector("output")?.textContent).toBe("/matches/55?replayGame=2&replayFrame=987&replayKind=frame");
});
