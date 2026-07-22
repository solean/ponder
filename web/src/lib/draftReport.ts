import { parseEventName } from "./events";
import type { DraftPick, DraftPickCard, DraftSession } from "./types";

export type DraftSessionStatus =
  | "complete"
  | "in-progress"
  | "recording-incomplete"
  | "empty";

export type DraftPickLogPick = {
  pickNumber: number;
  displayPick: number;
  pickedCards: DraftPickCard[];
};

export type DraftPickLogPack = {
  packNumber: number;
  displayPack: number;
  picks: DraftPickLogPick[];
};

function validDateValue(timestamp?: string | null): number | null {
  if (!timestamp) {
    return null;
  }
  const value = new Date(timestamp).getTime();
  return Number.isFinite(value) ? value : null;
}

function parseDraftCardIDs(raw: string): number[] {
  const trimmed = raw.trim();
  if (!trimmed || trimmed === "[]") {
    return [];
  }

  try {
    const decoded = JSON.parse(trimmed) as unknown;
    if (Array.isArray(decoded)) {
      return decoded
        .map((value) => {
          if (typeof value === "number" && Number.isFinite(value)) {
            return Math.trunc(value);
          }
          if (typeof value === "string") {
            const parsed = Number.parseInt(value.trim(), 10);
            return Number.isFinite(parsed) ? parsed : 0;
          }
          return 0;
        })
        .filter((cardID) => cardID > 0);
    }
  } catch {
    // Fall through to the tolerant legacy-log parser.
  }

  return (trimmed.match(/\d+/g) ?? [])
    .map((value) => Number.parseInt(value, 10))
    .filter((cardID) => Number.isFinite(cardID) && cardID > 0);
}

function normalizedPickedCards(pick: DraftPick): DraftPickCard[] {
  if (Array.isArray(pick.pickedCards) && pick.pickedCards.length > 0) {
    return pick.pickedCards.map((card) => ({
      cardId: card.cardId,
      cardName: card.cardName,
    }));
  }

  return parseDraftCardIDs(pick.pickedCardIds).map((cardId) => ({ cardId }));
}

export function draftPickLogPacks(picks: DraftPick[]): DraftPickLogPack[] {
  const packOffset = picks.some((pick) => pick.packNumber === 0) ? 1 : 0;
  const pickOffset = picks.some((pick) => pick.pickNumber === 0) ? 1 : 0;
  const grouped = new Map<number, DraftPickLogPick[]>();

  for (const pick of picks) {
    const rows = grouped.get(pick.packNumber) ?? [];
    rows.push({
      pickNumber: pick.pickNumber,
      displayPick: pick.pickNumber + pickOffset,
      pickedCards: normalizedPickedCards(pick),
    });
    grouped.set(pick.packNumber, rows);
  }

  return [...grouped.entries()]
    .sort((a, b) => a[0] - b[0])
    .map(([packNumber, rows]) => ({
      packNumber,
      displayPack: packNumber + packOffset,
      picks: rows.sort((a, b) => a.pickNumber - b.pickNumber),
    }));
}

export function draftSessionStatus(session: DraftSession): DraftSessionStatus {
  if (validDateValue(session.completedAt) != null) {
    return "complete";
  }
  if (validDateValue(session.startedAt) != null) {
    return "in-progress";
  }
  if (session.picks > 0) {
    return "recording-incomplete";
  }
  return "empty";
}

export function draftSessionStatusLabel(status: DraftSessionStatus): string {
  switch (status) {
    case "complete":
      return "Complete";
    case "in-progress":
      return "In progress";
    case "recording-incomplete":
      return "Recording incomplete";
    case "empty":
      return "No picks recorded";
  }
}

export function draftSessionDurationSeconds(session: DraftSession): number | null {
  const startedAt = validDateValue(session.startedAt);
  const completedAt = validDateValue(session.completedAt);
  if (startedAt == null || completedAt == null || completedAt <= startedAt) {
    return null;
  }
  return Math.floor((completedAt - startedAt) / 1000);
}

export function draftSessionType(session: DraftSession): string {
  const parsed = parseEventName(session.eventName);
  if (parsed.kindLabel !== "Unknown event") {
    return parsed.kindLabel;
  }
  return session.isBotDraft ? "Bot Draft" : "Player Draft";
}

export function draftSessionRecord(session: DraftSession): {
  label: string;
  winRate: number | null;
} | null {
  if (session.wins == null || session.losses == null) {
    return null;
  }
  const games = session.wins + session.losses;
  return {
    label: `${session.wins}–${session.losses}`,
    winRate: games > 0 ? session.wins / games : null,
  };
}

export function draftReplayCoverage(picks: DraftPick[]): {
  covered: number;
  total: number;
} {
  return {
    covered: picks.filter((pick) => (pick.packCards?.length ?? 0) > 0).length,
    total: picks.length,
  };
}
