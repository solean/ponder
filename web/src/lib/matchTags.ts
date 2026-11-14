/** Mirrors the server's db.MaxMatchTagLength and db.MaxMatchTags. */
export const MAX_MATCH_TAG_LENGTH = 40;
export const MAX_MATCH_TAGS = 20;

/** Offered when no match has been tagged yet. */
export const STARTER_MATCH_TAGS = ["land flooded", "mana screwed", "misplayed", "mulligan"];

/** Same normalisation as the server: trimmed, single-spaced, lowercase. */
export function normalizeMatchTag(raw: string): string {
  return raw.trim().split(/\s+/).join(" ").toLowerCase().slice(0, MAX_MATCH_TAG_LENGTH);
}

/** Adds a tag to a sorted set, ignoring blanks and duplicates. */
export function withMatchTag(tags: string[], raw: string): string[] {
  const tag = normalizeMatchTag(raw);
  if (!tag || tags.includes(tag) || tags.length >= MAX_MATCH_TAGS) return tags;
  return [...tags, tag].sort();
}

/**
 * Suggestions for the tag input: tags in use (most used first), or the
 * starters before anything is tagged, minus the match's own tags, filtered
 * by what has been typed.
 */
export function matchTagSuggestions(usage: string[], current: string[], query: string, limit = 8): string[] {
  const pool = usage.length > 0 ? usage : STARTER_MATCH_TAGS;
  const needle = normalizeMatchTag(query);
  const available = pool.filter((tag) => !current.includes(tag));
  if (!needle) return available.slice(0, limit);
  const starts = available.filter((tag) => tag.startsWith(needle));
  const contains = available.filter((tag) => !tag.startsWith(needle) && tag.includes(needle));
  return [...starts, ...contains].slice(0, limit);
}
