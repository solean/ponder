import type { ColorScheme, ThemeMode } from "./theme";

/** Per-theme echarts colors, shared so every chart reads as one system. */
export type ChartThemeTokens = {
  accent: string;
  accentFaint: string;
  accentGlow: string;
  accentSoft: string;
  axisLine: string;
  /** Second series hue, chosen to separate from `accent` under CVD. */
  contrast: string;
  axisText: string;
  hoverBorder: string;
  pointBorder: string;
  promotion: string;
  promotionGlow: string;
  splitLine: string;
  tooltipBackground: string;
  tooltipBorder: string;
  tooltipText: string;
};

export const CHART_THEME_TOKENS: Record<`${ColorScheme}-${ThemeMode}`, ChartThemeTokens> = {
  "ember-dark": {
    accent: "#ff8a24",
    accentFaint: "rgba(255, 138, 36, 0.02)",
    accentGlow: "rgba(255, 138, 36, 0.24)",
    accentSoft: "rgba(255, 138, 36, 0.12)",
    axisLine: "rgba(255, 145, 64, 0.16)",
    contrast: "#38bdf8",
    axisText: "#c5a086",
    hoverBorder: "#f2e6db",
    pointBorder: "rgba(24, 16, 10, 0.94)",
    promotion: "#38bdf8",
    promotionGlow: "rgba(56, 189, 248, 0.42)",
    splitLine: "rgba(255, 138, 36, 0.03)",
    tooltipBackground: "rgba(12, 8, 6, 0.97)",
    tooltipBorder: "rgba(255, 168, 90, 0.3)",
    tooltipText: "#f2e6db",
  },
  "dimir-dark": {
    accent: "#37b7e0",
    accentFaint: "rgba(55, 183, 224, 0.02)",
    accentGlow: "rgba(55, 183, 224, 0.24)",
    accentSoft: "rgba(55, 183, 224, 0.12)",
    axisLine: "rgba(90, 185, 220, 0.16)",
    contrast: "#e6a23c",
    axisText: "#93b2c4",
    hoverBorder: "#e3edf4",
    pointBorder: "rgba(10, 18, 24, 0.94)",
    promotion: "#52c7f2",
    promotionGlow: "rgba(82, 199, 242, 0.42)",
    splitLine: "rgba(55, 183, 224, 0.03)",
    tooltipBackground: "rgba(5, 10, 15, 0.97)",
    tooltipBorder: "rgba(90, 190, 226, 0.3)",
    tooltipText: "#e3edf4",
  },
  "steel-dark": {
    accent: "#8ab2dd",
    accentFaint: "rgba(138, 178, 221, 0.02)",
    accentGlow: "rgba(138, 178, 221, 0.22)",
    accentSoft: "rgba(138, 178, 221, 0.12)",
    axisLine: "rgba(150, 172, 198, 0.16)",
    contrast: "#e0a84a",
    axisText: "#a9b4c1",
    hoverBorder: "#e6eaef",
    pointBorder: "rgba(13, 17, 22, 0.94)",
    promotion: "#54b9ec",
    promotionGlow: "rgba(84, 185, 236, 0.4)",
    splitLine: "rgba(138, 178, 221, 0.03)",
    tooltipBackground: "rgba(7, 10, 14, 0.97)",
    tooltipBorder: "rgba(150, 182, 216, 0.3)",
    tooltipText: "#e6eaef",
  },
  "bamboo-dark": {
    accent: "#8fb573",
    accentFaint: "rgba(143, 181, 115, 0.02)",
    accentGlow: "rgba(143, 181, 115, 0.24)",
    accentSoft: "rgba(143, 181, 115, 0.12)",
    axisLine: "rgba(143, 181, 115, 0.16)",
    contrast: "#57a5e5",
    axisText: "#b6bda8",
    hoverBorder: "#f1e9d2",
    pointBorder: "rgba(28, 30, 27, 0.94)",
    promotion: "#57a5e5",
    promotionGlow: "rgba(87, 165, 229, 0.42)",
    splitLine: "rgba(143, 181, 115, 0.03)",
    tooltipBackground: "rgba(28, 30, 27, 0.97)",
    tooltipBorder: "rgba(163, 197, 137, 0.3)",
    tooltipText: "#f1e9d2",
  },
  "ember-light": {
    accent: "#c55a11",
    accentFaint: "rgba(197, 90, 17, 0.02)",
    accentGlow: "rgba(197, 90, 17, 0.15)",
    accentSoft: "rgba(197, 90, 17, 0.1)",
    axisLine: "rgba(140, 62, 8, 0.14)",
    contrast: "#087eae",
    axisText: "#5c402d",
    hoverBorder: "#fffaf4",
    pointBorder: "rgba(247, 239, 230, 0.96)",
    promotion: "#087eae",
    promotionGlow: "rgba(8, 126, 174, 0.28)",
    splitLine: "rgba(197, 90, 17, 0.02)",
    tooltipBackground: "rgba(250, 243, 236, 0.98)",
    tooltipBorder: "rgba(140, 62, 8, 0.28)",
    tooltipText: "#24150b",
  },
  "dimir-light": {
    accent: "#0f7fb2",
    accentFaint: "rgba(15, 127, 178, 0.02)",
    accentGlow: "rgba(15, 127, 178, 0.15)",
    accentSoft: "rgba(15, 127, 178, 0.1)",
    axisLine: "rgba(30, 90, 125, 0.14)",
    contrast: "#b86e00",
    axisText: "#46647a",
    hoverBorder: "#fbfcfd",
    pointBorder: "rgba(247, 249, 250, 0.96)",
    promotion: "#087fae",
    promotionGlow: "rgba(8, 127, 174, 0.28)",
    splitLine: "rgba(15, 127, 178, 0.02)",
    tooltipBackground: "rgba(250, 252, 253, 0.98)",
    tooltipBorder: "rgba(30, 90, 125, 0.28)",
    tooltipText: "#101c26",
  },
  "steel-light": {
    accent: "#3f699b",
    accentFaint: "rgba(63, 105, 155, 0.02)",
    accentGlow: "rgba(63, 105, 155, 0.15)",
    accentSoft: "rgba(63, 105, 155, 0.1)",
    axisLine: "rgba(70, 90, 115, 0.14)",
    contrast: "#b0700f",
    axisText: "#55606e",
    hoverBorder: "#f7f9fb",
    pointBorder: "rgba(238, 241, 244, 0.96)",
    promotion: "#247dab",
    promotionGlow: "rgba(36, 125, 171, 0.26)",
    splitLine: "rgba(63, 105, 155, 0.02)",
    tooltipBackground: "rgba(242, 244, 247, 0.98)",
    tooltipBorder: "rgba(70, 90, 115, 0.28)",
    tooltipText: "#171b21",
  },
  "bamboo-light": {
    accent: "#27850b",
    accentFaint: "rgba(39, 133, 11, 0.02)",
    accentGlow: "rgba(39, 133, 11, 0.15)",
    accentSoft: "rgba(39, 133, 11, 0.1)",
    axisLine: "rgba(122, 124, 96, 0.28)",
    contrast: "#1f6fb8",
    axisText: "#4a5246",
    hoverBorder: "#fffff8",
    pointBorder: "rgba(250, 250, 240, 0.96)",
    promotion: "#188a9e",
    promotionGlow: "rgba(24, 138, 158, 0.28)",
    splitLine: "rgba(39, 133, 11, 0.02)",
    tooltipBackground: "rgba(252, 252, 244, 0.98)",
    tooltipBorder: "rgba(122, 124, 96, 0.4)",
    tooltipText: "#3a4238",
  },
};
