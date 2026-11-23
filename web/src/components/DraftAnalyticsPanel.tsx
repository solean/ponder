import { useQuery } from "@tanstack/react-query";

import { PerformanceAnalyticsPanel } from "./DeckAnalyticsPanel";
import { api } from "../lib/api";
import { useQueryError } from "../lib/useQueryError";

/**
 * Draft performance analytics for one set and format; a null set or format
 * means every set or format.
 */
export function DraftAnalyticsPanel({ set, format }: { set: string | null; format: string | null }) {
  const query = useQuery({
    queryKey: ["draft-analytics", set, format],
    queryFn: () => api.draftAnalytics(set ?? undefined, format ?? undefined),
  });
  const analyticsError = useQueryError(query);

  return (
    <PerformanceAnalyticsPanel
      key={`${set ?? "all"}:${format ?? "all"}`}
      scope={{ kind: "drafts", set: set ?? undefined, format: format ?? undefined }}
      data={query.data?.analytics}
      isLoading={query.isLoading}
      errorView={analyticsError}
    />
  );
}
