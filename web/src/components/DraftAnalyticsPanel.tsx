import { useState } from "react";
import { useQuery } from "@tanstack/react-query";

import { PerformanceAnalyticsPanel } from "./DeckAnalyticsPanel";
import { StatusMessage } from "./StatusMessage";
import { api } from "../lib/api";
import { useSetCodes } from "../lib/useEventSets";

export function DraftAnalyticsPanel() {
  const [set, setSet] = useState<string | undefined>(undefined);
  // Keep the unfiltered catalog available when a filtered request fails or is empty.
  const allQuery = useQuery({
    queryKey: ["draft-analytics", null],
    queryFn: () => api.draftAnalytics(),
  });
  const query = useQuery({
    queryKey: ["draft-analytics", set ?? null],
    queryFn: () => api.draftAnalytics(set),
  });
  const setCodes = query.data?.sets ?? allQuery.data?.sets ?? [];
  const { lookup } = useSetCodes(setCodes);
  // Newest release first; sets without a known release date follow by code, and
  // the unknown-set group ("") stays last. Dates are ISO strings, so they compare lexically.
  const sets = [...setCodes].sort((a, b) => {
    if (!a || !b) return Number(!a) - Number(!b);
    const aReleased = lookup(a)?.releasedAt ?? "";
    const bReleased = lookup(b)?.releasedAt ?? "";
    return bReleased.localeCompare(aReleased) || a.localeCompare(b);
  });

  return (
    <div className="stack-lg">
      <section className="panel">
        <div className="panel-head deck-analytics-head">
          <label className="deck-analytics-version-select">
            <span>Draft set</span>
            <select
              value={set === undefined ? "all" : `set:${set}`}
              onChange={(event) => {
                const value = event.target.value;
                setSet(value === "all" ? undefined : value.slice(4));
              }}
            >
              <option value="all">All sets</option>
              {sets.map((code) => (
                <option key={code} value={`set:${code}`}>
                  {code ? (lookup(code)?.name ? `${lookup(code)?.name} (${code.toUpperCase()})` : code.toUpperCase()) : "Unknown set"}
                </option>
              ))}
            </select>
          </label>
          {query.error || allQuery.error ? (
            <button type="button" className="text-link" onClick={() => {
              void query.refetch();
              if (set !== undefined) void allQuery.refetch();
            }}>
              Retry analytics
            </button>
          ) : null}
        </div>
        {allQuery.error && set !== undefined ? (
          <StatusMessage tone="error">Could not load the complete draft set list.</StatusMessage>
        ) : null}
      </section>
      <PerformanceAnalyticsPanel
        key={set === undefined ? "all" : `set:${set}`}
        scope={{ kind: "drafts", set }}
        data={query.data?.analytics}
        isLoading={query.isLoading}
        error={query.error}
      />
    </div>
  );
}
