import type { ReactNode } from "react";
import { Link } from "react-router-dom";

import { ApiError } from "../lib/apiError";
import { isRetryableError } from "../lib/queryClient";
import type { QueryErrorView } from "../lib/useQueryError";
import { LoadingMark } from "./LoadingMessage";
import "./ErrorState.css";

/**
 * - page: the whole view failed. Boxed, with a large headline.
 * - panel: one section failed. Sits inside the section; the rest of the page stays usable.
 * - inline: a save failed next to its field. One line.
 */
export type ErrorStateSize = "page" | "panel" | "inline";

type ErrorStateProps = {
  /** What failed, as a sentence: "Couldn't load economy history." */
  title: ReactNode;
  /** Optional second line: why, or what to do about it. */
  detail?: ReactNode;
  size?: ErrorStateSize;
  /** Shows a "Try again" button (a "Retry" link when inline). */
  onRetry?: () => void;
  /** True while a retry is in flight. */
  retrying?: boolean;
  /** How many times this has failed. From 2 on, the block says it's still failing. */
  failureCount?: number;
  /** Further actions after "Try again", such as a link back to a list. */
  actions?: ReactNode;
  /** "missing" for something that doesn't exist, rather than something that failed. */
  icon?: "failed" | "missing";
};

export function ErrorState({
  title,
  detail,
  size = "panel",
  onRetry,
  retrying = false,
  failureCount = 0,
  actions,
  icon = "failed",
}: ErrorStateProps) {
  if (size === "inline") {
    return (
      <div className="error-state error-state--inline" role="alert" aria-atomic="true">
        <ErrorIcon kind="inline" />
        <span className="error-state__title">{title}</span>
        {detail ? <span className="error-state__detail">{detail}</span> : null}
        {onRetry ? (
          <button type="button" className="error-state__text-button" onClick={onRetry} disabled={retrying}>
            {retrying ? "Retrying…" : "Retry"}
          </button>
        ) : null}
        {actions}
      </div>
    );
  }

  const stillFailing = !retrying && failureCount > 1;
  return (
    // No aria-busy while retrying: background retries show this state too, and
    // a busy region can hide the Try again button from screen readers.
    <section className={`error-state error-state--${size}`} role="alert" aria-atomic="true">
      <div className="error-state__heading">
        <ErrorIcon kind={icon} />
        <p className="error-state__title">{title}</p>
      </div>
      {detail ? <p className="error-state__detail">{detail}</p> : null}
      {onRetry || actions ? (
        <div className="error-state__actions">
          {onRetry ? (
            <button
              type="button"
              className={`control-button${size === "page" ? " control-button--primary" : ""}`}
              onClick={onRetry}
              disabled={retrying}
            >
              {retrying ? (
                <>
                  <LoadingMark />
                  Trying again…
                </>
              ) : (
                <>
                  <RetryIcon />
                  Try again
                </>
              )}
            </button>
          ) : null}
          {actions}
        </div>
      ) : null}
      {stillFailing ? <p className="error-state__note">Still failing after {failureCount} tries.</p> : null}
    </section>
  );
}

export const RESTART_HINT = "If it keeps failing, restart Ponder.";

/**
 * ErrorState for a failed query, fed by useQueryError. The headline defaults
 * to the error's message: since IXN-90 the API sends one written for users,
 * such as "Couldn't load matches.". A retry is offered only when it could
 * help: never for a 4xx.
 */
export function QueryErrorState({
  view,
  title,
  detail,
  size = "panel",
  actions,
  icon,
}: {
  view: QueryErrorView;
  title?: ReactNode;
  detail?: ReactNode;
  size?: ErrorStateSize;
  actions?: ReactNode;
  icon?: "failed" | "missing";
}) {
  const { error } = view;
  const retryable = isRetryableError(error);
  return (
    <ErrorState
      size={size}
      title={title ?? (error?.message || "Something went wrong.")}
      detail={detail ?? (retryable && size !== "inline" ? RESTART_HINT : undefined)}
      onRetry={retryable ? () => void view.retry() : undefined}
      retrying={view.retrying}
      failureCount={view.tries}
      actions={actions}
      icon={icon ?? (isNotFound(error) ? "missing" : "failed")}
    />
  );
}

/** True when the API answered 404: the record doesn't exist. */
export function isNotFound(error: unknown): boolean {
  return error instanceof ApiError && error.status === 404;
}

/** A link back to a list, styled to sit in an ErrorState's actions. */
export function BackLink({ to, children, primary = false }: { to: string; children: ReactNode; primary?: boolean }) {
  return (
    <Link className={`control-button${primary ? " control-button--primary" : " control-button--quiet"}`} to={to}>
      {children}
    </Link>
  );
}

/**
 * Page error for a record that doesn't exist, such as a deck ID that isn't in
 * the database or isn't a number at all. Offers a way back instead of a retry.
 */
export function NotFoundState({
  title,
  detail = "The link may be out of date.",
  backTo,
  backLabel,
}: {
  title: ReactNode;
  detail?: ReactNode;
  backTo: string;
  backLabel: ReactNode;
}) {
  return (
    <ErrorState
      size="page"
      icon="missing"
      title={title}
      detail={detail}
      actions={
        <BackLink to={backTo} primary>
          {backLabel}
        </BackLink>
      }
    />
  );
}

function ErrorIcon({ kind }: { kind: "failed" | "missing" | "inline" }) {
  const common = {
    className: "error-state__icon",
    viewBox: "0 0 24 24",
    fill: "none",
    stroke: "currentColor",
    strokeLinecap: "round" as const,
    strokeLinejoin: "round" as const,
    "aria-hidden": true,
  };
  if (kind === "missing") {
    return (
      <svg {...common} strokeWidth={1.6}>
        <circle cx="11" cy="11" r="7" />
        <path d="M21 21l-4.35-4.35" />
        <path d="M8.5 8.5l5 5M13.5 8.5l-5 5" />
      </svg>
    );
  }
  if (kind === "inline") {
    return (
      <svg {...common} strokeWidth={2}>
        <circle cx="12" cy="12" r="9" />
        <path d="M12 7.5v5.5" />
        <path d="M12 16.5v.01" />
      </svg>
    );
  }
  return (
    <svg {...common} strokeWidth={1.6}>
      <path d="M12 3.5l9.5 16.5h-19z" />
      <path d="M12 10v4.5" />
      <path d="M12 17.5v.01" />
    </svg>
  );
}

function RetryIcon() {
  return (
    <svg
      className="error-state__button-icon"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={2.2}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M20 11a8 8 0 1 0-2.3 5.7" />
      <path d="M20 5v6h-6" />
    </svg>
  );
}
