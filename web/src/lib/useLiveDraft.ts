import { useEffect } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./api";
import type { LiveDraft } from "./types";

const queryKey = ["live-draft"];

export function useLiveDraft() {
  const client = useQueryClient();
  const query = useQuery({
    queryKey,
    queryFn: ({ signal }) => api.liveDraft(signal),
    // Fallback for browser development and events missed while hidden.
    refetchInterval: (query) => (query.state.data?.draft ? 1000 : 5000),
    refetchIntervalInBackground: true,
  });

  useEffect(() => {
    const onDraftChanged = (event: Event) => {
      const draft = (event as CustomEvent<LiveDraft | null>).detail;
      // Cancel first, including the initial fetch: an older response must not
      // restore a picked pack. The native snapshot deliberately has no display
      // metadata, keeping badges hidden until the current pack is enriched.
      void client.cancelQueries({ queryKey });
      client.setQueryData(queryKey, { draft });
      void client.invalidateQueries({ queryKey });
    };
    window.addEventListener("ponder:draft-changed", onDraftChanged);
    return () => window.removeEventListener("ponder:draft-changed", onDraftChanged);
  }, [client]);

  return query;
}
