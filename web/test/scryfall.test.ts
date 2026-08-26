import { expect, test } from "bun:test";

import { fetchCardPreview } from "../src/lib/scryfall";

test("serializes Scryfall requests and stops during a rate-limit cooldown", async () => {
  const originalFetch = globalThis.fetch;
  const requestedAt: number[] = [];
  let rateLimited = false;

  globalThis.fetch = async () => {
    requestedAt.push(Date.now());
    if (rateLimited) {
      return new Response("rate limited", {
        status: 429,
        headers: { "Retry-After": "60" },
      });
    }
    return Response.json({
      name: "Test Card",
      image_uris: { normal: "https://cards.example/test.jpg" },
    });
  };

  try {
    const previews = await Promise.all([fetchCardPreview(1), fetchCardPreview(2)]);
    expect(previews.every((preview) => preview?.name === "Test Card")).toBe(true);
    expect(requestedAt).toHaveLength(2);
    expect(requestedAt[1] - requestedAt[0]).toBeGreaterThanOrEqual(180);

    rateLimited = true;
    expect(await fetchCardPreview(3, "Rate Limited Card")).toBeNull();
    expect(await fetchCardPreview(4, "Blocked During Cooldown")).toBeNull();
    expect(requestedAt).toHaveLength(3);
  } finally {
    globalThis.fetch = originalFetch;
  }
});
