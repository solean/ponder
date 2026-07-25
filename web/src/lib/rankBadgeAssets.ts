import constructedBronze from "../assets/ranks/constructed-bronze.png";
import constructedDiamond from "../assets/ranks/constructed-diamond.png";
import constructedGold from "../assets/ranks/constructed-gold.png";
import constructedMythic from "../assets/ranks/constructed-mythic.png";
import constructedPlatinum from "../assets/ranks/constructed-platinum.png";
import constructedSilver from "../assets/ranks/constructed-silver.png";
import limitedBronze from "../assets/ranks/limited-bronze.png";
import limitedDiamond from "../assets/ranks/limited-diamond.png";
import limitedGold from "../assets/ranks/limited-gold.png";
import limitedMythic from "../assets/ranks/limited-mythic.png";
import limitedPlatinum from "../assets/ranks/limited-platinum.png";
import limitedSilver from "../assets/ranks/limited-silver.png";
import type { Ladder } from "./rankProgress";

const RANK_BADGES: Record<Ladder, Record<string, string>> = {
  constructed: {
    Bronze: constructedBronze,
    Silver: constructedSilver,
    Gold: constructedGold,
    Platinum: constructedPlatinum,
    Diamond: constructedDiamond,
    Mythic: constructedMythic,
  },
  limited: {
    Bronze: limitedBronze,
    Silver: limitedSilver,
    Gold: limitedGold,
    Platinum: limitedPlatinum,
    Diamond: limitedDiamond,
    Mythic: limitedMythic,
  },
};

export function rankBadgeUrl(ladder: Ladder, rankClass: string): string | null {
  return RANK_BADGES[ladder][rankClass] ?? null;
}
