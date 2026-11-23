import { afterEach, beforeEach, expect, test } from "bun:test";
import { QueryClient, QueryClientProvider, useQuery } from "@tanstack/react-query";
import { Window } from "happy-dom";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { ApiError } from "../src/lib/apiError";
import { useQueryError, type QueryErrorView } from "../src/lib/useQueryError";

let dom: Window;
let root: Root;
const originals = new Map<string, PropertyDescriptor | undefined>();

beforeEach(() => {
  dom = new Window();
  for (const [key, value] of Object.entries({ window: dom, document: dom.document, HTMLElement: dom.HTMLElement, Node: dom.Node, IS_REACT_ACT_ENVIRONMENT: true })) {
    originals.set(key, Object.getOwnPropertyDescriptor(globalThis, key));
    Object.defineProperty(globalThis, key, { value, configurable: true, writable: true });
  }
  const container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
});

afterEach(async () => {
  act(() => root.unmount());
  await dom.happyDOM.close();
  for (const [key, descriptor] of originals) {
    if (descriptor) Object.defineProperty(globalThis, key, descriptor);
    else Reflect.deleteProperty(globalThis, key);
  }
  originals.clear();
});

const settle = () => act(async () => { await new Promise((resolve) => setTimeout(resolve, 10)); });

test("keeps the error through background refetches, counts them, and resets after a success", async () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  let outcome: "fail" | "succeed" = "fail";
  let release: (() => void) | null = null;
  let latest: QueryErrorView | null = null;

  function Probe() {
    const query = useQuery({
      queryKey: ["probe"],
      queryFn: async () => {
        if (release === null) await new Promise<void>((resolve) => { release = resolve; });
        release = null;
        if (outcome === "fail") throw new ApiError(500, "Couldn't load matches.");
        return "ok";
      },
    });
    latest = useQueryError(query);
    return null;
  }

  // First load fails.
  await act(async () => root.render(<QueryClientProvider client={client}><Probe /></QueryClientProvider>));
  await act(async () => release?.());
  await settle();
  expect(latest!.error?.message).toBe("Couldn't load matches.");
  expect(latest!.tries).toBe(1);

  // A background refetch (no click): the error stays up and shows as retrying.
  void client.refetchQueries({ queryKey: ["probe"] });
  await settle();
  expect(latest!.error?.message).toBe("Couldn't load matches.");
  expect(latest!.retrying).toBe(true);
  await act(async () => release?.());
  await settle();
  expect(latest!.retrying).toBe(false);
  expect(latest!.tries).toBe(2);

  // It recovers: no error, count reset.
  outcome = "succeed";
  void client.refetchQueries({ queryKey: ["probe"] });
  await settle();
  await act(async () => release?.());
  await settle();
  expect(latest!.error).toBeNull();

  // A new failure starts counting from 1 again.
  outcome = "fail";
  void client.refetchQueries({ queryKey: ["probe"] });
  await settle();
  await act(async () => release?.());
  await settle();
  expect(latest!.error?.message).toBe("Couldn't load matches.");
  expect(latest!.tries).toBe(1);
  client.clear();
});
