import { useSetMatchSaved } from "../lib/useSetMatchSaved";

const STAR_PATH =
  "M12 3.2l2.62 5.53 6.03.73-4.45 4.16 1.16 5.97L12 16.65l-5.36 2.94 1.16-5.97-4.45-4.16 6.03-.73z";

export function StarIcon({ filled }: { filled: boolean }) {
  return (
    <svg className="save-match-star" viewBox="0 0 24 24" aria-hidden="true">
      <path
        d={STAR_PATH}
        fill={filled ? "currentColor" : "none"}
        stroke="currentColor"
        strokeWidth={1.6}
        strokeLinejoin="round"
      />
    </svg>
  );
}

/** Star toggle that saves or unsaves one match. */
export function SaveMatchButton({
  matchId,
  saved,
  className = "",
}: {
  matchId: number;
  saved: boolean;
  className?: string;
}) {
  const mutation = useSetMatchSaved();
  const label = saved ? "Unsave match" : "Save match";

  return (
    <button
      type="button"
      className={`save-match-button${saved ? " is-saved" : ""}${className ? ` ${className}` : ""}`}
      aria-pressed={saved}
      aria-label={label}
      title={label}
      onClick={() => mutation.mutate({ matchId, saved: !saved })}
    >
      <StarIcon filled={saved} />
    </button>
  );
}
