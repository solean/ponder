import { expect, test } from "bun:test";

import { fetchCardPreview } from "../src/lib/cardPreviews";

test("batches previews into one backend request, in order", async () => {
  const originalFetch = globalThis.fetch;
  const requests: Array<{ url: string; body: unknown }> = [];
  globalThis.fetch = async (input, init) => {
    requests.push({ url: String(input), body: JSON.parse(String(init?.body)) });
    return Response.json({
      previews: [
        { name: "First Card", imageUrl: "https://cards.example/first.jpg", colors: ["G"] },
        null,
        { name: "Llanowar Elves", imageUrl: "https://cards.example/llanowar-elves.jpg" },
      ],
    });
  };
  try {
    expect(await fetchCardPreview(0)).toBeNull();
    const previews = await Promise.all([
      fetchCardPreview(1, "First Card"),
      fetchCardPreview(2),
      fetchCardPreview(0, " Llanowar Elves "),
    ]);
    expect(requests).toEqual([
      {
        url: "/api/cards/previews",
        body: {
          cards: [
            { cardId: 1, name: "First Card" },
            { cardId: 2, name: "" },
            { cardId: 0, name: "Llanowar Elves" },
          ],
        },
      },
    ]);
    expect(previews.map((preview) => preview?.name ?? null)).toEqual(["First Card", null, "Llanowar Elves"]);
    expect(previews[0]?.colors).toEqual(["G"]);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("rejects when the backend can't be reached, so the query can retry", async () => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async () => {
    throw new TypeError("Failed to fetch");
  };
  try {
    await expect(fetchCardPreview(5, "Unreachable")).rejects.toThrow();
  } finally {
    globalThis.fetch = originalFetch;
  }
});
