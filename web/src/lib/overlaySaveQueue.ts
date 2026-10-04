import type { OverlaySettings } from "./overlaySettings";

export type OverlaySaveRequest = { settings: OverlaySettings; shortcut: boolean };

/** Serializes full-setting writes and coalesces rapid edits without losing explicit shortcut intent. */
export class OverlaySaveQueue {
  pending: OverlaySaveRequest | null = null;
  running = false;

  constructor(private readonly save: (request: OverlaySaveRequest) => Promise<void>, private readonly onError: (error: unknown) => void, private readonly onStart: () => void, private readonly onDone: () => void) {}

  enqueue(request: OverlaySaveRequest) {
    this.pending = { ...request, settings: { ...request.settings } };
    void this.drain();
  }

  async drain() {
    if (this.running || !this.pending) return;
    this.running = true;
    this.onStart();
    while (this.pending) {
      const request = this.pending;
      this.pending = null;
      try {
        await this.save(request);
      } catch (error) {
        const newest = this.pending as OverlaySaveRequest | null;
        this.pending = newest ? {
          settings: { ...newest.settings, shortcut: request.shortcut ? request.settings.shortcut : newest.settings.shortcut },
          shortcut: request.shortcut || newest.shortcut,
        } : request;
        this.running = false;
        this.onError(error);
        return;
      }
    }
    this.running = false;
    this.onDone();
  }
}
