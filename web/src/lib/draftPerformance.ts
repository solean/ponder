import { parseEventName } from "./events";
import type { DraftSession } from "./types";

/**
 * Rolling event win rates for the Drafts page performance chart. Each draft
 * session is one event; a window's rate pools match wins and losses across the
 * events in it, so a 7–2 run weighs more than a 0–3 one.
 */

export const ROLLING_WINDOWS = [10, 50] as const;

export type DraftPerformanceFilter = {
  /** Set code (e.g. "HOB"), or null for every set. */
  setCode: string | null;
  /** Event category from parseEventName (e.g. "Premier Draft"), or null for every format. */
  format: string | null;
};

export type DraftPerformancePoint = {
  sessionId: number;
  eventName: string;
  setCode: string | null;
  format: string;
  dateValue: number | null;
  wins: number;
  losses: number;
  trophy: boolean;
  /** 1-based position in the filtered, chronological run list. */
  eventNumber: number;
  /** Pooled match win rate over every event so far. */
  all: number;
  /** Pooled rate over the trailing N events; null until N events exist. */
  rolling: Record<(typeof ROLLING_WINDOWS)[number], number | null>;
};

export type DraftPerformance = {
  points: DraftPerformancePoint[];
  wins: number;
  losses: number;
  trophies: number;
};

function validDateValue(timestamp?: string | null): number | null {
  if (!timestamp) return null;
  const value = new Date(timestamp).getTime();
  return Number.isFinite(value) ? value : null;
}

export function draftSessionDateValue(draft: DraftSession): number | null {
  return (
    validDateValue(draft.startedAt) ??
    validDateValue(draft.completedAt) ??
    parseEventName(draft.eventName).dateValue
  );
}

/** Traditional draft is three Bo3 matches; the Bo1 queues run to seven wins. */
function trophyWins(format: string): number {
  return format === "Traditional Draft" ? 3 : 7;
}

type DraftRun = Omit<DraftPerformancePoint, "eventNumber" | "all" | "rolling">;

/** Sessions with a recorded result, oldest first. */
function draftRuns(drafts: DraftSession[]): DraftRun[] {
  return drafts
    .flatMap((draft): DraftRun[] => {
      const wins = draft.wins ?? 0;
      const losses = draft.losses ?? 0;
      if (wins + losses === 0) return [];
      const parsed = parseEventName(draft.eventName);
      return [
        {
          sessionId: draft.id,
          eventName: draft.eventName,
          setCode: parsed.setCode,
          format: parsed.category,
          dateValue: draftSessionDateValue(draft),
          wins,
          losses,
          trophy: wins >= trophyWins(parsed.category),
        },
      ];
    })
    .sort((a, b) => {
      if (a.dateValue != null && b.dateValue != null && a.dateValue !== b.dateValue) {
        return a.dateValue - b.dateValue;
      }
      if (a.dateValue != null && b.dateValue == null) return 1;
      if (a.dateValue == null && b.dateValue != null) return -1;
      return a.sessionId - b.sessionId;
    });
}

export type DraftPerformanceOptions = {
  /** Set codes, most recently drafted first. */
  sets: string[];
  /** Formats, most played first. */
  formats: string[];
};

export function draftPerformanceOptions(drafts: DraftSession[]): DraftPerformanceOptions {
  const runs = draftRuns(drafts);
  const sets: string[] = [];
  for (let i = runs.length - 1; i >= 0; i -= 1) {
    const code = runs[i].setCode;
    if (code && !sets.includes(code)) sets.push(code);
  }
  const formatCounts = new Map<string, number>();
  for (const run of runs) {
    formatCounts.set(run.format, (formatCounts.get(run.format) ?? 0) + 1);
  }
  const formats = [...formatCounts.entries()]
    .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
    .map(([format]) => format);
  return { sets, formats };
}

export function buildDraftPerformance(
  drafts: DraftSession[],
  filter: DraftPerformanceFilter,
): DraftPerformance {
  const runs = draftRuns(drafts).filter(
    (run) =>
      (filter.setCode == null || run.setCode === filter.setCode) &&
      (filter.format == null || run.format === filter.format),
  );

  // Prefix sums make every trailing window an O(1) subtraction.
  const winSums = [0];
  const lossSums = [0];
  for (const run of runs) {
    winSums.push(winSums[winSums.length - 1] + run.wins);
    lossSums.push(lossSums[lossSums.length - 1] + run.losses);
  }
  const rateBetween = (start: number, end: number) => {
    const wins = winSums[end] - winSums[start];
    const losses = lossSums[end] - lossSums[start];
    return wins / (wins + losses);
  };

  const points = runs.map((run, index): DraftPerformancePoint => {
    const end = index + 1;
    const rolling = {} as DraftPerformancePoint["rolling"];
    for (const size of ROLLING_WINDOWS) {
      rolling[size] = end >= size ? rateBetween(end - size, end) : null;
    }
    return { ...run, eventNumber: end, all: rateBetween(0, end), rolling };
  });

  return {
    points,
    wins: winSums[winSums.length - 1],
    losses: lossSums[lossSums.length - 1],
    trophies: runs.filter((run) => run.trophy).length,
  };
}
