import { expect, test } from "bun:test";
import { replayPositionKey } from "../src/lib/replay/position";

test("replay position keys are scoped by match, game and recording kind", () => {
 expect(replayPositionKey(80, 2, "frame")).toBe("ponder.replay.position.80.2.frame");
 expect(replayPositionKey(80, 2, "frame")).not.toBe(replayPositionKey(81, 2, "frame"));
 expect(replayPositionKey(80, 2, "frame")).not.toBe(replayPositionKey(80, 2, "observed"));
});
