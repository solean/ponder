import type { useMatchReplay } from "../../lib/replay/useMatchReplay";
import type { MatchReplayView } from "../../lib/replay/useMatchReplayView";
import type { Match } from "../../lib/types";
import { StatusMessage } from "../StatusMessage";
import type { MatchGameTabState } from "./MatchGameTabs";
import { MatchGameTabs } from "./MatchGameTabs";
import { MatchReplayFrameBoard } from "./MatchReplayFrameBoard";
import { MatchSideboardChangesPanel } from "./MatchSideboardChangesPanel";
import { MatchTimelineBoard } from "./MatchTimelineBoard";

export function MatchReplayPanel({
  bestOf,
  replayQuery,
  timelineError,
  view,
  gameTabs,
}: {
  bestOf: Match["bestOf"];
  replayQuery: ReturnType<typeof useMatchReplay>;
  timelineError: Error | null;
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
  const {
    timelineRows,
    hasReplayFrames,
    activeTimelineGameAnalytics,
    activeReplayGroup,
    activeTimelineGroup,
    boardPreviewByCardID,
    isBoardCardPreviewLoading,
  } = view;
  return (
    <section className="panel match-replay-panel">
      <div className="panel-head match-timeline-toolbar">
        <div className="match-timeline-heading">
          <h3>Game replay</h3>
        </div>
        {showTimelineGameTabs && activeTimelineGameNumber !== null ? (
          <MatchGameTabs
            gameNumbers={timelineGameNumbers}
            activeGameNumber={activeTimelineGameNumber}
            baseId={timelineGameTabBaseId}
            ariaLabel="Replay game selector"
            onSelect={setSelectedTimelineGameNumber}
            onKeyDown={handleTimelineGameTabKeyDown}
          />
        ) : null}
      </div>
      {bestOf === "bo3" &&
      activeTimelineGameNumber !== null &&
      activeTimelineGameNumber > 1 ? (
        <MatchSideboardChangesPanel
          gameNumber={activeTimelineGameNumber}
          changes={activeTimelineGameAnalytics?.sideboardChanges}
        />
      ) : null}
      <div className="stack-md">
        {!hasReplayFrames ? (
          <p className="match-board-disclaimer">
            First-sighting fallback: this board shows when cards were first
            publicly observed. Replay frames and the live stack are unavailable.
          </p>
        ) : null}
        {replayQuery.isPending ? (
          <StatusMessage>Loading replay frames…</StatusMessage>
        ) : hasReplayFrames ? (
          activeReplayGroup ? (
            <div
              id={showTimelineGameTabs ? activeTimelineGamePanelID : undefined}
              role={showTimelineGameTabs ? "tabpanel" : undefined}
              aria-labelledby={
                showTimelineGameTabs ? activeTimelineGameTabID : undefined
              }
            >
              <MatchReplayFrameBoard
                key={activeReplayGroup.gameNumber}
                gameNumber={activeReplayGroup.gameNumber}
                playDraw={activeTimelineGameAnalytics?.playDrawConfidence !== "unknown" ? activeTimelineGameAnalytics?.playDraw : undefined}
                frames={activeReplayGroup.frames}
                gameSummary={activeReplayGroup.summary}
                previewByCardID={boardPreviewByCardID}
              />
            </div>
          ) : (
            <StatusMessage>
              No observed replay data for this match yet.
            </StatusMessage>
          )
        ) : timelineError ? (
          <StatusMessage tone="error">{timelineError.message}</StatusMessage>
        ) : timelineRows.length === 0 ? (
          <StatusMessage>
            No observed replay data for this match yet.
          </StatusMessage>
        ) : activeTimelineGroup ? (
          <div
            id={showTimelineGameTabs ? activeTimelineGamePanelID : undefined}
            role={showTimelineGameTabs ? "tabpanel" : undefined}
            aria-labelledby={
              showTimelineGameTabs ? activeTimelineGameTabID : undefined
            }
          >
            <MatchTimelineBoard
              gameNumber={activeTimelineGroup[0]}
              plays={activeTimelineGroup[1]}
              previewByCardID={boardPreviewByCardID}
            />
          </div>
        ) : (
          <StatusMessage>
            No observed replay data for this match yet.
          </StatusMessage>
        )}
        {replayQuery.error ? (
          <StatusMessage tone="error">
            {replayQuery.error.message}
          </StatusMessage>
        ) : null}
        {isBoardCardPreviewLoading ? (
          <StatusMessage>Loading replay card art…</StatusMessage>
        ) : null}
      </div>
    </section>
  );
}
