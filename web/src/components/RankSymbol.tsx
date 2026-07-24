import constructedRankSprite from "../assets/mtga-ranks-constructed-48.png";
import limitedRankSprite from "../assets/mtga-ranks-limited-48.png";
import { rankSymbolIndex } from "../lib/rankSymbol";
import type { Ladder } from "../lib/rankProgress";
import type { RankState } from "../lib/types";

const SPRITE_BY_LADDER: Record<Ladder, string> = {
  constructed: constructedRankSprite,
  limited: limitedRankSprite,
};

export function RankSymbol({ ladder, rank }: { ladder: Ladder; rank: RankState | null }) {
  const index = rankSymbolIndex(rank);

  return (
    <span
      aria-hidden="true"
      className="rank-symbol"
      style={{
        backgroundImage: `url("${SPRITE_BY_LADDER[ladder]}")`,
        backgroundPosition: `${index * -48}px 0`,
      }}
    />
  );
}
