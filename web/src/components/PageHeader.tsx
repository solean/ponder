import type { ReactNode } from "react";

/**
 * The one header row every page starts with: title on the left, section tabs
 * beside it, a short summary, then filters and actions at the right end.
 */
export function PageHeader({
  title,
  tabs,
  meta,
  actions,
  className,
}: {
  title: ReactNode;
  /** Section tabs for the page, shown right after the title. */
  tabs?: ReactNode;
  /** Short summary text such as a count or the active scope. */
  meta?: ReactNode;
  /** Filters and buttons, pushed to the right end of the row. */
  actions?: ReactNode;
  className?: string;
}) {
  return (
    <header className={`page-header${className ? ` ${className}` : ""}`}>
      <h2 className="page-header-title">{title}</h2>
      {tabs}
      {meta ? <p className="page-header-meta">{meta}</p> : null}
      {actions ? <div className="page-header-actions">{actions}</div> : null}
    </header>
  );
}
