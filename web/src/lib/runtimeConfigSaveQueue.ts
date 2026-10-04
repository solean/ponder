import type { RuntimeConfig } from "./types";

/** Keep later edits queued while a full-config request is in flight. */
export class RuntimeConfigSaveQueue {
  baseline: RuntimeConfig;
  pending: Partial<RuntimeConfig> = {};
  running = false;

  constructor(initial: RuntimeConfig) {
    this.baseline = initial;
  }

  enqueue(patch: Partial<RuntimeConfig>) {
    this.pending = { ...this.pending, ...patch };
  }

  async drain(save: (config: RuntimeConfig, patch: Partial<RuntimeConfig>) => Promise<RuntimeConfig>) {
    if (this.running) return;
    this.running = true;
    try {
      while (Object.keys(this.pending).length) {
        const patch = this.pending;
        this.pending = {};
        try {
          this.baseline = await save({ ...this.baseline, ...patch }, patch);
        } catch (error) {
          this.pending = { ...patch, ...this.pending };
          throw error;
        }
      }
    } finally {
      this.running = false;
    }
  }
}
