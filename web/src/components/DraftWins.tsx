/** Wins that earn a trophy: 7 is the ceiling for both Premier and Quick Draft. */
export const TROPHY_WINS = 7;

/** A draft win count, badged with a trophy once the run maxes out. */
export function DraftWins({ wins }: { wins?: number | null }) {
  return (
    <span className="draft-wins">
      {wins ?? "-"}
      {wins != null && wins >= TROPHY_WINS ? (
        <svg className="draft-trophy" viewBox="0 0 16 16" role="img" aria-label={`${TROPHY_WINS}-win draft`}>
          <path d="M4.6 1.8h6.8v3.6a3.4 3.4 0 0 1-6.8 0V1.8Z" />
          <path
            d="M4.6 3.1H2.3v1.3a3 3 0 0 0 2.6 2.97M11.4 3.1h2.3v1.3a3 3 0 0 1-2.6 2.97"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.15"
            strokeLinecap="round"
          />
          <path d="M7.25 8.6h1.5v3.1h-1.5Z" />
          <path d="M5.15 11.7h5.7c.47 0 .85.38.85.85v1.65H4.3v-1.65c0-.47.38-.85.85-.85Z" />
        </svg>
      ) : null}
    </span>
  );
}
