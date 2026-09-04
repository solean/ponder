import { describe, expect, test } from "bun:test";

import { formatArenaDeck } from "../src/lib/arenaDeck";

describe("formatArenaDeck", () => {
  test("formats every Arena deck zone and sorts cards by name", () => {
    expect(
      formatArenaDeck([
        { section: "sideboard", cardName: "Negate", quantity: 2 },
        { section: "main", cardName: "Island", quantity: 20 },
        { section: "companion", cardName: "Jegantha, the Wellspring", quantity: 1 },
        { section: "main", cardName: "Consider", quantity: 4 },
        { section: "command", cardName: "Atraxa, Grand Unifier", quantity: 1 },
      ]),
    ).toBe(
      [
        "Companion\n1 Jegantha, the Wellspring",
        "Commander\n1 Atraxa, Grand Unifier",
        "Deck\n4 Consider\n20 Island",
        "Sideboard\n2 Negate",
      ].join("\n\n"),
    );
  });

  test("normalizes section and card-name whitespace", () => {
    expect(formatArenaDeck([{ section: " MAIN ", cardName: "  Lightning Strike ", quantity: 4 }])).toBe(
      "Deck\n4 Lightning Strike",
    );
  });

  test("rejects exports Arena cannot import reliably", () => {
    expect(() => formatArenaDeck([{ section: "main", quantity: 1 }])).toThrow("must have a name");
    expect(() => formatArenaDeck([{ section: "maybeboard", cardName: "Opt", quantity: 1 }])).toThrow(
      "Unsupported Arena deck section",
    );
    expect(() => formatArenaDeck([{ section: "main", cardName: "Opt", quantity: 0 }])).toThrow("Invalid quantity");
  });
});
