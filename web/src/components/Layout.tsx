import { useEffect, useMemo, useState } from "react";
import { NavLink, Outlet, useLocation, useNavigationType } from "react-router-dom";

import {
  COLOR_SCHEMES,
  ThemeContext,
  type ColorScheme,
  type ModePreference,
  type ThemeMode,
} from "../lib/theme";
import { APP_NAME } from "../lib/branding";
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

const MODE_STORAGE_KEY = "mtgdata.theme";
const SCHEME_STORAGE_KEY = "mtgdata.scheme";
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
  return APP_NAME;
}

function applyThemeColorMeta(mode: ThemeMode, scheme: ColorScheme) {
  const head = document.head;
  if (!head) return;

  let metaThemeColor = document.querySelector('meta[name="theme-color"]');
  if (!metaThemeColor) {
    metaThemeColor = document.createElement("meta");
    metaThemeColor.setAttribute("name", "theme-color");
    head.appendChild(metaThemeColor);
  }

  const themeColors: Record<ColorScheme, Record<ThemeMode, string>> = {
    ember: { dark: "#050302", light: "#f4ece1" },
    dimir: { dark: "#020507", light: "#f7f9fa" },
    steel: { dark: "#040608", light: "#eef0f3" },
  };
  metaThemeColor.setAttribute("content", themeColors[scheme][mode]);
}

/** Legacy single-key values "dimir"/"steel" migrate to mode=dark + that scheme. */
function readStoredModePreference(): ModePreference {
  if (typeof window === "undefined") return "dark";
  try {
    const stored = window.localStorage.getItem(MODE_STORAGE_KEY);
    if (stored === "light" || stored === "system") return stored;
    return "dark";
  } catch {
    return "dark";
  }
}

function readStoredScheme(): ColorScheme {
  if (typeof window === "undefined") return "ember";
  try {
    const stored = window.localStorage.getItem(SCHEME_STORAGE_KEY);
    if ((COLOR_SCHEMES as readonly string[]).includes(stored ?? "")) {
      return stored as ColorScheme;
    }
    const legacy = window.localStorage.getItem(MODE_STORAGE_KEY);
    if (legacy === "dimir" || legacy === "steel") return legacy;
    return "ember";
  } catch {
    return "ember";
  }
}

function readSystemTheme(): ThemeMode {
  if (typeof window === "undefined" || !window.matchMedia) return "dark";
  return window.matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark";
}

export function Layout() {
  const location = useLocation();
  const navigationType = useNavigationType();
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
    document.documentElement.dataset.theme = mode;
    document.documentElement.dataset.scheme = scheme;
    applyThemeColorMeta(mode, scheme);
    try {
      window.localStorage.setItem(MODE_STORAGE_KEY, modePreference);
      window.localStorage.setItem(SCHEME_STORAGE_KEY, scheme);
    } catch {
      // Ignore storage failures and keep the in-memory theme.
    }
  }, [mode, modePreference, scheme]);

  useEffect(() => {
    const context = pageTitle(location.pathname);
    document.title = context === APP_NAME ? context : `${context} · ${APP_NAME}`;
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
            <h1>{APP_NAME}</h1>
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
