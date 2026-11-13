import { useMutation, useQueryClient } from "@tanstack/react-query";

import { api } from "./api";
import type { MatchDetail, MatchList } from "./types";

type Variables = { matchId: number; saved: boolean };
type Snapshot = { list?: MatchList; detail?: MatchDetail };

/** Saves or unsaves a match, updating the list and detail caches optimistically. */
export function useSetMatchSaved() {
  const queryClient = useQueryClient();

  return useMutation<{ saved: boolean }, Error, Variables, Snapshot>({
    mutationFn: ({ matchId, saved }) => api.setMatchSaved(matchId, saved),
    onMutate: async ({ matchId, saved }) => {
      await Promise.all([
        queryClient.cancelQueries({ queryKey: ["matches"] }),
        queryClient.cancelQueries({ queryKey: ["match-detail", matchId] }),
      ]);
      const snapshot: Snapshot = {
        list: queryClient.getQueryData<MatchList>(["matches"]),
        detail: queryClient.getQueryData<MatchDetail>(["match-detail", matchId]),
      };
      if (snapshot.list) {
        queryClient.setQueryData<MatchList>(["matches"], {
          ...snapshot.list,
          matches: snapshot.list.matches.map((match) => (match.id === matchId ? { ...match, saved } : match)),
        });
      }
      if (snapshot.detail) {
        queryClient.setQueryData<MatchDetail>(["match-detail", matchId], {
          ...snapshot.detail,
          match: { ...snapshot.detail.match, saved },
        });
      }
      return snapshot;
    },
    onError: (_error, { matchId }, snapshot) => {
      if (snapshot?.list) queryClient.setQueryData(["matches"], snapshot.list);
      if (snapshot?.detail) queryClient.setQueryData(["match-detail", matchId], snapshot.detail);
    },
    onSettled: (_data, _error, { matchId }) => {
      void queryClient.invalidateQueries({ queryKey: ["matches"] });
      void queryClient.invalidateQueries({ queryKey: ["match-detail", matchId] });
    },
  });
}
