import { parseEventName } from "./events";
import type { Match } from "./types";

/**
 * Pure aggregation helpers for the Overview page. Everything operates on the
 * match list as returned by /api/matches (newest first); sortMatchesDesc is
 * available as a defensive re-sort.
 */

export type WinLossRecord = {
  wins: number;
  losses: number;
};

export type Streak = {
  result: "win" | "loss";
  length: number;
};

export function sortMatchesDesc(matches: Match[]): Match[] {
  return [...matches].sort(
    (a, b) => new Date(b.startedAt).getTime() - new Date(a.startedAt).getTime(),
  );
}

export function recordOf(matches: Match[]): WinLossRecord {
  let wins = 0;
  let losses = 0;
  for (const match of matches) {
    if (match.result === "win") wins += 1;
    else if (match.result === "loss") losses += 1;
  }
  return { wins, losses };
}

/** Win rate over decided matches, or null when nothing is decided. */
export function recordWinRate(record: WinLossRecord): number | null {
  const decided = record.wins + record.losses;
  return decided > 0 ? record.wins / decided : null;
}

/** Record over the `count` most recent decided matches (input newest first). */
export function recentForm(matches: Match[], count: number): WinLossRecord {
  const decided = matches.filter((match) => match.result === "win" || match.result === "loss");
  return recordOf(decided.slice(0, count));
}

/**
 * Current run of identical results starting from the most recent decided
 * match; unknown results are skipped rather than breaking the run.
 */
export function currentStreak(matches: Match[]): Streak | null {
  let streak: Streak | null = null;
  for (const match of matches) {
    if (match.result !== "win" && match.result !== "loss") continue;
    if (!streak) {
      streak = { result: match.result, length: 1 };
      continue;
    }
    if (match.result !== streak.result) break;
    streak.length += 1;
  }
  return streak;
}

const LIMITED_EVENT = /draft|sealed|jump in/i;

export function isLimitedEvent(eventName: string): boolean {
  return LIMITED_EVENT.test(parseEventName(eventName).category);
}

export type SplitRecords = {
  constructed: WinLossRecord;
  limited: WinLossRecord;
  play: WinLossRecord;
  draw: WinLossRecord;
  bo1: WinLossRecord;
  bo3: WinLossRecord;
};

export function splitRecords(matches: Match[]): SplitRecords {
  const decided = matches.filter((match) => match.result === "win" || match.result === "loss");
  return {
    constructed: recordOf(decided.filter((match) => !isLimitedEvent(match.eventName))),
    limited: recordOf(decided.filter((match) => isLimitedEvent(match.eventName))),
    play: recordOf(decided.filter((match) => match.playDraw === "play")),
    draw: recordOf(decided.filter((match) => match.playDraw === "draw")),
    bo1: recordOf(decided.filter((match) => match.bestOf === "bo1")),
    bo3: recordOf(decided.filter((match) => match.bestOf === "bo3")),
  };
}

export type DailyActivity = {
  /** Local date key, e.g. "2026-07-03". */
  date: string;
  label: string;
  count: number;
  wins: number;
  losses: number;
  unknown: number;
  trackedSeconds: number;
  timedMatches: number;
  constructed: number;
  limited: number;
};

function localDateKey(date: Date): string {
  const year = date.getFullYear();
  const month = `${date.getMonth() + 1}`.padStart(2, "0");
  const day = `${date.getDate()}`.padStart(2, "0");
  return `${year}-${month}-${day}`;
}

/** Matches per local calendar day for the trailing `days` days (oldest first). */
export function dailyActivity(matches: Match[], days: number, now = new Date()): DailyActivity[] {
  const activity = new Map<string, Omit<DailyActivity, "date" | "label">>();
  for (const match of matches) {
    const started = new Date(match.startedAt);
    if (Number.isNaN(started.getTime())) continue;

    const key = localDateKey(started);
    const totals = activity.get(key) ?? {
      count: 0,
      wins: 0,
      losses: 0,
      unknown: 0,
      trackedSeconds: 0,
      timedMatches: 0,
      constructed: 0,
      limited: 0,
    };

    totals.count += 1;
    totals[match.result === "win" ? "wins" : match.result === "loss" ? "losses" : "unknown"] += 1;
    if (match.secondsCount != null && match.secondsCount > 0) {
      totals.trackedSeconds += match.secondsCount;
      totals.timedMatches += 1;
    }
    totals[isLimitedEvent(match.eventName) ? "limited" : "constructed"] += 1;
    activity.set(key, totals);
  }

  const out: DailyActivity[] = [];
  for (let offset = days - 1; offset >= 0; offset -= 1) {
    const date = new Date(now.getFullYear(), now.getMonth(), now.getDate() - offset);
    const key = localDateKey(date);
    out.push({
      date: key,
      label: date.toLocaleDateString(undefined, { month: "short", day: "numeric" }),
      ...(activity.get(key) ?? {
        count: 0,
        wins: 0,
        losses: 0,
        unknown: 0,
        trackedSeconds: 0,
        timedMatches: 0,
        constructed: 0,
        limited: 0,
      }),
    });
  }
  return out;
}

