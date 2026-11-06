import { useQuery } from "@tanstack/react-query";

import { PerformanceAnalyticsPanel } from "./DeckAnalyticsPanel";
import { api } from "../lib/api";

/**
 * Draft performance analytics for one set and format; a null set or format
 * means every set or format.
 */
export function DraftAnalyticsPanel({ set, format }: { set: string | null; format: string | null }) {
  const query = useQuery({
    queryKey: ["draft-analytics", set, format],
    queryFn: () => api.draftAnalytics(set ?? undefined, format ?? undefined),
  });

  return (
    <PerformanceAnalyticsPanel
      key={`${set ?? "all"}:${format ?? "all"}`}
      scope={{ kind: "drafts", set: set ?? undefined, format: format ?? undefined }}
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
