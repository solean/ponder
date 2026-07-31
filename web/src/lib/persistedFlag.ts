import { useCallback, useSyncExternalStore } from "react";

/**
 * Boolean UI preferences (panel collapsed, banner minimized, …) persisted in
 * localStorage and shared process-wide: every component reading the same key
 * re-renders together, so duplicated panels can't drift apart, and edits made
 * in another tab arrive through the storage event.
 *
 * The in-memory map is authoritative once a value has been set, so the toggle
 * keeps working when localStorage is unavailable (private mode, quota).
 */

const memory = new Map<string, boolean>();
const listenersByKey = new Map<string, Set<() => void>>();

function readFlag(key: string, fallback: boolean): boolean {
  const pending = memory.get(key);
  if (pending !== undefined) return pending;
  if (typeof window === "undefined") return fallback;
  try {
    const stored = window.localStorage.getItem(key);
    if (stored === "true") return true;
    if (stored === "false") return false;
  } catch {
    // Ignore storage failures and fall back to the default.
  }
  return fallback;
}

function writeFlag(key: string, value: boolean): void {
  memory.set(key, value);
  try {
    window.localStorage.setItem(key, String(value));
  } catch {
    // Ignore storage failures; the in-memory value still drives the UI.
  }
  for (const listener of listenersByKey.get(key) ?? []) listener();
}

function subscribe(key: string, listener: () => void): () => void {
  let listeners = listenersByKey.get(key);
  if (!listeners) {
    listeners = new Set();
    listenersByKey.set(key, listeners);
  }
  listeners.add(listener);

  const onStorage = (event: StorageEvent) => {
    if (event.key !== key) return;
    // Another tab is the source of truth now; drop our cached copy.
    memory.delete(key);
    listener();
  };
  window.addEventListener("storage", onStorage);

  return () => {
    listeners.delete(listener);
    if (listeners.size === 0) listenersByKey.delete(key);
    window.removeEventListener("storage", onStorage);
  };
}

export function usePersistedFlag(
  key: string,
  fallback: boolean,
): [boolean, (next: boolean | ((current: boolean) => boolean)) => void] {
  const value = useSyncExternalStore(
    useCallback((listener: () => void) => subscribe(key, listener), [key]),
    () => readFlag(key, fallback),
    () => fallback,
  );

  const setValue = useCallback(
    (next: boolean | ((current: boolean) => boolean)) => {
      writeFlag(
        key,
        typeof next === "function" ? next(readFlag(key, fallback)) : next,
      );
    },
    [key, fallback],
  );

  return [value, setValue];
}
