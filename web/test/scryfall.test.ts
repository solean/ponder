import { expect, test } from "bun:test";

import { fetchCardPreview } from "../src/lib/scryfall";

test("batches named card previews and stops during a rate-limit cooldown", async () => {
  const originalFetch = globalThis.fetch;
  const requestedURLs: string[] = [];
  const submittedNames: string[][] = [];
  let rateLimited = false;

  globalThis.fetch = async (input, init) => {
    requestedURLs.push(String(input));
    if (rateLimited) {
      return new Response("rate limited", {
        status: 429,
        headers: { "Retry-After": "60" },
      });
    }

    const body = JSON.parse(String(init?.body)) as { identifiers: Array<{ name: string }> };
    const names = body.identifiers.map((identifier) => identifier.name);
    submittedNames.push(names);
    return Response.json({
      data: names.map((name) => ({
        name,
        image_uris: { normal: `https://cards.example/${encodeURIComponent(name)}.jpg` },
      })),
      not_found: [],
    });
  };

  try {
    const previews = await Promise.all([
      fetchCardPreview(1, "First Card"),
      fetchCardPreview(2, "Second Card"),
    ]);
    expect(previews.map((preview) => preview?.name)).toEqual(["First Card", "Second Card"]);
    expect(requestedURLs).toEqual(["https://api.scryfall.com/cards/collection"]);
    expect(submittedNames).toEqual([["First Card", "Second Card"]]);

    rateLimited = true;
    expect(await fetchCardPreview(3, "Rate Limited Card")).toBeNull();
    expect(await fetchCardPreview(4, "Blocked During Cooldown")).toBeNull();
    expect(requestedURLs).toHaveLength(2);
  } finally {
    globalThis.fetch = originalFetch;
  }
});
