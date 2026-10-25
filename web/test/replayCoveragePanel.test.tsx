import { afterEach, beforeEach, expect, mock, test } from "bun:test";
import { MemoryRouter } from "react-router-dom";
import { Window } from "happy-dom";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
mock.module("../src/components/ManaSymbol", () => ({ ManaSymbol: () => null }));
const { MatchReplayPanel } = await import("../src/components/match-detail/MatchReplayPanel");
import type { MatchReplayView } from "../src/lib/replay/useMatchReplayView";
import type { useMatchReplay } from "../src/lib/replay/useMatchReplay";
import type { MatchGameTabState } from "../src/components/match-detail/MatchGameTabs";

let dom: Window;
let root: Root;
let container: HTMLDivElement;
const originals = new Map<string, PropertyDescriptor | undefined>();
beforeEach(() => {
  dom = new Window();
  for (const [key, value] of Object.entries({ window: dom, document: dom.document, HTMLElement: dom.HTMLElement, Node: dom.Node, requestAnimationFrame: dom.requestAnimationFrame.bind(dom), cancelAnimationFrame: dom.cancelAnimationFrame.bind(dom), IS_REACT_ACT_ENVIRONMENT: true })) {
    originals.set(key, Object.getOwnPropertyDescriptor(globalThis, key));
    Object.defineProperty(globalThis, key, { value, configurable: true, writable: true });
  }
  container = document.createElement("div"); document.body.append(container); root = createRoot(container);
});
afterEach(async () => {
  act(() => root.unmount()); await dom.happyDOM.close();
  for (const [key, descriptor] of originals) { if (descriptor) Object.defineProperty(globalThis, key, descriptor); else Reflect.deleteProperty(globalThis, key); }
  originals.clear();
});
const tabs: MatchGameTabState = {
  timelineGameNumbers: [1,2,3], activeTimelineGameNumber: 2, showTimelineGameTabs: true,
  activeTimelineGameTabID: "game-2", activeTimelineGamePanelID: "panel-2", timelineGameTabBaseId: "game",
  setSelectedTimelineGameNumber: () => {}, handleTimelineGameTabKeyDown: () => {},
};
function render(query: Partial<ReturnType<typeof useMatchReplay>> = {}, override: Partial<MatchReplayView> = {}, timelineError: Error | null = null) {
  const view: MatchReplayView = {
    timelineRows: [], replayFrames: [], hasReplayFrames: false, boardPreviewCards: [], boardPreviewByCardID: new Map(),
    isBoardCardPreviewLoading: false, timelineGameNumbers: [1,2,3], activeTimelineGameNumber: 2,
    activeTimelineGameAnalytics: null, activeReplayGroup: null, activeTimelineGroup: null, ...override,
  };
  const replayQuery: ReturnType<typeof useMatchReplay> = {
    data: [], matchComplete: true, isPending: false, error: null, isRetrying: false, retry: async () => {}, ...query,
  };
  act(() => root.render(<MemoryRouter><MatchReplayPanel matchId={12} bookmarkResult={{ bookmark: null, error: null }} applyBookmark={false} onBookmarkApplied={() => {}} bestOf="bo3" replayQuery={replayQuery} timelineError={timelineError} view={view} gameTabs={{ ...tabs, timelineGameNumbers: view.timelineGameNumbers }} /></MemoryRouter>));
}
test("historical empty game has permanent recovery copy and all BO3 tabs", () => {
  render();
  expect(container.querySelector('[data-recording-state="missing"]')?.textContent).toContain("Game 2");
  expect(container.textContent).toContain("Refreshing cannot recreate");
  expect(container.querySelectorAll('[role="tab"]').length).toBe(3);
  expect(container.textContent).not.toContain("yet");
});
test("load failures offer a working retry and do not claim the recording is missing", () => {
  const retry = mock(async () => {});
  render({ error: new Error("offline"), retry });
  expect(container.querySelector('[data-recording-state]')).toBeNull();
  const button = [...container.querySelectorAll("button")].find(button => button.textContent === "Retry loading replay")!;
  act(() => button.click()); expect(retry).toHaveBeenCalledTimes(1);
  render({ error: new Error("offline"), retry, isRetrying: true });
  expect([...container.querySelectorAll("button")].find(button => button.textContent === "Retrying…")?.disabled).toBe(true);
});
test("loading does not imply permanent absence", () => {
  render({ isPending: true, matchComplete: undefined });
  expect(container.textContent).toContain("Checking replay recording");
  expect(container.querySelector('[data-recording-state]')).toBeNull();
});
test("timeline-only selected game shows fallback even when another game has frames", () => {
  const play = { id: 1, cardId: 0, gameNumber: 2, playerSide: "self" as const, firstPublicZone: "battlefield" };
  render({}, { hasReplayFrames: true, replayFrames: [{ id: 1, gameNumber: 1 }], activeTimelineGroup: [2, [play]], timelineRows: [play] });
  expect(container.querySelector('[data-recording-state="fallback"]')?.textContent).toContain("First-sighting fallback");
  expect(container.textContent).toContain("later moves, hidden cards and the live stack");
  expect(container.querySelector('[aria-label="Game 2 replay controls"]')).not.toBeNull();
});
test("coverage notices expose complete, partial and actively recording states per selected game", () => {
  const frames = [
    { id: 1, gameNumber: 2, gameStateId: 1, gameStateType: "full", gameStage: "start" },
    { id: 2, gameNumber: 2, gameStateId: 2, prevGameStateId: 1, gameStage: "play" },
    { id: 3, gameNumber: 2, gameStateId: 3, prevGameStateId: 2, gameStage: "gameover" },
  ];
  render({}, { replayFrames: frames });
  expect(container.querySelector('[data-recording-state="complete"]')?.textContent).toContain("Game 2 · Complete recording");
  render({}, { replayFrames: frames.slice(1) });
  expect(container.querySelector('[data-recording-state="partial"]')?.textContent).toContain("game start is not verified");
  const live = frames.slice(0, 2).map(frame => ({ ...frame, recordedAt: new Date().toISOString() }));
  render({ matchComplete: false }, { replayFrames: live, timelineGameNumbers: [1,2] });
  expect(container.querySelector('[data-recording-state="recording"]')?.textContent).toContain("updates automatically");
});

test("open latest game waits without claiming recording is active or permanently missing", () => {
  render({ matchComplete: false }, { timelineGameNumbers: [1,2] });
  expect(container.querySelector('[data-recording-state="waiting"]')?.textContent).toContain("Waiting for recording");
  expect(container.textContent).not.toContain("Refreshing cannot recreate");
});

test("a sightings load failure cannot be described as permanent absence", () => {
  render({}, {}, new Error("timeline offline"));
  expect(container.querySelector('[data-recording-state]')).toBeNull();
  expect(container.textContent).toContain("Unable to load first public sightings");
});
