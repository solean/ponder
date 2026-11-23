import { expect, test } from "bun:test";
import { ApiError } from "../src/lib/apiError";
import { MAX_QUERY_RETRIES, createQueryClient, isRetryableError, shouldRetryQuery } from "../src/lib/queryClient";

test("4xx responses from the API are never retried", () => {
  for (const status of [400, 403, 404, 409]) {
    expect(isRetryableError(new ApiError(status, "nope"))).toBe(false);
    expect(shouldRetryQuery(0, new ApiError(status, "nope"))).toBe(false);
  }
});

test("server errors, network errors, and other failures retry up to the limit", () => {
  for (const error of [new ApiError(500, "Couldn't load matches."), new ApiError(0, "unreachable"), new Error("Scryfall timed out")]) {
    expect(isRetryableError(error)).toBe(true);
    for (let failures = 0; failures < MAX_QUERY_RETRIES; failures++) {
      expect(shouldRetryQuery(failures, error)).toBe(true);
    }
    expect(shouldRetryQuery(MAX_QUERY_RETRIES, error)).toBe(false);
  }
});

async function attemptsUntilError(error: Error): Promise<number> {
  const client = createQueryClient();
  let attempts = 0;
  await client
    .fetchQuery({
      queryKey: ["attempts", error.message],
      queryFn: async () => {
        attempts++;
        throw error;
      },
      retryDelay: 0,
    })
    .catch(() => undefined);
  client.clear();
  return attempts;
}

test("the app's query client fails a 404 at once and retries a 500", async () => {
  expect(await attemptsUntilError(new ApiError(404, "match not found"))).toBe(1);
  expect(await attemptsUntilError(new ApiError(500, "Couldn't load this match."))).toBe(1 + MAX_QUERY_RETRIES);
});
