import { createContext, useContext } from "react";

/** Light/dark appearance, orthogonal to the color scheme. */
export type ThemeMode = "dark" | "light";
export type ModePreference = ThemeMode | "system";

/** Palette identity applied on top of the mode. */
export type ColorScheme = "ember" | "dimir" | "steel" | "bamboo";

export const COLOR_SCHEMES: readonly ColorScheme[] = ["ember", "dimir", "steel", "bamboo"];

export type ThemeContextValue = {
  /** Resolved appearance actually applied to the document. */
  mode: ThemeMode;
  /** Stored appearance preference; "system" follows prefers-color-scheme. */
  modePreference: ModePreference;
  setModePreference: (preference: ModePreference) => void;
  scheme: ColorScheme;
  setScheme: (scheme: ColorScheme) => void;
};

export const ThemeContext = createContext<ThemeContextValue>({
  mode: "dark",
  modePreference: "dark",
  setModePreference: () => {},
  scheme: "bamboo",
  setScheme: () => {},
});

export function useTheme(): { mode: ThemeMode; scheme: ColorScheme } {
  const { mode, scheme } = useContext(ThemeContext);
  return { mode, scheme };
}

export function useThemeControls(): ThemeContextValue {
  return useContext(ThemeContext);
}

export const MODE_STORAGE_KEY = "ponder.theme";
export const SCHEME_STORAGE_KEY = "ponder.scheme";

/** Legacy single-key values "dimir"/"steel" migrate to mode=dark + that scheme. */
export function readStoredModePreference(): ModePreference {
  if (typeof window === "undefined") return "dark";
  try {
    const stored = window.localStorage.getItem(MODE_STORAGE_KEY);
    if (stored === "light" || stored === "system") return stored;
    return "dark";
  } catch {
    return "dark";
  }
}

export function readStoredScheme(): ColorScheme {
  if (typeof window === "undefined") return "bamboo";
  try {
    const stored = window.localStorage.getItem(SCHEME_STORAGE_KEY);
    if ((COLOR_SCHEMES as readonly string[]).includes(stored ?? "")) {
      return stored as ColorScheme;
    }
    const legacy = window.localStorage.getItem(MODE_STORAGE_KEY);
    if (legacy === "dimir" || legacy === "steel") return legacy;
    return "bamboo";
  } catch {
    return "bamboo";
  }
}

export function readSystemTheme(): ThemeMode {
  if (typeof window === "undefined" || !window.matchMedia) return "dark";
  return window.matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark";
}

const THEME_COLORS: Record<ColorScheme, Record<ThemeMode, string>> = {
  ember: { dark: "#020101", light: "#f4ece1" },
  dimir: { dark: "#020507", light: "#f7f9fa" },
  steel: { dark: "#040608", light: "#eef0f3" },
  bamboo: { dark: "#1c1e1b", light: "#fafaf0" },
};

export function applyTheme(mode: ThemeMode, scheme: ColorScheme) {
  document.documentElement.dataset.theme = mode;
  document.documentElement.dataset.scheme = scheme;

  let metaThemeColor = document.querySelector('meta[name="theme-color"]');
  if (!metaThemeColor) {
    metaThemeColor = document.createElement("meta");
    metaThemeColor.setAttribute("name", "theme-color");
    document.head.appendChild(metaThemeColor);
  }
  metaThemeColor.setAttribute("content", THEME_COLORS[scheme][mode]);
}

/**
 * Applies the stored theme before React renders, so screens shown ahead of
 * Layout (the startup loader) already use the configured palette.
 */
export function applyStoredTheme() {
  const preference = readStoredModePreference();
  applyTheme(preference === "system" ? readSystemTheme() : preference, readStoredScheme());
}
