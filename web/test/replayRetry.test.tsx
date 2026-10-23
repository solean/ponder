import { afterEach, beforeEach, expect, spyOn, test } from "bun:test";
import { Window } from "happy-dom";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { api } from "../src/lib/api";
import { useMatchReplay } from "../src/lib/replay/useMatchReplay";

let dom: Window;
let root: Root;
let client: QueryClient;
let status: ReturnType<typeof spyOn<typeof api, "matchReplayStatus">>;
let payload: ReturnType<typeof spyOn<typeof api, "matchReplay">>;
let query: ReturnType<typeof useMatchReplay>;
const originals = new Map<string, PropertyDescriptor | undefined>();
beforeEach(() => {
  dom = new Window();
  for (const [key, value] of Object.entries({ window: dom, document: dom.document, IS_REACT_ACT_ENVIRONMENT: true })) {
    originals.set(key, Object.getOwnPropertyDescriptor(globalThis, key));
    Object.defineProperty(globalThis, key, { value, configurable: true, writable: true });
  }
  client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  const container = document.createElement("div"); document.body.append(container); root = createRoot(container);
  status = spyOn(api, "matchReplayStatus"); payload = spyOn(api, "matchReplay");
});
afterEach(async () => {
  act(() => root.unmount()); client.clear(); status.mockRestore(); payload.mockRestore(); await dom.happyDOM.close();
  for (const [key, descriptor] of originals) { if (descriptor) Object.defineProperty(globalThis, key, descriptor); else Reflect.deleteProperty(globalThis, key); }
  originals.clear();
});
function Harness() { query = useMatchReplay(25, true); return null; }
async function flush() { await act(async () => { await new Promise(resolve => setTimeout(resolve, 20)); }); }
function render() { act(() => root.render(<QueryClientProvider client={client}><Harness /></QueryClientProvider>)); }
test("retry recovers a failed status request and loads frames", async () => {
  status.mockRejectedValueOnce(new Error("offline")).mockResolvedValue({ revision: "one", complete: true });
  payload.mockResolvedValue([{ id: 1 }]);
  render(); await flush();
  expect(query.error?.message).toBe("offline"); expect(query.isPending).toBe(false);
  expect(payload).not.toHaveBeenCalled();
  const invalidations = spyOn(client, "invalidateQueries");
  await act(async () => { await query.retry(); }); await flush();
  expect(invalidations.mock.calls.some(([filters]) => filters?.queryKey?.[0] === "match-timeline")).toBe(true);
  invalidations.mockRestore();
  expect(query.error).toBeNull(); expect(query.data).toEqual([{ id: 1 }]);
  expect(query.matchComplete).toBe(true);
});
test("retry explicitly retries the same failed payload revision", async () => {
  status.mockResolvedValue({ revision: "same", complete: true });
  payload.mockRejectedValueOnce(new Error("payload offline")).mockResolvedValue([{ id: 2 }]);
  render(); await flush(); await flush();
  expect(query.error?.message).toBe("payload offline");
  await act(async () => { await query.retry(); }); await flush();
  expect(query.error).toBeNull(); expect(query.data).toEqual([{ id: 2 }]);
  expect(payload.mock.calls.length).toBeGreaterThanOrEqual(2);
});
