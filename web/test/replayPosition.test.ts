import { expect, test } from "bun:test";
import { parseReplayBookmark, replayBookmarkPath, replayPositionKey } from "../src/lib/replay/position";

test("local replay links encode match, game, recording kind and stable identity", () => {
 const path = replayBookmarkPath(80, { game: 2, frameId: 431, kind: "frame" });
 expect(path).toBe("/matches/80?replayGame=2&replayFrame=431&replayKind=frame");
 expect(parseReplayBookmark(path.slice(path.indexOf("?"))).bookmark).toEqual({ game: 2, frameId: 431, kind: "frame" });
 expect(replayPositionKey(80, 2, "frame")).not.toBe(replayPositionKey(81, 2, "frame"));
 expect(replayPositionKey(80, 2, "frame")).not.toBe(replayPositionKey(80, 2, "observed"));
});
test("incomplete, invalid and obsolete bookmark references cannot pick a default frame", () => {
 for (const search of ["?replayGame=2", "?replayGame=2&replayFrame=0&replayKind=frame", "?replayGame=-1&replayFrame=2&replayKind=frame", "?replayGame=2&replayFrame=NaN&replayKind=frame", "?replayGame=2&replayFrame=8&replayKind=missing"]) {
  expect(parseReplayBookmark(search).bookmark).toBeNull();
  expect(parseReplayBookmark(search).error).toContain("invalid");
 }
 expect(parseReplayBookmark("")).toEqual({ bookmark: null, error: null });
});

test("copied private desktop URLs can be parsed locally without opening that host", async () => {
 const { parseReplayLink } = await import("../src/lib/replay/position");
 expect(parseReplayLink("wails://wails/matches/80?replayGame=2&replayFrame=431&replayKind=frame")).toEqual({ matchId: 80, game: 2, frameId: 431, kind: "frame" });
 expect(parseReplayLink("/matches/80?replayGame=2&replayFrame=431&replayKind=frame")?.frameId).toBe(431);
 expect(parseReplayLink("/matches/80")).toBeNull();
 expect(parseReplayLink("/settings?replayGame=2&replayFrame=431&replayKind=frame")).toBeNull();
});
