import { afterEach, expect, test } from "bun:test";
import { waitForBackend } from "../src/lib/startup";

const originalFetch = globalThis.fetch;
afterEach(() => { globalThis.fetch = originalFetch; });

const options = { timeoutMs: 100, pollMs: 1 };

test("waits through startup and transient connection failures until ready", async () => {
  let calls = 0;
  globalThis.fetch = async () => {
    calls++;
    if (calls === 1) throw new TypeError("connection refused");
    if (calls === 2) return new Response("proxy unavailable", { status: 502 });
    if (calls === 3) return Response.json({ status: "starting" }, { status: 503 });
    return Response.json({ status: "ok" });
  };
  expect(await waitForBackend("/api/health", new AbortController().signal, options)).toBe(true);
  expect(calls).toBe(4);
});

test("reports terminal startup errors immediately and permits a new attempt", async () => {
  let calls = 0;
  globalThis.fetch = async () => {
    calls++;
    return Response.json({ status: "failed", error: "initialize database: disk full" }, { status: 503 });
  };
  await expect(waitForBackend("/api/health", new AbortController().signal, options)).rejects.toThrow("disk full");
  expect(calls).toBe(1);
  globalThis.fetch = async () => Response.json({ status: "ok" });
  expect(await waitForBackend("/api/health", new AbortController().signal, options)).toBe(true);
});

test("limits waiting when the backend never becomes ready", async () => {
  globalThis.fetch = async () => Response.json({ status: "starting" }, { status: 503 });
  await expect(waitForBackend("/api/health", new AbortController().signal, { timeoutMs: 10, pollMs: 1 })).rejects.toThrow("taking longer");
});

test("cancels an in-flight request when the startup screen unmounts", async () => {
  const controller = new AbortController();
  let requestSignal: AbortSignal | undefined;
  globalThis.fetch = async (_, init) => {
    requestSignal = init?.signal as AbortSignal;
    return new Promise((_, reject) => {
      requestSignal!.addEventListener("abort", () => reject(requestSignal!.reason), { once: true });
    });
  };
  const pending = waitForBackend("/api/health", controller.signal, options);
  controller.abort(new Error("unmounted"));
  await expect(pending).rejects.toThrow("unmounted");
  expect(requestSignal?.aborted).toBe(true);
});

test("times out a stalled health request", async () => {
  globalThis.fetch = async (_, init) => new Promise((_, reject) => {
    init!.signal!.addEventListener("abort", () => reject(init!.signal!.reason), { once: true });
  });
  await expect(waitForBackend("/api/health", new AbortController().signal, { timeoutMs: 10, pollMs: 1 })).rejects.toThrow("taking longer");
});

test("reports rejected startup requests immediately instead of timing out", async () => {
  for (const status of [401, 403]) {
    let calls = 0;
    globalThis.fetch = async () => {
      calls++;
      return Response.json({ error: "forbidden origin" }, { status });
    };
    await expect(waitForBackend("/api/health", new AbortController().signal, options)).rejects.toThrow(`(${status}): forbidden origin`);
    expect(calls).toBe(1);
  }
});
