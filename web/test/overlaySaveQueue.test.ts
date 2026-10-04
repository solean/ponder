import { describe, expect, test } from "bun:test";
import { OverlaySaveQueue, type OverlaySaveRequest } from "../src/lib/overlaySaveQueue";
import { DEFAULT_OVERLAY_SETTINGS } from "../src/lib/overlaySettings";
const request = (opacity: number, shortcut = false): OverlaySaveRequest => ({ settings: { ...DEFAULT_OVERLAY_SETTINGS, opacity, shortcut: shortcut ? "Ctrl+P" : DEFAULT_OVERLAY_SETTINGS.shortcut }, shortcut });
const turn = () => new Promise((resolve) => setTimeout(resolve, 0));
describe("overlay autosave queue", () => {
  test("serializes writes and coalesces rapid edits to the newest settings", async () => {
    let release!: () => void;
    const writes: number[] = [];
    const queue = new OverlaySaveQueue(async ({ settings }) => {
      writes.push(settings.opacity);
      if (writes.length === 1) await new Promise<void>((resolve) => { release = resolve; });
    }, () => {}, () => {}, () => {});
    queue.enqueue(request(.4)); queue.enqueue(request(.5)); queue.enqueue(request(.6));
    expect(writes).toEqual([.4]);
    release(); await turn();
    expect(writes).toEqual([.4, .6]); expect(queue.running).toBe(false);
  });
  test("retry keeps newer edits and explicitly applied shortcut after failure", async () => {
    let reject!: (error: Error) => void;
    const writes: OverlaySaveRequest[] = [];
    const queue = new OverlaySaveQueue(async (value) => {
      writes.push(value);
      if (writes.length === 1) await new Promise<void>((_, fail) => { reject = fail; });
    }, () => {}, () => {}, () => {});
    queue.enqueue(request(.4, true)); queue.enqueue(request(.7));
    reject(new Error("offline")); await turn();
    expect(queue.running).toBe(false); expect(queue.pending?.settings.opacity).toBe(.7);
    expect(queue.pending?.settings.shortcut).toBe("Ctrl+P"); expect(queue.pending?.shortcut).toBe(true);
    await queue.drain();
    expect(writes[1].settings.opacity).toBe(.7); expect(writes[1].settings.shortcut).toBe("Ctrl+P"); expect(queue.pending).toBe(null);
  });
});
