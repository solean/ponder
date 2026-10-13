import { useQuery } from "@tanstack/react-query";

import { api } from "./api";
import { collectSetCodes } from "./events";
import type { SetInfo } from "./types";

export type SetLookup = (code?: string | null) => SetInfo | undefined;

/**
 * Resolves friendly set metadata for every set code referenced by the given
 * event names in a single batched request. Returns a `lookup(code)` helper that
 * pages thread into <EventLabel>. Set metadata is effectively static, so it is
 * cached aggressively.
 */
export function useEventSets(eventNames: Array<string | null | undefined>): {
  lookup: SetLookup;
  isLoading: boolean;
} {
  return useSetCodes(collectSetCodes(eventNames));
}

/** Resolves the same cached metadata for explicit set filters. */
export function useSetCodes(setCodes: string[]): {
  lookup: SetLookup;
  isLoading: boolean;
} {
  const codes = [...new Set(setCodes.filter(Boolean).map((code) => code.toLowerCase()))].sort();
  const query = useQuery({
    queryKey: ["sets", codes],
    queryFn: () => api.sets(codes),
    enabled: codes.length > 0,
    staleTime: 1000 * 60 * 60 * 24,
  });

  const lookup: SetLookup = (code) => {
    if (!code) return undefined;
    return query.data?.[code.toLowerCase()];
  };

  return { lookup, isLoading: query.isLoading };
}

/**
 * Newest release first; sets without a known release date follow by code, and
 * the unknown-set group ("") stays last. Dates are ISO strings, so they compare lexically.
 */
export function sortSetCodesByRelease(setCodes: string[], lookup: SetLookup): string[] {
  return [...setCodes].sort((a, b) => {
    if (!a || !b) return Number(!a) - Number(!b);
    const aReleased = lookup(a)?.releasedAt ?? "";
    const bReleased = lookup(b)?.releasedAt ?? "";
    return bReleased.localeCompare(aReleased) || a.localeCompare(b);
  });
}

/** Set picker option text: "Name (CODE)", bare "CODE", or "Unknown set" for "". */
export function setOptionLabel(code: string, lookup: SetLookup): string {
  if (!code) return "Unknown set";
  const name = lookup(code)?.name;
  return name ? `${name} (${code.toUpperCase()})` : code.toUpperCase();
}
