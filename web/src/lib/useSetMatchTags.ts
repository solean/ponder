import { useMutation, useQueryClient } from "@tanstack/react-query";

import { api } from "./api";
import type { MatchDetail, MatchList } from "./types";

type Variables = { matchId: number; tags: string[] };
type Snapshot = { list?: MatchList; detail?: MatchDetail };

/** Replaces a match's tags, updating the list and detail caches optimistically. */
export function useSetMatchTags() {
  const queryClient = useQueryClient();

  return useMutation<{ tags: string[] }, Error, Variables, Snapshot>({
    mutationFn: ({ matchId, tags }) => api.setMatchTags(matchId, tags),
    onMutate: async ({ matchId, tags }) => {
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
          matches: snapshot.list.matches.map((match) => (match.id === matchId ? { ...match, tags } : match)),
        });
      }
      if (snapshot.detail) {
        queryClient.setQueryData<MatchDetail>(["match-detail", matchId], {
          ...snapshot.detail,
          match: { ...snapshot.detail.match, tags },
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
      void queryClient.invalidateQueries({ queryKey: ["match-tags"] });
    },
  });
}
