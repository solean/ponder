import type { SeventeenLandsFormat, SeventeenLandsUserGroup } from "./types";

export const SEVENTEEN_LANDS_FORMAT_LABELS: Record<Exclude<SeventeenLandsFormat, "">, string> = {
  PremierDraft: "Premier Draft",
  TradDraft: "Traditional Draft",
  QuickDraft: "Quick Draft",
  PickTwoDraft: "Pick Two Draft",
};

export const SEVENTEEN_LANDS_USER_GROUP_LABELS: Record<SeventeenLandsUserGroup, string> = {
  "": "All players",
  top: "Top players",
  middle: "Middle players",
  bottom: "Bottom players",
};
