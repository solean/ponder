import { afterEach, beforeEach, expect, test } from "bun:test";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Window } from "happy-dom";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { ConfigResetNotice } from "../src/components/ConfigResetNotice";

const backupPath = "/Users/me/Library/Application Support/ponder/config.json.corrupt-1";
const resetStatus = {
  capabilities: { reveal: true },
  configReset: { backupPath, reason: "config.json is not valid JSON" },
};
let dismissSucceeds = false;
const originalFetch = globalThis.fetch;
const stubFetch = (async (input: RequestInfo | URL) => {
  const path = new URL(String(input), "http://localhost").pathname;
  if (path === "/api/runtime/status") return Response.json(resetStatus);
  if (path === "/api/runtime/reveal") {
    return Response.json({ error: "Couldn't show the file in Finder." }, { status: 500 });
  }
  if (path === "/api/runtime/config-reset/dismiss") {
    return dismissSucceeds
      ? Response.json({ capabilities: { reveal: true } })
      : Response.json({ error: "Couldn't save settings." }, { status: 500 });
  }
  return Response.json({});
}) as typeof fetch;

let dom: Window;
let root: Root;
let container: HTMLDivElement;
const originals = new Map<string, PropertyDescriptor | undefined>();

beforeEach(() => {
  dom = new Window({ url: "http://localhost/" });
  for (const [key, value] of Object.entries({ window: dom, document: dom.document, HTMLElement: dom.HTMLElement, Node: dom.Node, IS_REACT_ACT_ENVIRONMENT: true })) {
    originals.set(key, Object.getOwnPropertyDescriptor(globalThis, key));
    Object.defineProperty(globalThis, key, { value, configurable: true, writable: true });
  }
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
  globalThis.fetch = stubFetch;
  dismissSucceeds = false;
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

const settle = () => act(async () => { await new Promise((resolve) => setTimeout(resolve, 20)); });

test("a failed Show in Finder is explained on screen, not in a tooltip", async () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  await act(async () => root.render(<QueryClientProvider client={client}><ConfigResetNotice /></QueryClientProvider>));
  await settle();

  const reveal = Array.from(container.querySelectorAll("button")).find((b) => b.textContent === "Show in Finder");
  expect(reveal).toBeDefined();
  expect(container.querySelector(".error-state")).toBeNull();

  await act(async () => reveal!.click());
  await settle();

  const error = container.querySelector(".error-state--inline");
  expect(error?.getAttribute("role")).toBe("alert");
  expect(error?.textContent).toContain("Couldn't show the file in Finder.");
  expect(reveal!.getAttribute("title")).toBeNull();
  client.clear();
});

async function renderNotice() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  await act(async () => root.render(<QueryClientProvider client={client}><ConfigResetNotice /></QueryClientProvider>));
  await settle();
  return client;
}
const dismissButton = () => Array.from(container.querySelectorAll("button")).find((b) => b.textContent === "Dismiss");

test("a failed dismiss says so and leaves the notice up", async () => {
  const client = await renderNotice();

  await act(async () => dismissButton()!.click());
  await settle();

  const error = container.querySelector(".error-state--inline");
  expect(error?.getAttribute("role")).toBe("alert");
  expect(error?.textContent).toContain("Couldn't dismiss this notice.");
  expect(container.querySelector(".config-reset-notice")).not.toBeNull();
  expect(dismissButton()!.className).toContain("is-failed");
  client.clear();
});

test("a successful dismiss still hides the notice", async () => {
  dismissSucceeds = true;
  const client = await renderNotice();

  await act(async () => dismissButton()!.click());
  await settle();

  expect(container.querySelector(".config-reset-notice")).toBeNull();
  client.clear();
});
