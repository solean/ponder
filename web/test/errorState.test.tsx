import { afterEach, beforeEach, expect, test } from "bun:test";
import { Window } from "happy-dom";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { ErrorState, QueryErrorState, RESTART_HINT, type FailedQuery } from "../src/components/ErrorState";
import { ApiError } from "../src/lib/apiError";

let dom: Window;
let root: Root;
let container: HTMLDivElement;
const originals = new Map<string, PropertyDescriptor | undefined>();

beforeEach(() => {
  dom = new Window();
  for (const [key, value] of Object.entries({ window: dom, document: dom.document, HTMLElement: dom.HTMLElement, Node: dom.Node, IS_REACT_ACT_ENVIRONMENT: true })) {
    originals.set(key, Object.getOwnPropertyDescriptor(globalThis, key));
    Object.defineProperty(globalThis, key, { value, configurable: true, writable: true });
  }
  container = document.createElement("div");
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

const render = (node: React.ReactNode) => act(() => root.render(node));
const alert = () => container.querySelector('[role="alert"]');
const buttons = () => Array.from(container.querySelectorAll("button"));

test("page errors announce the headline and offer Try again", () => {
  let retries = 0;
  render(<ErrorState size="page" title="Couldn't load economy history." detail={RESTART_HINT} onRetry={() => retries++} />);

  expect(alert()?.textContent).toContain("Couldn't load economy history.");
  expect(alert()?.textContent).toContain(RESTART_HINT);
  const [retry] = buttons();
  expect(retry.textContent).toBe("Try again");
  expect(retry.className).toContain("control-button--primary");
  act(() => retry.click());
  expect(retries).toBe(1);
});

test("a retry in flight disables the button and says so", () => {
  render(<ErrorState title="Couldn't load matchups." onRetry={() => {}} retrying failureCount={2} />);

  const [retry] = buttons();
  expect(retry.disabled).toBe(true);
  expect(retry.textContent).toBe("Trying again…");
  expect(alert()?.getAttribute("aria-busy")).toBe("true");
  expect(container.textContent).not.toContain("Still failing");
});

test("repeated failures say how many tries it has taken", () => {
  render(<ErrorState title="Couldn't load matchups." onRetry={() => {}} failureCount={1} />);
  expect(container.textContent).not.toContain("Still failing");

  render(<ErrorState title="Couldn't load matchups." onRetry={() => {}} failureCount={3} />);
  expect(container.textContent).toContain("Still failing after 3 tries.");
});

test("inline errors are one line with a Retry link", () => {
  let retries = 0;
  render(<ErrorState size="inline" title="Couldn't save tags." onRetry={() => retries++} />);

  expect(alert()?.className).toContain("error-state--inline");
  const [retry] = buttons();
  expect(retry.textContent).toBe("Retry");
  act(() => retry.click());
  expect(retries).toBe(1);
});

function failedQuery(error: Error, overrides: Partial<FailedQuery> = {}): FailedQuery & { refetches: number } {
  const query = { error, isFetching: false, errorUpdateCount: 1, refetches: 0, refetch: () => { query.refetches++; } };
  return Object.assign(query, overrides);
}

test("a failed query uses the API's message and can be retried", () => {
  const query = failedQuery(new ApiError(500, "Couldn't load matches."));
  render(<QueryErrorState query={query} size="page" />);

  expect(alert()?.textContent).toContain("Couldn't load matches.");
  expect(alert()?.textContent).toContain(RESTART_HINT);
  act(() => buttons()[0].click());
  expect(query.refetches).toBe(1);
});

test("a 404 isn't offered a retry and gets the missing icon", () => {
  render(<QueryErrorState query={failedQuery(new ApiError(404, "match not found"))} title="This match doesn't exist." />);

  expect(alert()?.textContent).toContain("This match doesn't exist.");
  expect(alert()?.textContent).not.toContain(RESTART_HINT);
  expect(buttons()).toHaveLength(0);
  expect(container.querySelector(".error-state__icon circle")).not.toBeNull();
});

test("extra actions render beside Try again", () => {
  render(
    <QueryErrorState
      query={failedQuery(new ApiError(0, "Couldn't reach Ponder's backend."))}
      size="page"
      actions={<a href="/">Back to overview</a>}
    />,
  );

  expect(buttons()[0].textContent).toBe("Try again");
  expect(container.querySelector(".error-state__actions a")?.textContent).toBe("Back to overview");
});
