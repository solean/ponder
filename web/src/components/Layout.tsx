import { useEffect, useMemo, useState } from "react";
import { NavLink, Outlet, useLocation, useNavigationType } from "react-router-dom";

import { ThemeContext, type Theme, type ThemePreference } from "../lib/theme";
import { AppErrorFallback, ErrorBoundary } from "./ErrorBoundary";
import { LiveMatchBanner } from "./LiveMatchBanner";
import { Plasma } from "./Plasma";

const tabs = [
  { to: "/", label: "Overview" },
  { to: "/matches", label: "Matches" },
  { to: "/decks", label: "Decks" },
  { to: "/drafts", label: "Drafts" },
  { to: "/settings", label: "Settings" },
];

const THEME_STORAGE_KEY = "mtgdata.theme";
// Temporary kill switch while investigating frontend jank from the fixed WebGL background.
const ENABLE_BACKGROUND_ANIMATION = false;
const scrollPositions = new Map<string, number>();

function pageTitle(pathname: string): string {
  if (pathname === "/") return "Overview";
  if (pathname === "/matches") return "Matches";
  if (pathname.startsWith("/matches/")) return "Match Detail";
  if (pathname === "/decks") return "Decks";
  if (pathname.startsWith("/decks/")) return "Deck Detail";
  if (pathname === "/drafts") return "Drafts";
  if (pathname.startsWith("/drafts/")) return "Draft Detail";
  if (pathname === "/settings") return "Settings";
  return "MTGData Control Room";
}

function applyThemeColorMeta(theme: Theme) {
  const head = document.head;
  if (!head) return;

  let metaThemeColor = document.querySelector('meta[name="theme-color"]');
  if (!metaThemeColor) {
    metaThemeColor = document.createElement("meta");
    metaThemeColor.setAttribute("name", "theme-color");
    head.appendChild(metaThemeColor);
  }

  metaThemeColor.setAttribute("content", theme === "dark" ? "#050302" : "#f4ece1");
}

function readStoredPreference(): ThemePreference {
  if (typeof window === "undefined") return "dark";
  try {
    const stored = window.localStorage.getItem(THEME_STORAGE_KEY);
    return stored === "light" || stored === "system" ? stored : "dark";
  } catch {
    return "dark";
  }
}

function readSystemTheme(): Theme {
  if (typeof window === "undefined" || !window.matchMedia) return "dark";
  return window.matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark";
}

export function Layout() {
  const location = useLocation();
  const navigationType = useNavigationType();
  const [preference, setPreference] = useState<ThemePreference>(readStoredPreference);
  const [systemTheme, setSystemTheme] = useState<Theme>(readSystemTheme);
  const theme: Theme = preference === "system" ? systemTheme : preference;
  const themeContextValue = useMemo(() => ({ theme, preference, setPreference }), [theme, preference]);

  useEffect(() => {
    if (!window.matchMedia) return;
    const query = window.matchMedia("(prefers-color-scheme: light)");
    const onChange = (event: MediaQueryListEvent) => setSystemTheme(event.matches ? "light" : "dark");
    query.addEventListener("change", onChange);
    return () => query.removeEventListener("change", onChange);
  }, []);

  useEffect(() => {
    document.documentElement.dataset.theme = theme;
    applyThemeColorMeta(theme);
    try {
      window.localStorage.setItem(THEME_STORAGE_KEY, preference);
    } catch {
      // Ignore storage failures and keep the in-memory theme.
    }
  }, [theme, preference]);

  useEffect(() => {
    const context = pageTitle(location.pathname);
    document.title = context === "MTGData Control Room" ? context : `${context} · MTGData Control Room`;
  }, [location.pathname]);

  useEffect(() => {
    return () => {
      scrollPositions.set(location.key, window.scrollY);
    };
  }, [location.key]);

  useEffect(() => {
    if (navigationType === "POP") {
      const top = scrollPositions.get(location.key) ?? 0;
      window.scrollTo({ top, left: 0, behavior: "auto" });
      return;
    }
    window.scrollTo({ top: 0, left: 0, behavior: "auto" });
  }, [location.key, navigationType]);

  return (
    <ThemeContext.Provider value={themeContextValue}>
      <>
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
        <div className="app-shell">
          <header className="topbar">
          <div className="brand">
            <span className="title-sigil" aria-hidden="true" />
            <h1>MTGA Data Tracker</h1>
          </div>
          <div className="topbar-controls">
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
          </div>
        </header>
          {location.pathname === "/settings" ? null : (
            <ErrorBoundary label="LiveMatchBanner">
              <LiveMatchBanner />
            </ErrorBoundary>
          )}
          <main id="main-content" className="content" tabIndex={-1}>
            <ErrorBoundary
              key={location.pathname}
              label="page"
              fallback={(error, reset) => <AppErrorFallback error={error} onRetry={reset} scope="page" />}
            >
              <Outlet />
            </ErrorBoundary>
          </main>
        </div>
      </>
    </ThemeContext.Provider>
  );
}
