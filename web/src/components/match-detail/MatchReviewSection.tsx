import type { useMatchReplay } from "../../lib/replay/useMatchReplay";
import type { MatchReplayView } from "../../lib/replay/useMatchReplayView";
import { GameReviewPanel } from "../GameReviewPanel";
import { StatusMessage } from "../StatusMessage";
import type { MatchGameTabState } from "./MatchGameTabs";
import { MatchGameTabs } from "./MatchGameTabs";

export function MatchReviewSection({
  matchId,
  replayQuery,
  view,
  gameTabs,
}: {
  matchId: number;
  replayQuery: ReturnType<typeof useMatchReplay>;
  view: MatchReplayView;
  gameTabs: MatchGameTabState;
}) {
  const {
    timelineGameNumbers,
    activeTimelineGameNumber,
    showTimelineGameTabs,
    activeTimelineGameTabID,
    activeTimelineGamePanelID,
    timelineGameTabBaseId,
    setSelectedTimelineGameNumber,
    handleTimelineGameTabKeyDown,
  } = gameTabs;
  const { boardPreviewCards, activeReplayGroup } = view;
  return (
    <>
      {replayQuery.error ? (
        <StatusMessage tone="error">{replayQuery.error.message}</StatusMessage>
      ) : null}
      {replayQuery.isPending ? (
        <StatusMessage>Loading replay frames for review…</StatusMessage>
      ) : activeTimelineGameNumber !== null ? (
        <>
          {showTimelineGameTabs ? (
            <MatchGameTabs
              gameNumbers={timelineGameNumbers}
              activeGameNumber={activeTimelineGameNumber}
              baseId={timelineGameTabBaseId}
              ariaLabel="Review game selector"
              onSelect={setSelectedTimelineGameNumber}
              onKeyDown={handleTimelineGameTabKeyDown}
            />
          ) : null}
          <div
            id={showTimelineGameTabs ? activeTimelineGamePanelID : undefined}
            role={showTimelineGameTabs ? "tabpanel" : undefined}
            aria-labelledby={
              showTimelineGameTabs ? activeTimelineGameTabID : undefined
            }
          >
            <GameReviewPanel
              key={activeTimelineGameNumber}
              matchId={matchId}
              gameNumber={activeTimelineGameNumber}
              cards={boardPreviewCards}
              hasReplayFrames={(activeReplayGroup?.frames.length ?? 0) > 0}
            />
          </div>
        </>
      ) : (
        <StatusMessage>
          No games are available to review for this match.
        </StatusMessage>
      )}
    </>
  );
}
