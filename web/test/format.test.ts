import { describe, expect, test } from "bun:test";

import { formatBytes, formatGameFormat, shortenHomePath } from "../src/lib/format";

describe("formatBytes", () => {
  test("returns dash for zero, negative, and non-finite values", () => {
    expect(formatBytes(0)).toBe("-");
    expect(formatBytes(-42)).toBe("-");
    expect(formatBytes(Number.NaN)).toBe("-");
  });

  test("formats bytes without decimals", () => {
    expect(formatBytes(1)).toBe("1 B");
    expect(formatBytes(512)).toBe("512 B");
  });

  test("uses one decimal below 10 in a unit, none above", () => {
    expect(formatBytes(1536)).toBe("1.5 KB");
    expect(formatBytes(10 * 1024)).toBe("10 KB");
    expect(formatBytes(5.25 * 1024 * 1024)).toBe("5.3 MB");
    expect(formatBytes(3 * 1024 * 1024 * 1024)).toBe("3.0 GB");
  });
});

describe("shortenHomePath", () => {
  test("abbreviates a macOS home prefix", () => {
    expect(shortenHomePath("/Users/chris/Library/Logs/app.log")).toBe("~/Library/Logs/app.log");
  });

  test("leaves other paths untouched", () => {
    expect(shortenHomePath("data/ponder.db")).toBe("data/ponder.db");
    expect(shortenHomePath("/var/log/system.log")).toBe("/var/log/system.log");
    expect(shortenHomePath("/Users/chris")).toBe("/Users/chris");
  });
});

describe("formatGameFormat", () => {
  test("removes the best-of-three prefix from Arena format names", () => {
    expect(formatGameFormat("TraditionalStandard")).toBe("Standard");
    expect(formatGameFormat("Traditional_Explorer")).toBe("Pioneer");
  });

  test("renames Arena's legacy Explorer format to Pioneer", () => {
    expect(formatGameFormat("Explorer")).toBe("Pioneer");
    expect(formatGameFormat("TraditionalExplorer")).toBe("Pioneer");
  });

  test("adds spaces to compound format names and handles missing data", () => {
    expect(formatGameFormat("HistoricBrawl")).toBe("Historic Brawl");
    expect(formatGameFormat("Pioneer")).toBe("Pioneer");
    expect(formatGameFormat("")).toBe("-");
    expect(formatGameFormat(null)).toBe("-");
  });
});
