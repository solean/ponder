import {
  createContext,
  type Dispatch,
  type ReactNode,
  type SetStateAction,
  useContext,
  useEffect,
  useLayoutEffect,
  useMemo,
  useState,
} from "react";
import { Link, type LinkProps, useLocation } from "react-router-dom";

import { APP_NAME } from "../lib/branding";
import {
  breadcrumbNavigationStateForPath,
  breadcrumbParentsFromState,
  breadcrumbsForPath,
} from "../lib/breadcrumbs";

type ResolvedBreadcrumbLabel = {
  pathname: string;
  label: string;
};

type BreadcrumbContextValue = {
  resolvedLabel: ResolvedBreadcrumbLabel | null;
  setResolvedLabel: Dispatch<SetStateAction<ResolvedBreadcrumbLabel | null>>;
};

const BreadcrumbContext = createContext<BreadcrumbContextValue | null>(null);

export function BreadcrumbProvider({ children }: { children: ReactNode }) {
  const [resolvedLabel, setResolvedLabel] = useState<ResolvedBreadcrumbLabel | null>(null);
  const value = useMemo(
    () => ({ resolvedLabel, setResolvedLabel }),
    [resolvedLabel],
  );

  return <BreadcrumbContext.Provider value={value}>{children}</BreadcrumbContext.Provider>;
}

/** Lets a detail screen replace its ID-based fallback with loaded record data. */
export function useBreadcrumbLabel(label?: string | null) {
  const context = useContext(BreadcrumbContext);
  const setResolvedLabel = context?.setResolvedLabel;
  const { pathname } = useLocation();

  useLayoutEffect(() => {
    const normalizedLabel = label?.trim();
    if (!setResolvedLabel || !normalizedLabel) return;

    setResolvedLabel({ pathname, label: normalizedLabel });
    return () => {
      setResolvedLabel((current) =>
        current?.pathname === pathname ? null : current,
      );
    };
  }, [label, pathname, setResolvedLabel]);
}

/** Preserves the visible trail for a related link and collapses route cycles. */
export function useBreadcrumbNavigationState(destinationPath?: string) {
  const { pathname, state } = useLocation();
  const context = useContext(BreadcrumbContext);
  const resolvedLabel = context?.resolvedLabel?.pathname === pathname
    ? context.resolvedLabel.label
    : null;

  return useMemo(() => {
    const contextualParents = breadcrumbParentsFromState(state);
    return breadcrumbNavigationStateForPath(
      pathname,
      resolvedLabel,
      contextualParents,
      destinationPath,
    );
  }, [destinationPath, pathname, resolvedLabel, state]);
}

type ContextualLinkProps = Omit<LinkProps, "state" | "to"> & {
  to: string;
};

/** Link for a genuine parent/child relationship between app records. */
export function ContextualLink({ to, ...props }: ContextualLinkProps) {
  const state = useBreadcrumbNavigationState(to);
  return <Link {...props} to={to} state={state} />;
}

export function Breadcrumbs() {
  const { pathname, state } = useLocation();
  const context = useContext(BreadcrumbContext);
  const resolvedLabel = context?.resolvedLabel?.pathname === pathname
    ? context.resolvedLabel.label
    : null;
  const breadcrumbs = useMemo(() => {
    const contextualParents = breadcrumbParentsFromState(state);
    return breadcrumbsForPath(pathname, resolvedLabel, contextualParents);
  }, [pathname, resolvedLabel, state]);
  const currentPage = breadcrumbs[breadcrumbs.length - 1]?.label;

  useEffect(() => {
    document.title = currentPage ? `${currentPage} · ${APP_NAME}` : APP_NAME;
  }, [currentPage]);

  if (breadcrumbs.length <= 1) return null;

  return (
    <nav className="breadcrumbs" aria-label="Breadcrumb">
      <ol className="breadcrumb-list">
        {breadcrumbs.map((breadcrumb) => (
          <li className="breadcrumb-item" key={breadcrumb.to ?? breadcrumb.label}>
            {breadcrumb.to ? (
              <Link className="breadcrumb-link" to={breadcrumb.to}>
                {breadcrumb.label}
              </Link>
            ) : (
              <span className="breadcrumb-current" aria-current="page">
                {breadcrumb.label}
              </span>
            )}
          </li>
        ))}
      </ol>
    </nav>
  );
}
