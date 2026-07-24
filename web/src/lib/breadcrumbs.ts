export type BreadcrumbItem = {
  label: string;
  to?: string;
};

export type BreadcrumbParent = BreadcrumbItem & {
  to: string;
};

export type BreadcrumbNavigationState = {
  breadcrumbParents: BreadcrumbParent[];
};

const sectionLabels = {
  matches: "Matches",
  decks: "Decks",
  drafts: "Drafts",
  ranked: "Ranked",
  economy: "Economy",
  settings: "Settings",
} as const;

type Section = keyof typeof sectionLabels;

const detailPrefixes: Partial<Record<Section, string>> = {
  matches: "Match",
  decks: "Deck",
  drafts: "Draft",
};

function normalizePathname(pathname: string): string {
  if (pathname === "/") return pathname;
  return pathname.replace(/\/+$/, "") || "/";
}

function decodeSegment(segment: string): string {
  try {
    return decodeURIComponent(segment);
  } catch {
    return segment;
  }
}

function isSection(value: string | undefined): value is Section {
  if (!value) return false;
  return Object.prototype.hasOwnProperty.call(sectionLabels, value);
}

export function breadcrumbParentsFromState(state: unknown): BreadcrumbParent[] {
  if (!state || typeof state !== "object") return [];

  const parents = (state as Partial<BreadcrumbNavigationState>).breadcrumbParents;
  if (!Array.isArray(parents)) return [];

  return parents.flatMap((parent) => {
    if (
      !parent ||
      typeof parent.label !== "string" ||
      typeof parent.to !== "string" ||
      !parent.label.trim() ||
      !parent.to.startsWith("/")
    ) {
      return [];
    }
    return [{ label: parent.label.trim(), to: parent.to }];
  });
}

export function breadcrumbNavigationStateForPath(
  pathname: string,
  resolvedDetailLabel?: string | null,
  contextualParents: BreadcrumbParent[] = [],
  destinationPath?: string,
): BreadcrumbNavigationState | undefined {
  const breadcrumbs = breadcrumbsForPath(pathname, resolvedDetailLabel, contextualParents);
  if (breadcrumbs.length <= 1) return undefined;

  let parents = breadcrumbs.slice(1).map((breadcrumb) => ({
    label: breadcrumb.label,
    to: breadcrumb.to ?? pathname,
  }));

  if (destinationPath) {
    const destination = normalizePathname(destinationPath.split(/[?#]/, 1)[0] || "/");
    const repeatedDestinationIndex = parents.findIndex(
      (parent) => normalizePathname(parent.to.split(/[?#]/, 1)[0] || "/") === destination,
    );
    if (repeatedDestinationIndex >= 0) {
      parents = parents.slice(0, repeatedDestinationIndex);
    }
  }

  if (parents.length === 0) return undefined;

  return {
    breadcrumbParents: parents,
  };
}

/** Builds the canonical breadcrumb trail for every navigable app route. */
export function breadcrumbsForPath(
  pathname: string,
  resolvedDetailLabel?: string | null,
  contextualParents: BreadcrumbParent[] = [],
): BreadcrumbItem[] {
  const normalizedPathname = normalizePathname(pathname);
  const breadcrumbs: BreadcrumbItem[] = [{ label: "Overview" }];

  if (normalizedPathname === "/") return breadcrumbs;

  const segments = normalizedPathname.split("/").filter(Boolean);
  const section = segments[0];
  if (!isSection(section)) return [];

  breadcrumbs[0].to = "/";

  const sectionLabel = sectionLabels[section];
  if (segments.length === 1) {
    breadcrumbs.push({ label: sectionLabel });
    return breadcrumbs;
  }

  const detailPrefix = detailPrefixes[section];
  if (!detailPrefix || segments.length !== 2) return [];

  const detailID = decodeSegment(segments[1]);
  const detailLabel = resolvedDetailLabel?.trim() || `${detailPrefix} #${detailID}`;
  if (contextualParents.length > 0) {
    return [
      { label: "Overview", to: "/" },
      ...contextualParents,
      { label: detailLabel },
    ];
  }

  breadcrumbs.push({ label: sectionLabel, to: `/${section}` });
  breadcrumbs.push({ label: detailLabel });
  return breadcrumbs;
}
