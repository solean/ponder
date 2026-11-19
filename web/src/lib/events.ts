import type { SetInfo } from "./types";

/**
 * Arena event names are raw IDs that come in a few shapes:
 *   - "QuickDraft_TMT_20260313" (Type_SET_DATE)
 *   - "FIN_Quick_Draft"        (SET_Type)
 *   - "Ladder", "Play", "Traditional_Ladder" (no set)
 *
 * This module turns those into structured, display-ready pieces so every page
 * renders event names the same way (friendly kind + set name + set symbol).
 */

const DATE_TOKEN = /^(\d{4})(\d{2})(\d{2})$/;
// Set codes are short all-caps alphanumerics (TMT, FIN, ECL, Y25); this
// deliberately excludes mixed-case type words like "Quick" or "Draft".
const SET_CODE_TOKEN = /^[A-Z0-9]{2,5}$/;

export interface ParsedEvent {
  raw: string;
  /** Uppercase set code (e.g. "TMT"), or null for non-set events. */
  setCode: string | null;
  /** Friendly event kind, e.g. "Quick Draft" or "Ranked Ladder". */
  kindLabel: string;
  /** Stable category for filtering/grouping (same vocabulary as kindLabel). */
  category: string;
  /** Milliseconds (UTC) parsed from an embedded date, if present. */
  dateValue: number | null;
}

/**
 * Maps a normalized event name (lowercased, underscores stripped) to a friendly
 * kind. Order matters: more specific patterns must come before broader ones.
 */
function classify(normalized: string): string | null {
  if (!normalized) return null;
  if (normalized.includes("picktwo")) return "Pick Two Draft";
  if (normalized.includes("traddraft") || normalized.includes("traditionaldraft")) return "Traditional Draft";
  if (normalized.includes("quickdraft")) return "Quick Draft";
  if (normalized.includes("premierdraft")) return "Premier Draft";
  if (normalized.includes("botdraft")) return "Bot Draft";
  if (normalized.includes("playerdraft")) return "Player Draft";
  if (normalized.includes("draft")) return "Other Draft";
  if (normalized.includes("tradsealed") || normalized.includes("traditionalsealed")) return "Traditional Sealed";
  if (normalized.includes("premiersealed")) return "Premier Sealed";
  if (normalized.includes("sealed")) return "Sealed";
  if (normalized.includes("jumpin")) return "Jump In";
  if (normalized.includes("tradladder") || normalized.includes("traditionalladder")) return "Traditional Ladder";
  if (normalized.includes("ladder")) return "Ranked Ladder";
  if (normalized.includes("midweek") || normalized.startsWith("mwm")) return "Midweek Magic";
  if (normalized.includes("brawl")) return "Brawl";
  if (normalized === "play") return "Play";
  return null;
}

export function parseEventName(raw?: string | null): ParsedEvent {
  const value = (raw ?? "").trim();
  const empty: ParsedEvent = {
    raw: value,
    setCode: null,
    kindLabel: "Unknown event",
    category: "Unknown",
    dateValue: null,
  };
  if (!value) return empty;

  const tokens = value.split("_");
  let setCode: string | null = null;
  let dateValue: number | null = null;

  for (const token of tokens) {
    const dateMatch = token.match(DATE_TOKEN);
    if (dateMatch) {
      const [, year, month, day] = dateMatch;
      dateValue = Date.UTC(Number(year), Number(month) - 1, Number(day));
      continue;
    }
    if (!setCode && SET_CODE_TOKEN.test(token)) {
      setCode = token;
    }
  }

  const normalized = value.toLowerCase().replace(/_/g, "");
  const classified = classify(normalized);
  // Unrecognized events fall back to a prettified raw name so they stay readable.
  const kindLabel = classified ?? value.replace(/_/g, " ");

  return {
    raw: value,
    setCode,
    kindLabel,
    category: classified ?? value.replace(/_/g, " "),
    dateValue,
  };
}

/** Stable category for the Matches event filter. */
export function eventCategory(raw?: string | null): string {
  return parseEventName(raw).category;
}

/**
 * Human-readable event name. Prefers the resolved set name; falls back to the
 * set code while metadata is still loading, and to just the kind for non-set
 * events.
 */
export function eventDisplayName(parsed: ParsedEvent, setInfo?: SetInfo | null): string {
  const set = setInfo?.name ?? parsed.setCode;
  if (parsed.kindLabel === "Unknown event") {
    return set ?? parsed.kindLabel;
  }
  return set ? `${parsed.kindLabel} · ${set}` : parsed.kindLabel;
}

/** Distinct uppercase set codes referenced by a list of event names. */
export function collectSetCodes(eventNames: Array<string | null | undefined>): string[] {
  const codes = new Set<string>();
  for (const name of eventNames) {
    const code = parseEventName(name).setCode;
    if (code) codes.add(code);
  }
  return [...codes];
}
