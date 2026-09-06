import { useEffect, useRef } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";

import { api } from "../api";
import type { MatchReplayFrame, MatchReplayStatus } from "../types";

type ReplayCacheData = {
  revision: string;
  frames: MatchReplayFrame[];
};

type ObservedRevision = {
  matchId: number;
  revision: string;
};

const selectFrames = (data: ReplayCacheData) => data.frames;

export function useMatchReplay(matchId: number, enabled: boolean) {
  const queryClient = useQueryClient();
  const requestedRevision = useRef<ObservedRevision | null>(null);
  const observedRevision = useRef<ObservedRevision | null>(null);
  const statusQuery = useQuery({
    queryKey: ["match-replay-status", matchId],
    queryFn: ({ signal }) => api.matchReplayStatus(matchId, signal),
    enabled,
    staleTime: 0,
    gcTime: 60_000,
    refetchInterval: (query) => query.state.data?.complete ? 10_000 : 2_000,
    refetchIntervalInBackground: false,
  });
  // Recheck status on entry before starting any network payload load. A cached
  // payload stays visible while this small request is pending or fails.
  const canFetchReplay = enabled && statusQuery.isSuccess && statusQuery.isFetchedAfterMount;
  const replayQuery = useQuery({
    queryKey: ["match-replay", matchId],
    queryFn: async ({ signal }): Promise<ReplayCacheData> => {
      const status = queryClient.getQueryData<MatchReplayStatus>(["match-replay-status", matchId]);
      if (!status) {
        throw new Error("Replay status is unavailable.");
      }
      // Never stamp a response with a status that arrived during its request.
      const revision = status.revision;
      requestedRevision.current = { matchId, revision };
      const frames = await api.matchReplay(matchId, signal);
      return { revision, frames };
    },
    enabled: canFetchReplay,
    staleTime: statusQuery.data?.complete ? 5 * 60_000 : Infinity,
    gcTime: 60_000,
    select: selectFrames,
  });
  const revision = statusQuery.data?.revision;

  useEffect(() => {
    if (!enabled) {
      requestedRevision.current = null;
      void queryClient.cancelQueries({ queryKey: ["match-replay", matchId], exact: true });
      void queryClient.cancelQueries({ queryKey: ["match-replay-status", matchId], exact: true });
    }
  }, [enabled, matchId, queryClient]);

  useEffect(() => {
    if (!canFetchReplay || revision === undefined) {
      return;
    }
    const replayKey = ["match-replay", matchId];
    const cached = queryClient.getQueryData<ReplayCacheData>(replayKey);
    const previous = observedRevision.current;
    const previousRevision = previous?.matchId === matchId ? previous.revision : cached?.revision;
    observedRevision.current = { matchId, revision };
    if (previousRevision !== undefined && previousRevision !== revision) {
      // Refresh these only on a revision change, not on every status poll.
      void queryClient.invalidateQueries({ queryKey: ["match-detail", matchId], exact: true });
      void queryClient.invalidateQueries({ queryKey: ["match-timeline", matchId], exact: true });
    }

    const requested = requestedRevision.current;
    if (
      cached?.revision === revision ||
      queryClient.getQueryState(replayKey)?.fetchStatus !== "idle" ||
      (requested?.matchId === matchId && requested.revision === revision)
    ) {
      return;
    }
    // Let an old request settle, then compare again. Do not keep cancelling a
    // slow request at every poll, or retry a failed revision outside Query's
    // retry policy. Only one full payload is retained for each match.
    void queryClient.invalidateQueries(
      { queryKey: replayKey, exact: true },
      { cancelRefetch: false },
    );
  }, [
    canFetchReplay,
    matchId,
    queryClient,
    revision,
    replayQuery.dataUpdatedAt,
    replayQuery.errorUpdatedAt,
    replayQuery.fetchStatus,
  ]);

  const error = statusQuery.error ?? replayQuery.error;
  return {
    data: replayQuery.data,
    isPending: replayQuery.isPending && !error,
    error,
  };
}
