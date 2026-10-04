import { describe, expect, test } from "bun:test";
import { RuntimeConfigSaveQueue } from "../src/lib/runtimeConfigSaveQueue";
import type { RuntimeConfig } from "../src/lib/types";

const initial: RuntimeConfig = {
  logPath: "original.log", pollIntervalSeconds: 2, includePrev: true,
  autoStartLive: true, autoCheckUpdates: false, aiProvider: "claude", aiModel: "opus",
};

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

describe("runtime config autosave queue", () => {
  test("serializes rapid edits and preserves unrelated saved fields", async () => {
    const queue = new RuntimeConfigSaveQueue(initial);
    const first = deferred<RuntimeConfig>();
    const requests: RuntimeConfig[] = [];
    queue.enqueue({ logPath: "custom.log" });
    const saving = queue.drain(async (config) => {
      requests.push(config);
      return requests.length === 1 ? first.promise : config;
    });
    queue.enqueue({ logPath: "original.log", autoCheckUpdates: true });
    queue.enqueue({ aiProvider: "openai", aiModel: "default" });
    await queue.drain(async () => { throw new Error("Concurrent save"); });
    expect(requests).toHaveLength(1);
    first.resolve({ ...requests[0], pollIntervalSeconds: 5 });
    await saving;
    expect(requests[1]).toEqual({ ...initial, pollIntervalSeconds: 5,
      autoCheckUpdates: true, aiProvider: "openai", aiModel: "default" });
    expect(queue.baseline).toEqual(requests[1]);
    expect(queue.pending).toEqual({});
  });

  test("retains failed edits and newer requested values for retry", async () => {
    const queue = new RuntimeConfigSaveQueue(initial);
    const first = deferred<RuntimeConfig>();
    queue.enqueue({ autoStartLive: false, aiModel: "sonnet" });
    const saving = queue.drain(() => first.promise);
    queue.enqueue({ autoStartLive: true, includePrev: false });
    first.reject(new Error("Offline"));
    await expect(saving).rejects.toThrow("Offline");
    expect(queue.baseline).toEqual(initial);
    expect(queue.pending).toEqual({ autoStartLive: true, aiModel: "sonnet", includePrev: false });
    expect(queue.running).toBe(false);
    await queue.drain(async (config) => config);
    expect(queue.baseline).toEqual({ ...initial, aiModel: "sonnet", includePrev: false });
    expect(queue.pending).toEqual({});
  });
});
