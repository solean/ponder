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
