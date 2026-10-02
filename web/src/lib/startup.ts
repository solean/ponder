class StartupFailure extends Error {}

function pause(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    signal.throwIfAborted();
    const abort = () => {
      clearTimeout(timer);
      reject(signal.reason);
    };
    const timer = setTimeout(() => {
      signal.removeEventListener("abort", abort);
      resolve();
    }, ms);
    signal.addEventListener("abort", abort, { once: true });
  });
}

// Only readiness blocks the shell. Maintenance continues after health is OK.
// Bound both individual requests and the overall wait so an unavailable
// backend always leads to an actionable error rather than an endless loader.
export async function waitForBackend(
  url: string,
  signal: AbortSignal,
  { timeoutMs = 60_000, pollMs = 500 } = {},
): Promise<true> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    signal.throwIfAborted();
    const request = new AbortController();
    const abort = () => request.abort(signal.reason);
    signal.addEventListener("abort", abort, { once: true });
    const timer = setTimeout(() => request.abort(), Math.min(5_000, deadline - Date.now()));
    try {
      const response = await fetch(url, { signal: request.signal, cache: "no-store" });
      const body = await response.json() as { status?: string; error?: string };
      signal.throwIfAborted();
      if (response.ok && body.status === "ok") return true;
      if (response.status === 401 || response.status === 403) {
        throw new StartupFailure(
          `Ponder’s startup request was rejected (${response.status}): ${body.error || "access denied"}`,
        );
      }
      if (body.status === "failed") {
        throw new StartupFailure(body.error || "Ponder could not start. Restart the app and try again.");
      }
      if (response.ok) {
        throw new StartupFailure("Ponder received an unexpected response. Restart the app and try again.");
      }
    } catch (error) {
      signal.throwIfAborted();
      if (error instanceof StartupFailure) throw error;
      // A dev server may briefly refuse connections or return non-JSON while
      // its backend starts. These failures are retryable within the deadline.
    } finally {
      clearTimeout(timer);
      signal.removeEventListener("abort", abort);
    }
    await pause(Math.max(0, Math.min(pollMs, deadline - Date.now())), signal);
  }
  throw new Error("Ponder is taking longer than expected to start. Try again, or restart the app if this continues.");
}
