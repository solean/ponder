import { CardStatLine, useDraftCardStats } from "./CardStats";
import { draftBadgePosition } from "../lib/draftBadgeLayout";
import type { OverlaySettings } from "../lib/overlaySettings";
import type { LiveDraft } from "../lib/types";
import "./DraftCardBadges.css";

/** Experimental positioning for Arena's default draft grid, calibrated in Settings. */
export function DraftCardBadges({ draft, settings }: { draft: LiveDraft; settings: OverlaySettings }) {
  const stats = useDraftCardStats(draft.eventName);
  // Never fall back to the log order: Arena displays a different order. Missing
  // metadata for even one card could shift every subsequent rating.
  if (draft.waiting || !draft.displayPack?.length || draft.displayPack.length !== draft.pack.length ||
      stats.query.data?.status !== "ok" || stats.query.isError) return null;

  return (
    <div className="draft-card-badges" role="group" aria-label="Experimental on-card 17Lands ratings">
      {draft.displayPack.map((card, index) => {
        const position = draftBadgePosition(index, settings);
        const stat = stats.byId.get(card.cardId);
        // Offscreen calibration must not produce a badge at an unrelated edge.
        if (position.left < 0 || position.left > 100 || position.top < 0 || position.top > 100) return null;
        return (
          <div key={`${draft.sessionId}:${draft.packNumber}:${draft.pickNumber}:${index}`} className="draft-card-badge"
            style={{ left: `${position.left}%`, top: `${position.top}%`, maxWidth: `${settings.draftBadgeWidth}%` }}
            aria-label={`${card.cardName || `Card ${card.cardId}`}: ${stat?.gihWinRate == null ? "no GIH win rate available" : `${(stat.gihWinRate * 100).toFixed(1)}% GIH win rate`}`}>
            <span className="draft-card-badge-label">GIH</span>
            {stat ? <CardStatLine cardId={card.cardId} stats={stats} compact /> : <strong>—</strong>}
          </div>
        );
      })}
    </div>
  );
}
