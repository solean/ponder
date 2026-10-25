import type { useMatchReplay } from "../../lib/replay/useMatchReplay";
import type { MatchReplayView } from "../../lib/replay/useMatchReplayView";
import { replayRecordingCoverage } from "../../lib/replay/recordingCoverage";
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
    replayFrames,
    activeTimelineGameAnalytics,
    activeReplayGroup,
    activeTimelineGroup,
    boardPreviewByCardID,
    isBoardCardPreviewLoading,
  } = view;
  const rawGameFrames = replayFrames.filter(frame => (frame.gameNumber && frame.gameNumber > 0 ? frame.gameNumber : 1) === (activeTimelineGameNumber ?? 1));
  const hasPlayableReplay = (activeReplayGroup?.frames.length ?? 0) > 0;
  const hasFallback = (activeTimelineGroup?.[1].length ?? 0) > 0;
  const coverage = replayRecordingCoverage({
    frames: rawGameFrames,
    hasFallback,
    matchComplete: replayQuery.matchComplete,
    gameEnded: Boolean(activeTimelineGameAnalytics?.endedAt),
    isLatestGame: activeTimelineGameNumber === timelineGameNumbers[timelineGameNumbers.length - 1],
  });
  const canDescribeCoverage = !replayQuery.isPending && ((!replayQuery.error && !timelineError) || rawGameFrames.length > 0 || hasFallback);
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
        {replayQuery.error || timelineError ? (
          <div className="match-replay-recording-error">
            <StatusMessage tone="error">
              Unable to {replayQuery.error ? "load or refresh the recording" : "load first public sightings"}. Retry to check the saved data again.
              {hasPlayableReplay || hasFallback ? " Previously loaded data remains available below." : ""}
            </StatusMessage>
            <button className="match-replay-button" type="button" disabled={replayQuery.isRetrying} onClick={() => void replayQuery.retry()}>
              {replayQuery.isRetrying ? "Retrying…" : "Retry loading replay"}
            </button>
          </div>
        ) : null}
        {canDescribeCoverage ? (
          <div className="match-replay-recording" data-recording-state={coverage.state}>
            <p><strong>{activeTimelineGameNumber !== null ? `Game ${activeTimelineGameNumber} · ` : ""}{coverage.label}</strong></p>
            <p>{coverage.detail}</p>
          </div>
        ) : null}
        {replayQuery.isPending && !hasPlayableReplay && !hasFallback ? (
          <StatusMessage>Checking replay recording…</StatusMessage>
        ) : (
          <div
            id={showTimelineGameTabs ? activeTimelineGamePanelID : undefined}
            role={showTimelineGameTabs ? "tabpanel" : undefined}
            aria-labelledby={showTimelineGameTabs ? activeTimelineGameTabID : undefined}
          >
            {hasPlayableReplay && activeReplayGroup ? (
              <MatchReplayFrameBoard
                key={activeReplayGroup.gameNumber}
                gameNumber={activeReplayGroup.gameNumber}
                playDraw={activeTimelineGameAnalytics?.playDrawConfidence !== "unknown" ? activeTimelineGameAnalytics?.playDraw : undefined}
                frames={activeReplayGroup.frames}
                gameSummary={activeReplayGroup.summary}
                previewByCardID={boardPreviewByCardID}
              />
            ) : hasFallback && activeTimelineGroup ? (
              <>
                {coverage.state !== "fallback" ? <p className="match-board-disclaimer">First-sighting fallback: only first public card sightings are shown; later moves and the live stack cannot be reconstructed.</p> : null}
                <MatchTimelineBoard
                  gameNumber={activeTimelineGroup[0]}
                  plays={activeTimelineGroup[1]}
                  previewByCardID={boardPreviewByCardID}
                />
              </>
            ) : rawGameFrames.length > 0 ? (
              <StatusMessage>No playable board states were saved for this game.</StatusMessage>
            ) : null}
          </div>
        )}
        {isBoardCardPreviewLoading ? (
          <StatusMessage>Loading replay card art…</StatusMessage>
        ) : null}
      </div>
    </section>
  );
}
