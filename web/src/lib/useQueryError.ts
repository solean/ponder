import { useRef } from "react";

/** The parts of a React Query result that useQueryError reads. */
export type ErrorTrackedQuery = {
  data: unknown;
  error: Error | null;
  isFetching: boolean;
  errorUpdateCount: number;
  refetch: () => Promise<unknown>;
};

/** What an error view needs: see QueryErrorState. */
export type QueryErrorView = {
  /** The error to show, or null when there's nothing to show. */
  error: Error | null;
  /** True while the query is fetching again after failing. */
  retrying: boolean;
  /** Failed attempts since the error first appeared, counting background ones. */
  tries: number;
  /** Fetches again. */
  retry: () => Promise<unknown>;
};

/**
 * Keeps a failed query's error on screen while it fetches again.
 *
 * React Query v5 resets a query that has no data to "pending" whenever it
 * refetches, clearing `error`. Without this, every retry (a Try again click,
 * polling, the window regaining focus) swaps the page back to its loading
 * state, then mounts a fresh error block. Pages check `view.error` before
 * their loading state.
 *
 * `tries` counts every failed attempt since the error appeared, whoever
 * started it, and resets once the query succeeds.
 */
export function useQueryError(query: ErrorTrackedQuery): QueryErrorView {
  const lastError = useRef<Error | null>(null);
  // query.errorUpdateCount when the current run of failures began.
  const firstFailure = useRef(0);

  // Recorded during render so the error never disappears for a frame.
  if (query.error) {
    if (lastError.current === null) firstFailure.current = query.errorUpdateCount;
    lastError.current = query.error;
  } else if (query.data !== undefined) {
    lastError.current = null;
  }

  const error = query.error ?? (query.isFetching ? lastError.current : null);
  return {
    error,
    retrying: error !== null && query.isFetching,
    tries: error === null ? 0 : query.errorUpdateCount - firstFailure.current + 1,
    retry: query.refetch,
  };
}
