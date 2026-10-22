import { expect, test } from "bun:test";
import { selectScrubberLabels, scrubberTurnLabel } from "../src/lib/replay/scrubberLabels";
test("uneven boundaries use measured widths within 726px", () => {
 const labels = [0,1,2,20,21,22,75,99,100].map(index => ({ index, width: index > 90 ? 54 : 32 }));
 const selected = [...selectScrubberLabels(labels, 100, 726)];
 expect(selected.length).toBeLessThan(labels.length);
 selected.forEach(([index, left], i) => {
  expect(left).toBeGreaterThanOrEqual(0);
  expect(left + labels.find(l => l.index === index)!.width).toBeLessThanOrEqual(726);
  if (i) expect(left - selected[i-1]![1] - labels.find(l => l.index === selected[i-1]![0])!.width).toBeGreaterThanOrEqual(10);
 });
});
test("round turns are distinct without guessing identity", () => {
 expect(scrubberTurnLabel(1)).toBe("T1a"); expect(scrubberTurnLabel(2)).toBe("T1b"); expect(scrubberTurnLabel(-1)).toBe("Pre");
});
test("unmeasured containers hide labels", () => { expect(selectScrubberLabels([{index:0,width:40}],0,0).size).toBe(0); });

test("known starting player determines own and opposing turns", () => {
 expect(scrubberTurnLabel(1, "play")).toBe("T1 You");
 expect(scrubberTurnLabel(2, "play")).toBe("T1 Opp");
 expect(scrubberTurnLabel(1, "draw")).toBe("T1 Opp");
});

import { timelinePhaseLabel } from "../src/lib/replay";
test("main phases use game terminology", () => {
 expect(timelinePhaseLabel("Main1")).toBe("Precombat main");
 expect(timelinePhaseLabel("Main2")).toBe("Postcombat main");
 expect(timelinePhaseLabel("combat_begin")).toBe("Combat Begin");
});
