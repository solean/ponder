import { useEffect, useMemo, useRef, useState } from "react";
import { NavLink, Outlet, useLocation, useNavigationType } from "react-router-dom";

import {
  MODE_STORAGE_KEY,
  SCHEME_STORAGE_KEY,
  ThemeContext,
  applyTheme,
  readStoredModePreference,
  readStoredScheme,
  readSystemTheme,
  type ColorScheme,
  type ModePreference,
  type ThemeMode,
} from "../lib/theme";
import { APP_NAME } from "../lib/branding";
import { BreadcrumbProvider, Breadcrumbs } from "./Breadcrumbs";
import { AppErrorFallback, ErrorBoundary } from "./ErrorBoundary";
import { LiveMatchBanner } from "./LiveMatchBanner";
import { Plasma } from "./Plasma";

const tabs = [
  { to: "/", label: "Overview" },
  { to: "/matches", label: "Matches" },
  { to: "/decks", label: "Decks" },
  { to: "/drafts", label: "Drafts" },
  { to: "/economy", label: "Economy" },
  { to: "/settings", label: "Settings" },
];

// Temporary kill switch while investigating frontend jank from the fixed WebGL background.
const ENABLE_BACKGROUND_ANIMATION = false;
const scrollPositions = new Map<string, number>();

export function Layout() {
  const location = useLocation();
  const navigationType = useNavigationType();
  // The content pane, not the document, scrolls so the titlebar and scrollbar
  // never overlap.
  const scrollRef = useRef<HTMLDivElement>(null);
  const [modePreference, setModePreference] = useState<ModePreference>(readStoredModePreference);
  const [scheme, setScheme] = useState<ColorScheme>(readStoredScheme);
  const [systemTheme, setSystemTheme] = useState<ThemeMode>(readSystemTheme);
  const mode: ThemeMode = modePreference === "system" ? systemTheme : modePreference;
  const themeContextValue = useMemo(
    () => ({ mode, modePreference, setModePreference, scheme, setScheme }),
    [mode, modePreference, scheme],
  );

  useEffect(() => {
    if (!window.matchMedia) return;
    const query = window.matchMedia("(prefers-color-scheme: light)");
    const onChange = (event: MediaQueryListEvent) => setSystemTheme(event.matches ? "light" : "dark");
    query.addEventListener("change", onChange);
    return () => query.removeEventListener("change", onChange);
  }, []);

  useEffect(() => {
    applyTheme(mode, scheme);
    try {
      window.localStorage.setItem(MODE_STORAGE_KEY, modePreference);
      window.localStorage.setItem(SCHEME_STORAGE_KEY, scheme);
    } catch {
      // Ignore storage failures and keep the in-memory theme.
    }
  }, [mode, modePreference, scheme]);

  useEffect(() => {
    const scroller = scrollRef.current;
    return () => {
      if (scroller) scrollPositions.set(location.key, scroller.scrollTop);
    };
  }, [location.key]);

  useEffect(() => {
    const top = navigationType === "POP" ? (scrollPositions.get(location.key) ?? 0) : 0;
    scrollRef.current?.scrollTo({ top, left: 0, behavior: "auto" });
  }, [location.key, navigationType]);

  return (
    <ThemeContext.Provider value={themeContextValue}>
      <BreadcrumbProvider>
        {ENABLE_BACKGROUND_ANIMATION ? (
          <div className="plasma-bg" aria-hidden="true">
            <Plasma
              color="#ff8a24"
              speed={0.35}
              direction="forward"
              scale={1.4}
              opacity={0.18}
              mouseInteractive={false}
            />
          </div>
        ) : null}
        <div className="app-frame">
          <header className="titlebar">
            <div className="brand">
              <span className="title-sigil" aria-hidden="true" />
              <h1>{APP_NAME}</h1>
            </div>
            <nav className="tabs" aria-label="Primary">
              {tabs.map((tab) => (
                <NavLink
                  key={tab.to}
                  to={tab.to}
                  end={tab.to === "/"}
                  className={({ isActive }) => `tab ${isActive ? "is-active" : ""}`}
                >
                  {tab.label}
                </NavLink>
              ))}
            </nav>
          </header>
          <div ref={scrollRef} className="app-scroll">
            <div className={`app-shell${location.pathname === "/matches" ? " is-viewport-page" : ""}`}>
              <main id="main-content" className="content">
                <div className="page-context-bar">
                  {location.pathname === "/" || location.pathname === "/settings" ? null : (
                    <ErrorBoundary label="LiveMatchBanner">
                      <LiveMatchBanner />
                    </ErrorBoundary>
                  )}
                  <Breadcrumbs />
                </div>
                <ErrorBoundary
                  key={location.pathname}
                  label="page"
                  fallback={(error, reset) => <AppErrorFallback error={error} onRetry={reset} scope="page" />}
                >
                  <Outlet />
                </ErrorBoundary>
              </main>
            </div>
          </div>
        </div>
      </BreadcrumbProvider>
    </ThemeContext.Provider>
  );
}
