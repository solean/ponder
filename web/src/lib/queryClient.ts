import { QueryClient } from "@tanstack/react-query";

import { ApiError } from "./apiError";

/** Same as React Query's default: three retries after the first failure. */
export const MAX_QUERY_RETRIES = 3;

/**
 * Whether trying a failed request again could help. A 4xx from Ponder's API,
 * such as a missing match, gives the same answer every time. Network errors,
 * 5xx responses, and errors from other services might not.
 */
export function isRetryableError(error: unknown): boolean {
  return !(error instanceof ApiError && error.status >= 400 && error.status < 500);
}

/** React Query `retry` option. `failureCount` is the failures so far, from 0. */
export function shouldRetryQuery(failureCount: number, error: unknown): boolean {
  return isRetryableError(error) && failureCount < MAX_QUERY_RETRIES;
}

export function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: shouldRetryQuery },
    },
  });
}
