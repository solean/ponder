import { Component, type ErrorInfo, type ReactNode } from "react";

type FallbackRender = (error: Error, reset: () => void) => ReactNode;

type Props = {
  children: ReactNode;
  /**
   * Rendered instead of crashing when a child throws. May be a node or a render
   * function receiving the error and a `reset` to retry. Defaults to nothing
   * (silent) — use a visible fallback for app/page scopes.
   */
  fallback?: ReactNode | FallbackRender;
  /** Optional label to identify the boundary in console errors. */
  label?: string;
};

type State = { error: Error | null };

/**
 * Catches render errors in a subtree so one broken component can't blank the
 * whole app. Wraps the global live banner (silent) and the page/app shells
 * (visible fallback) so frontend crashes surface a message instead of a white
 * screen.
 */
export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error(`ErrorBoundary${this.props.label ? ` (${this.props.label})` : ""} caught:`, error, info);
  }

  reset = () => this.setState({ error: null });

  render() {
    const { error } = this.state;
    if (error) {
      const { fallback } = this.props;
      if (typeof fallback === "function") {
        return (fallback as FallbackRender)(error, this.reset);
      }
      return fallback ?? null;
    }
    return this.props.children;
  }
}

/**
 * Visible fallback shown when a page or the whole app crashes. Surfaces the
 * error message and offers retry (re-render the subtree) and reload (full
 * refresh) so the user is never stuck on a blank screen.
 */
export function AppErrorFallback({
  error,
  onRetry,
  scope = "app",
}: {
  error: Error;
  onRetry: () => void;
  scope?: string;
}) {
  return (
    <div className="app-error" role="alert">
      <div className="app-error-card">
        <p className="app-error-kicker">Something went wrong</p>
        <h2>{scope === "page" ? "This page hit an error" : "The app hit an error"}</h2>
        <pre className="app-error-message">{error.message || "Unknown error"}</pre>
        <div className="app-error-actions">
          <button type="button" className="app-error-button is-primary" onClick={onRetry}>
            Try again
          </button>
          <button type="button" className="app-error-button" onClick={() => window.location.reload()}>
            Reload app
          </button>
        </div>
        <p className="app-error-hint">If this keeps happening, check the developer console for details.</p>
      </div>
    </div>
  );
}
