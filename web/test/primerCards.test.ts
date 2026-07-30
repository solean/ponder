import { describe, expect, test } from "bun:test";
import type { Root } from "mdast";

import { linkPrimerCardNames, primerCardIdFromHref } from "../src/lib/primerCards";

const cards = [
  { cardId: 1, cardName: "Slickshot Show-Off" },
  { cardId: 2, cardName: "Spell Pierce" },
];

describe("linkPrimerCardNames", () => {
  test("links every whole card-name mention while preserving surrounding prose", () => {
    const root: Root = {
      type: "root",
      children: [
        {
          type: "paragraph",
          children: [
            {
              type: "text",
              value: "Slickshot Show-Off races; protect slickshot show-off with Spell Pierce, not Spell Pierced.",
            },
          ],
        },
      ],
    };

    linkPrimerCardNames(root, cards);

    expect(root.children[0]).toEqual({
      type: "paragraph",
      children: [
        { type: "link", url: "#primer-card-1", title: null, children: [{ type: "text", value: "Slickshot Show-Off" }] },
        { type: "text", value: " races; protect " },
        { type: "link", url: "#primer-card-1", title: null, children: [{ type: "text", value: "slickshot show-off" }] },
        { type: "text", value: " with " },
        { type: "link", url: "#primer-card-2", title: null, children: [{ type: "text", value: "Spell Pierce" }] },
        { type: "text", value: ", not Spell Pierced." },
      ],
    });
  });

  test("leaves existing links and code spans untouched", () => {
    const root: Root = {
      type: "root",
      children: [
        {
          type: "paragraph",
          children: [
            { type: "inlineCode", value: "Spell Pierce" },
            { type: "text", value: " or " },
            {
              type: "link",
              url: "https://example.com",
              children: [{ type: "text", value: "Slickshot Show-Off" }],
            },
          ],
        },
      ],
    };

    linkPrimerCardNames(root, cards);

    expect(root.children[0]).toEqual({
      type: "paragraph",
      children: [
        { type: "inlineCode", value: "Spell Pierce" },
        { type: "text", value: " or " },
        {
          type: "link",
          url: "https://example.com",
          children: [{ type: "text", value: "Slickshot Show-Off" }],
        },
      ],
    });
  });
});

describe("primerCardIdFromHref", () => {
  test("accepts only positive integer primer-card targets", () => {
    expect(primerCardIdFromHref("#primer-card-42")).toBe(42);
    expect(primerCardIdFromHref("#primer-card-0")).toBeNull();
    expect(primerCardIdFromHref("#primer-card-4.2")).toBeNull();
    expect(primerCardIdFromHref("https://example.com")).toBeNull();
  });
});
