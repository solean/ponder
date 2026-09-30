import type { ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";

import { api } from "../lib/api";

export function StartupGate({ children }: { children: ReactNode }) {
  const startup = useQuery({
    queryKey: ["startup"],
    queryFn: ({ signal }) => api.waitForStartup(signal),
    staleTime: Infinity,
    retry: false,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  });

  if (startup.data) return children;

  const error = !startup.isFetching && startup.error;
  return (
    <main className="startup-screen">
      <div className="startup-card">
        {error ? (
          <>
            <div role="alert">
              <h1>Ponder couldn’t start</h1>
              <p>Try again. If this continues, restart the app.</p>
              <details className="startup-details">
                <summary>Show details</summary>
                <pre className="app-error-message">{error.message}</pre>
              </details>
            </div>
            <button type="button" className="app-error-button is-primary" onClick={() => void startup.refetch()}>
              Try again
            </button>
          </>
        ) : (
          <div role="status" aria-live="polite">
            <span className="startup-spinner" aria-hidden="true" />
            <h1>Starting Ponder…</h1>
            <p>Getting things ready.</p>
          </div>
        )}
      </div>
    </main>
  );
}
