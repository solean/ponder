import type {
  AutostartStatus,
  DeckDetail,
  DeckSummary,
  DraftPick,
  DraftSession,
  Match,
  MatchCardPlay,
  MatchDetail,
  MatchReplayFrame,
  Overview,
  RankHistoryPoint,
  RuntimeConfig,
  RuntimeOperation,
  LiveMatch,
  RuntimeStatus,
  SetInfo,
  UpdateCheck,
} from "./types";

const API_BASE = import.meta.env.VITE_API_BASE ?? "";

async function getJSON<T>(path: string): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`);
  if (!res.ok) {
    const text = await res.text();
    throw new Error(`Request failed (${res.status}): ${text}`);
  }
  return (await res.json()) as T;
}

async function postJSON<T>(path: string, body?: unknown): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: body == null ? undefined : JSON.stringify(body),
  });
  if (!res.ok) {
    const text = await res.text();
    throw new Error(`Request failed (${res.status}): ${text}`);
  }
  return (await res.json()) as T;
}

export const api = {
  overview: () => getJSON<Overview>("/api/overview"),
  rankHistory: () => getJSON<RankHistoryPoint[]>("/api/rank-history"),
  matches: (limit = 500) => getJSON<Match[]>(`/api/matches?limit=${limit}`),
  matchDetail: (matchId: number) => getJSON<MatchDetail>(`/api/matches/${matchId}`),
  matchTimeline: (matchId: number) => getJSON<MatchCardPlay[]>(`/api/matches/${matchId}/timeline`),
  matchReplay: (matchId: number) => getJSON<MatchReplayFrame[]>(`/api/matches/${matchId}/replay`),
  decks: (scope: "constructed" | "draft" | "all" = "constructed") =>
    getJSON<DeckSummary[]>(scope === "constructed" ? "/api/decks" : `/api/decks?scope=${scope}`),
  deckDetail: (deckId: number) => getJSON<DeckDetail>(`/api/decks/${deckId}`),
  drafts: () => getJSON<DraftSession[]>("/api/drafts"),
  draftPicks: (draftId: number) => getJSON<DraftPick[]>(`/api/drafts/${draftId}/picks`),
  sets: (codes: string[]) =>
    getJSON<Record<string, SetInfo>>(`/api/sets?codes=${encodeURIComponent(codes.join(","))}`),
  live: () => getJSON<{ live: LiveMatch | null }>("/api/live"),
  runtimeStatus: () => getJSON<RuntimeStatus>("/api/runtime/status"),
  saveRuntimeConfig: (config: RuntimeConfig) => postJSON<RuntimeStatus>("/api/runtime/config", config),
  runImport: (resume = true) => postJSON<RuntimeOperation>("/api/runtime/import", { resume }),
  startLive: () => postJSON<RuntimeStatus>("/api/runtime/live/start"),
  stopLive: () => postJSON<RuntimeStatus>("/api/runtime/live/stop"),
  autostartStatus: () => getJSON<AutostartStatus>("/api/runtime/autostart"),
  setAutostart: (enabled: boolean) => postJSON<AutostartStatus>("/api/runtime/autostart", { enabled }),
  checkForUpdate: () => getJSON<UpdateCheck>("/api/runtime/update-check"),
};