export type MatchAverages = {
  seconds: number | null;
  turns: number | null;
};

export function matchAverages(matches: Match[]): MatchAverages {
  let secondsTotal = 0;
  let secondsCount = 0;
  let turnsTotal = 0;
  let turnsCount = 0;
  for (const match of matches) {
    if (match.secondsCount != null && match.secondsCount > 0) {
      secondsTotal += match.secondsCount;
      secondsCount += 1;
    }
    if (match.turnCount != null && match.turnCount > 0) {
      turnsTotal += match.turnCount;
      turnsCount += 1;
    }
  }
  return {
    seconds: secondsCount > 0 ? Math.round(secondsTotal / secondsCount) : null,
    turns: turnsCount > 0 ? Math.round(turnsTotal / turnsCount) : null,
  };
}

export type ScheduleBucket = {
  /** Weekday index (0 = Sunday) for weekdays, bucket start hour for time slots. */
  key: number;
  label: string;
  /** Local clock range, e.g. "4 PM – 8 PM"; empty for weekday buckets. */
  range: string;
  record: WinLossRecord;
  /** Every match in the bucket, including undecided ones. */
  matches: number;
};

/** Sunday-first so the ordering matches the activity graph rows. */
const WEEKDAY_REFERENCE = new Date(2024, 0, 7);

const TIME_SLOT_HOURS = 4;
const TIME_SLOT_LABELS = [
  "Late night",
  "Early morning",
  "Morning",
  "Afternoon",
  "Evening",
  "Night",
];

function emptyBuckets(count: number, label: (index: number) => Omit<ScheduleBucket, "record" | "matches">): ScheduleBucket[] {
  return Array.from({ length: count }, (_unused, index) => ({
    ...label(index),
    record: { wins: 0, losses: 0 },
    matches: 0,
  }));
}

function tallyByBucket(
  matches: Match[],
  buckets: ScheduleBucket[],
  bucketOf: (started: Date) => number,
): ScheduleBucket[] {
  for (const match of matches) {
    const started = new Date(match.startedAt);
    if (Number.isNaN(started.getTime())) continue;

    const bucket = buckets[bucketOf(started)];
    if (!bucket) continue;

    bucket.matches += 1;
    if (match.result === "win") bucket.record.wins += 1;
    else if (match.result === "loss") bucket.record.losses += 1;
  }
  return buckets;
}

/** Record per local weekday, Sunday first. */
export function weekdayPerformance(matches: Match[]): ScheduleBucket[] {
  const weekdayFormatter = new Intl.DateTimeFormat(undefined, { weekday: "long" });
  const buckets = emptyBuckets(7, (index) => ({
    key: index,
    label: weekdayFormatter.format(
      new Date(
        WEEKDAY_REFERENCE.getFullYear(),
        WEEKDAY_REFERENCE.getMonth(),
        WEEKDAY_REFERENCE.getDate() + index,
      ),
    ),
    range: "",
  }));
  return tallyByBucket(matches, buckets, (started) => started.getDay());
}

/** Record per four-hour block of the local day, starting at midnight. */
export function timeOfDayPerformance(matches: Match[]): ScheduleBucket[] {
  const hourFormatter = new Intl.DateTimeFormat(undefined, { hour: "numeric" });
  const hourLabel = (hour: number) =>
    hourFormatter.format(new Date(2024, 0, 7, hour % 24));
  const buckets = emptyBuckets(TIME_SLOT_LABELS.length, (index) => {
    const start = index * TIME_SLOT_HOURS;
    return {
      key: start,
      label: TIME_SLOT_LABELS[index],
      range: `${hourLabel(start)} – ${hourLabel(start + TIME_SLOT_HOURS)}`,
    };
  });
  return tallyByBucket(matches, buckets, (started) =>
    Math.floor(started.getHours() / TIME_SLOT_HOURS),
  );
}
