import { useQuery } from "@tanstack/react-query";

import { PerformanceAnalyticsPanel } from "./DeckAnalyticsPanel";
import { api } from "../lib/api";

/** Draft performance analytics for one set, or every set when `set` is null. */
export function DraftAnalyticsPanel({ set }: { set: string | null }) {
  const query = useQuery({
    queryKey: ["draft-analytics", set],
    queryFn: () => api.draftAnalytics(set ?? undefined),
  });

  return (
    <PerformanceAnalyticsPanel
      key={set ?? "all"}
      scope={{ kind: "drafts", set: set ?? undefined }}
      data={query.data?.analytics}
      isLoading={query.isLoading}
      error={query.error}
      headerControls={
        query.error ? (
          <button type="button" className="text-link" onClick={() => void query.refetch()}>
            Retry analytics
          </button>
        ) : null
      }
    />
  );
}
