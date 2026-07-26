import type { DeckAnalyticsCoverage } from "./types";

export type DeckAnalyticsCoverageIssueKey =
  | "results"
  | "opening-hands"
  | "play-draw"
  | "card-data"
  | "turn-data"
  | "land-drops";

export type DeckAnalyticsCoverageIssue = {
  key: DeckAnalyticsCoverageIssueKey;
  label: string;
  shortLabel: string;
  available: number;
  total: number;
  missing: number;
  impact: string;
};

export type DeckAnalyticsCoveragePresentation = {
  state: "empty" | "complete" | "partial";
  statusLabel: string;
  issues: DeckAnalyticsCoverageIssue[];
  issueSummary: string;
  versionNote?: string;
};

type CoverageDefinition = {
  key: DeckAnalyticsCoverageIssueKey;
  field: keyof Pick<
    DeckAnalyticsCoverage,
    | "gamesWithResult"
    | "gamesWithOpeningHand"
    | "gamesWithPlayDraw"
    | "gamesWithCardStats"
    | "gamesWithTurnStats"
    | "gamesWithLandJudged"
  >;
  label: string;
  shortLabel: string;
  impact: string;
};

const COVERAGE_DEFINITIONS: CoverageDefinition[] = [
  {
    key: "results",
    field: "gamesWithResult",
    label: "Game results",
    shortLabel: "Results",
    impact: "Games without a result are excluded from records and win rates.",
  },
  {
    key: "opening-hands",
    field: "gamesWithOpeningHand",
    label: "Opening-hand analysis",
    shortLabel: "Opening hands",
    impact: "Affects kept-hand size and opening-card analysis.",
  },
  {
    key: "play-draw",
    field: "gamesWithPlayDraw",
    label: "Play/draw splits",
    shortLabel: "Play/draw",
    impact: "Affects on-the-play and on-the-draw comparisons.",
  },
  {
    key: "card-data",
    field: "gamesWithCardStats",
    label: "Card performance",
    shortLabel: "Card data",
    impact: "Affects card-seen, drawn, played, and stranded statistics.",
  },
  {
    key: "turn-data",
    field: "gamesWithTurnStats",
    label: "Turn analysis",
    shortLabel: "Turn data",
    impact: "Affects turn curves and game-shape analysis.",
  },
  {
    key: "land-drops",
    field: "gamesWithLandJudged",
    label: "Land-drop analysis",
    shortLabel: "Land drops",
    impact: "Affects missed-land-drop and clean-land-drop comparisons.",
  },
];

function pluralized(count: number, singular: string, plural = `${singular}s`): string {
  return count === 1 ? singular : plural;
}

function versionCoverageNote(coverage: DeckAnalyticsCoverage, versionId: number): string | undefined {
  if (versionId > 0 || coverage.matchesWithVersion >= coverage.matches) {
    return undefined;
  }
  const unversioned = Math.max(0, coverage.matches - coverage.matchesWithVersion);
  if (unversioned === 0) {
    return undefined;
  }
  return `${unversioned} ${pluralized(unversioned, "match", "matches")} ${
    unversioned === 1 ? "isn’t" : "aren’t"
  } linked to a deck version`;
}

export function coverageSampleLabel(available: number, total: number): string | undefined {
  if (total <= 0 || available >= total) {
    return undefined;
  }
  const safeAvailable = Math.max(0, Math.min(available, total));
  return `${safeAvailable} of ${total} ${pluralized(total, "game")}`;
}

export function buildDeckAnalyticsCoveragePresentation(
  coverage: DeckAnalyticsCoverage,
  versionId = 0,
): DeckAnalyticsCoveragePresentation {
  const total = Math.max(0, coverage.gameCount);
  const note = versionCoverageNote(coverage, versionId);
  if (total === 0) {
    return {
      state: "empty",
      statusLabel: "No games analyzed",
      issues: [],
      issueSummary: "",
      versionNote: note,
    };
  }

  const issues = COVERAGE_DEFINITIONS.flatMap<DeckAnalyticsCoverageIssue>((definition) => {
    const available = Math.max(0, Math.min(coverage[definition.field], total));
    if (available === total) {
      return [];
    }
    return [
      {
        key: definition.key,
        label: definition.label,
        shortLabel: definition.shortLabel,
        available,
        total,
        missing: total - available,
        impact: definition.impact,
      },
    ];
  });

  const visibleIssues = issues.slice(0, 2);
  const issueSummary = visibleIssues
    .map((issue) => `${issue.shortLabel} ${issue.available}/${issue.total}`)
    .concat(issues.length > visibleIssues.length ? [`+${issues.length - visibleIssues.length} more`] : [])
    .join(" · ");

  return {
    state: issues.length === 0 ? "complete" : "partial",
    statusLabel: issues.length === 0 ? `All ${total} ${pluralized(total, "game")} analyzed` : "Some metrics use fewer games",
    issues,
    issueSummary,
    versionNote: note,
  };
}
