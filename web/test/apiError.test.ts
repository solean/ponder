import { afterEach, expect, test } from "bun:test";
import {
  ApiError,
  BACKEND_UNREACHABLE_MESSAGE,
  apiErrorFromResponse,
  errorMessageFromBody,
  requestJSON,
} from "../src/lib/apiError";

const originalFetch = globalThis.fetch;
afterEach(() => { globalThis.fetch = originalFetch; });

test("uses the backend's error message without the JSON wrapper", async () => {
  const err = await apiErrorFromResponse(Response.json({ error: "match not found" }, { status: 404 }));
  expect(err).toBeInstanceOf(ApiError);
  expect(err.status).toBe(404);
  expect(err.message).toBe("match not found");
});

test("keeps short plain-text bodies and drops HTML, other JSON, and empty bodies", () => {
  expect(errorMessageFromBody("404 page not found\n")).toBe("404 page not found");
  expect(errorMessageFromBody("<html><body>Bad Gateway</body></html>")).toBeNull();
  expect(errorMessageFromBody(JSON.stringify({ status: "failed" }))).toBeNull();
  expect(errorMessageFromBody(JSON.stringify({ error: "  " }))).toBeNull();
  expect(errorMessageFromBody("x".repeat(500))).toBeNull();
  expect(errorMessageFromBody("")).toBeNull();
});

test("falls back to the status when the body has no usable message", async () => {
  const err = await apiErrorFromResponse(new Response("", { status: 502 }));
  expect(err.message).toBe("Request failed with status 502.");
});

test("requestJSON returns the body of a successful response", async () => {
  globalThis.fetch = async () => Response.json({ ok: true });
  expect(await requestJSON<{ ok: boolean }>("/api/x")).toEqual({ ok: true });
});

test("requestJSON throws ApiError with the readable message", async () => {
  globalThis.fetch = async () => Response.json({ error: "initialize database: disk full" }, { status: 500 });
  const err = await requestJSON("/api/x").catch((e: unknown) => e);
  expect(err).toBeInstanceOf(ApiError);
  expect((err as ApiError).status).toBe(500);
  expect((err as ApiError).message).toBe("initialize database: disk full");
});

test("requestJSON reports an unreachable backend in words", async () => {
  globalThis.fetch = async () => { throw new TypeError("Load failed"); };
  const err = await requestJSON("/api/x").catch((e: unknown) => e);
  expect((err as ApiError).status).toBe(0);
  expect((err as ApiError).message).toBe(BACKEND_UNREACHABLE_MESSAGE);
});

test("requestJSON rethrows aborts unchanged", async () => {
  const controller = new AbortController();
  const abort = new DOMException("The operation was aborted.", "AbortError");
  globalThis.fetch = async () => { controller.abort(); throw abort; };
  const err = await requestJSON("/api/x", { signal: controller.signal }).catch((e: unknown) => e);
  expect(err).toBe(abort);
});
