const RESULT_LETTERS: Record<string, string> = { win: "W", loss: "L" };

export function ResultPill({ result }: { result: string }) {
  const normalized = result.toLowerCase();
  const letter = RESULT_LETTERS[normalized];
  const className = letter ? `pill ${normalized}` : "pill";
  const label = result || "unknown";
  return (
    <span className={className} title={label} aria-label={label}>
      {letter ?? "?"}
    </span>
  );
}
