import type { LiveMana } from "../lib/types";
import { ManaSymbol } from "./ManaSymbol";

const COLORS = ["W", "U", "B", "R", "G", "C"] as const;
const COLOR_NAMES = { W: "White", U: "Blue", B: "Black", R: "Red", G: "Green", C: "Colorless" };

export function OverlayManaPanel({ mana }: { mana?: LiveMana }) {
  if (mana?.landsKnown && mana.lands === 0) return null;

  return (
    <aside className="overlay-panel overlay-panel-mana" aria-label="Your mana">
      <div className="overlay-mana-totals">
        <div><strong>{mana?.landsKnown ? mana.lands : "—"}</strong><span>Lands</span></div>
        <div><strong>{mana?.available ?? "—"}</strong><span>Available · est.</span></div>
        <div><strong>{mana?.poolKnown ? mana.floating : "—"}</strong><span>In pool</span></div>
      </div>
      <div className="overlay-mana-colors" aria-label="Available mana by color">
        {COLORS.map((color) => (
          <span key={color} aria-label={`${COLOR_NAMES[color]}: ${mana ? mana.byColor[color] ?? 0 : "unknown"}`}>
            <ManaSymbol token={color} /><strong>{mana ? mana.byColor[color] ?? 0 : "—"}</strong>
          </span>
        ))}
      </div>
      <p className="overlay-mana-note">
        {!mana ? "Waiting for game state" : mana.unresolved > 0
          ? `${mana.unresolved} conditional or unknown · colors overlap`
          : !mana.poolKnown ? "Pool unknown · colors overlap" : "Includes pool · colors overlap"}
      </p>
    </aside>
  );
}
