import type {
  AiStatus,
  AutostartStatus,
  DeckAnalytics,
  DeckAnalyticsGameRef,
  DeckAnalyticsGamesParams,
  DeckDetail,
  DeckPrimer,
  DeckSummary,
  DraftPick,
  DraftSession,
  EconomyHistory,
  Match,
  MatchCardPlay,
  MatchDetail,
  MatchReplayFrame,
  DeckMatchupsResponse,
  LimitedMatchupsResponse,
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
  economy: () => getJSON<EconomyHistory>("/api/economy"),
  matches: (limit = 500) => getJSON<Match[]>(`/api/matches?limit=${limit}`),
  matchDetail: (matchId: number) => getJSON<MatchDetail>(`/api/matches/${matchId}`),
  matchTimeline: (matchId: number) => getJSON<MatchCardPlay[]>(`/api/matches/${matchId}/timeline`),
  matchReplay: (matchId: number) => getJSON<MatchReplayFrame[]>(`/api/matches/${matchId}/replay`),
  decks: (scope: "constructed" | "draft" | "all" = "constructed") =>
    getJSON<DeckSummary[]>(scope === "constructed" ? "/api/decks" : `/api/decks?scope=${scope}`),
  deckDetail: (deckId: number) => getJSON<DeckDetail>(`/api/decks/${deckId}`),
  deckAnalytics: (deckId: number, versionId?: number) =>
    getJSON<DeckAnalytics>(
      versionId ? `/api/decks/${deckId}/analytics?version=${versionId}` : `/api/decks/${deckId}/analytics`,
    ),
  deckAnalyticsGames: (deckId: number, params: DeckAnalyticsGamesParams) => {
    const search = new URLSearchParams();
    if (params.version) search.set("version", String(params.version));
    if (params.card) search.set("card", String(params.card));
    if (params.facet) search.set("facet", params.facet);
    if (params.keptSize != null) search.set("keptSize", String(params.keptSize));
    if (params.mulligans != null) search.set("mulligans", String(params.mulligans));
    if (params.game) search.set("game", params.game);
    if (params.playDraw) search.set("playDraw", params.playDraw);
    const query = search.toString();
    return getJSON<DeckAnalyticsGameRef[]>(
      query ? `/api/decks/${deckId}/analytics/games?${query}` : `/api/decks/${deckId}/analytics/games`,
    );
  },
  deckMatchups: (deckId: number) => getJSON<DeckMatchupsResponse>(`/api/decks/${deckId}/matchups`),
  limitedMatchups: () => getJSON<LimitedMatchupsResponse>("/api/limited/matchups"),
  setOpponentArchetype: (matchId: number, archetype: string) =>
    postJSON<{ status: string; archetype: string }>(`/api/matches/${matchId}/opponent-archetype`, { archetype }),
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
  pickLogFile: () => postJSON<{ path: string }>("/api/runtime/pick-log"),
  revealPath: (path: string) => postJSON<{ status: string }>("/api/runtime/reveal", { path }),
  aiStatus: () => getJSON<AiStatus>("/api/ai/status"),
  deckPrimer: async (deckId: number): Promise<DeckPrimer | null> => {
    const res = await fetch(`${API_BASE}/api/decks/${deckId}/primer`);
    if (res.status === 404) {
      return null;
    }
    if (!res.ok) {
      const text = await res.text();
      throw new Error(`Request failed (${res.status}): ${text}`);
    }
    return (await res.json()) as DeckPrimer;
  },
};

export type PrimerStreamHandlers = {
  onDelta: (text: string) => void;
  onDone: (primer: DeckPrimer) => void;
  onError: (message: string) => void;
};

/**
 * Generates a deck primer, streaming progress via Server-Sent Events over a
 * POST fetch (EventSource only supports GET). The Accept header also tells
 * the backend to skip gzip so events arrive incrementally.
 */
export async function generateDeckPrimer(
  deckId: number,
  handlers: PrimerStreamHandlers,
  signal?: AbortSignal,
): Promise<void> {
  let res: Response;
  try {
    res = await fetch(`${API_BASE}/api/decks/${deckId}/primer`, {
      method: "POST",
      headers: { Accept: "text/event-stream" },
      signal,
    });
  } catch (err) {
    if (signal?.aborted) return;
    handlers.onError(err instanceof Error ? err.message : String(err));
    return;
  }

  if (!res.ok || !res.body) {
    const text = await res.text().catch(() => "");
    try {
      const parsed = JSON.parse(text) as { error?: string };
      handlers.onError(parsed.error ?? `Request failed (${res.status})`);
    } catch {
      handlers.onError(`Request failed (${res.status}): ${text}`);
    }
    return;
  }

  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let finished = false;

  const dispatch = (rawEvent: string) => {
    let eventName = "message";
    const dataLines: string[] = [];
    for (const line of rawEvent.split("\n")) {
      if (line.startsWith("event:")) {
        eventName = line.slice(6).trim();
      } else if (line.startsWith("data:")) {
        dataLines.push(line.slice(5).trimStart());
      }
    }
    if (dataLines.length === 0) return;
    const data = dataLines.join("\n");
    if (eventName === "delta") {
      handlers.onDelta(JSON.parse(data) as string);
    } else if (eventName === "done") {
      finished = true;
      handlers.onDone(JSON.parse(data) as DeckPrimer);
    } else if (eventName === "error") {
      finished = true;
      handlers.onError((JSON.parse(data) as { error: string }).error);
    }
  };

  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });
      let sep: number;
      while ((sep = buffer.indexOf("\n\n")) !== -1) {
        const rawEvent = buffer.slice(0, sep);
        buffer = buffer.slice(sep + 2);
        dispatch(rawEvent);
      }
    }
    if (!finished) {
      handlers.onError("Generation stream ended unexpectedly.");
    }
  } catch (err) {
    if (signal?.aborted) return;
    if (!finished) {
      handlers.onError(err instanceof Error ? err.message : String(err));
    }
  }
}
