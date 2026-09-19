import React from "react";
import ReactDOM from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { App } from "./App";
import { AppErrorFallback, ErrorBoundary } from "./components/ErrorBoundary";

// Self-hosted fonts (bundled by Vite; no network dependency at runtime).
// Inter: body/reading. IBM Plex Mono: data, labels, numerals. Space Grotesk: display headings.
import "@fontsource/inter/400.css";
import "@fontsource/inter/500.css";
import "@fontsource/inter/600.css";
import "@fontsource/inter/700.css";
import "@fontsource/ibm-plex-mono/400.css";
import "@fontsource/ibm-plex-mono/500.css";
import "@fontsource/ibm-plex-mono/600.css";
import "@fontsource/space-grotesk/600.css";
import "@fontsource/space-grotesk/700.css";

import "./styles.css";

// Apply transparency before React's first overlay paint to avoid an opaque
// flash while the dedicated Wails window loads.
document.documentElement.classList.toggle("overlay-document", window.location.pathname === "/overlay");

// Wails' host-side ExecJS silently queues every script until the page reports
// "wails:runtime:ready", and only the framework runtime sends that. Without it
// the overlay's native cursor feed and its refetch nudge never arrive, so hover
// previews never open. The desktop asset server owns this path; under
// `bun run dev` in a plain browser it 404s, which costs the browser nothing.
const wailsRuntime = document.createElement("script");
wailsRuntime.type = "module";
wailsRuntime.src = "/wails/runtime.js";
document.head.append(wailsRuntime);

const queryClient = new QueryClient();

ReactDOM.createRoot(document.getElementById("root") as HTMLElement).render(
  <React.StrictMode>
    <ErrorBoundary label="app" fallback={(error, reset) => <AppErrorFallback error={error} onRetry={reset} />}>
      <QueryClientProvider client={queryClient}>
        <BrowserRouter>
          <App />
        </BrowserRouter>
      </QueryClientProvider>
    </ErrorBoundary>
  </React.StrictMode>,
);
