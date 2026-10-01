import { useMemo, useState } from "react";
import { formatDateTime } from "../../lib/format";
import {
  boardPlayMeta,
  buildReplayTurnBoundaries,
  cardDisplayName,
  replayMomentLabel,
  replayTurnValue,
  timelinePlayerLabel,
  timelineZoneLabel,
} from "../../lib/replay";
import { useReplayKeyboard } from "../../lib/replay/useReplayKeyboard";
import { useReplayPlayer } from "../../lib/replay/useReplayPlayer";
import type { CardPreview } from "../../lib/scryfall";
import type { MatchCardPlay } from "../../lib/types";
import { StatusMessage } from "../StatusMessage";
import { MatchReplayCard } from "./CardPreviews";
import { MatchReplayBattlefield } from "./ReplayBattlefield";
import { MatchReplayScrubber, MatchReplaySpeedControl } from "./ReplayControls";
import { MatchReplaySideSummary, MatchReplayZoneDialog } from "./ReplayZones";
import type { MatchReplayZoneDialogState } from "./types";

export function MatchTimelineBoard({
  gameNumber,
  plays,
  previewByCardID,
}: {
  gameNumber: number;
  plays: MatchCardPlay[];
  previewByCardID: Map<number, CardPreview | null>;
}) {
  const {
    index: selectedActionIndex,
    setIndex: setSelectedActionIndex,
    isPlaying,
    setIsPlaying,
    speed,
    setSpeed,
  } = useReplayPlayer(plays.length, plays.length > 0 ? plays.length - 1 : 0);
  const [zoneDialogState, setZoneDialogState] =
    useState<MatchReplayZoneDialogState | null>(null);

  const currentAction = plays[selectedActionIndex] ?? null;
  const visiblePlays = useMemo(
    () => plays.slice(0, selectedActionIndex + 1),
    [plays, selectedActionIndex],
  );
  const opponentVisiblePlays = visiblePlays.filter(
    (play) => play.playerSide === "opponent",
  );
  const selfVisiblePlays = visiblePlays.filter(
    (play) => play.playerSide === "self",
  );
  const unknownVisiblePlays = visiblePlays.filter(
    (play) => play.playerSide === "unknown",
  );
  const turnBoundaries = useMemo(
    () => buildReplayTurnBoundaries(plays),
    [plays],
  );

  const currentTurnBoundaryIndex = currentAction
    ? turnBoundaries.findIndex(
        (boundary) =>
          boundary.turnKey === replayTurnValue(currentAction.turnNumber),
      )
    : -1;

  const lastActionIndex = plays.length > 0 ? plays.length - 1 : 0;
  const goToFirstAction = () => {
    setIsPlaying(false);
    setSelectedActionIndex(0);
  };
  const goToLastAction = () => {
    setIsPlaying(false);
    setSelectedActionIndex(lastActionIndex);
  };
  const goToPrevAction = () => {
    setIsPlaying(false);
    setSelectedActionIndex((currentIndex) => Math.max(currentIndex - 1, 0));
  };
  const goToNextAction = () => {
    setIsPlaying(false);
    setSelectedActionIndex((currentIndex) =>
      Math.min(currentIndex + 1, lastActionIndex),
    );
  };
  const goToPrevActionTurn = () => {
    setIsPlaying(false);
    setSelectedActionIndex(
      turnBoundaries[currentTurnBoundaryIndex - 1]?.firstIndex ?? 0,
    );
  };
  const goToNextActionTurn = () => {
    setIsPlaying(false);
    setSelectedActionIndex(
      turnBoundaries[currentTurnBoundaryIndex + 1]?.firstIndex ??
        lastActionIndex,
    );
  };
  const togglePlay = () => setIsPlaying((currentValue) => !currentValue);

  useReplayKeyboard({
    onStepBackward: goToPrevAction,
    onStepForward: goToNextAction,
    onPrevTurn: goToPrevActionTurn,
    onNextTurn: goToNextActionTurn,
    onTogglePlay: togglePlay,
    onFirst: goToFirstAction,
    onLast: goToLastAction,
  });

  if (!currentAction) {
    return (
      <article className="panel inner match-replay-game">
        <h4>Game {gameNumber}</h4>
        <StatusMessage>No observed card plays for this game.</StatusMessage>
      </article>
    );
  }

  const currentPreview = previewByCardID.get(currentAction.cardId) ?? null;
  const currentName =
    currentPreview?.name ??
    cardDisplayName({
      cardId: currentAction.cardId,
      cardName: currentAction.cardName,
    });
  const canStepBackward = selectedActionIndex > 0;
  const canStepForward = selectedActionIndex < plays.length - 1;
  const canJumpPrevTurn = currentTurnBoundaryIndex > 0;
  const canJumpNextTurn =
    currentTurnBoundaryIndex >= 0 &&
    currentTurnBoundaryIndex < turnBoundaries.length - 1;

  return (
    <article className="panel inner match-replay-game">
      <div className="match-replay-command-deck">
        <div className="match-replay-head">
          <div className="match-replay-head-copy">
            <h4>Game {gameNumber}</h4>
            <p className="match-replay-caption">
              {replayMomentLabel(currentAction)} • Action{" "}
              {selectedActionIndex + 1} of {plays.length}
            </p>
          </div>
          <p className="match-replay-kicker">Observed replay</p>
        </div>

        <div className="match-replay-controls-bar">
          <div
            className="match-replay-button-row"
            role="group"
            aria-label={`Game ${gameNumber} replay controls`}
          >
            <button
              type="button"
              className="match-replay-button is-primary"
              onClick={togglePlay}
              aria-pressed={isPlaying}
            >
              <span>{isPlaying ? "Pause" : "Play"}</span>
              <kbd className="match-replay-button-shortcut" aria-hidden="true">
                Space
              </kbd>
            </button>
            <button
              type="button"
              className="match-replay-button"
              onClick={goToPrevActionTurn}
              disabled={!canJumpPrevTurn}
              aria-label="Previous turn"
            >
              <span>Prev Turn</span>
              <kbd className="match-replay-button-shortcut" aria-hidden="true">
                ⇧←
              </kbd>
            </button>
            <button
              type="button"
              className="match-replay-button"
              onClick={goToPrevAction}
              disabled={!canStepBackward}
              aria-label="Previous action"
            >
              <span>Prev Action</span>
              <kbd className="match-replay-button-shortcut" aria-hidden="true">
                ←
              </kbd>
            </button>
            <button
              type="button"
              className="match-replay-button"
              onClick={goToNextAction}
              disabled={!canStepForward}
            >
              <span>Next Action</span>
              <kbd className="match-replay-button-shortcut" aria-hidden="true">
                →
              </kbd>
            </button>
            <button
              type="button"
              className="match-replay-button"
              onClick={goToNextActionTurn}
              disabled={!canJumpNextTurn}
            >
              <span>Next Turn</span>
              <kbd className="match-replay-button-shortcut" aria-hidden="true">
                ⇧→
              </kbd>
            </button>
          </div>

          <div className="match-replay-controls-aux">
            <MatchReplaySpeedControl speed={speed} onSelectSpeed={setSpeed} />
          </div>
        </div>

        <div className="match-replay-track-panel">
          <MatchReplayScrubber
            length={plays.length}
            index={selectedActionIndex}
            onSeek={(nextIndex) => {
              setIsPlaying(false);
              setSelectedActionIndex(nextIndex);
            }}
            turnBoundaries={turnBoundaries}
            itemLabel="action"
          />
        </div>
      </div>

      <div className="match-replay-canvas">
        <aside className="match-replay-sidebar">
          <MatchReplaySideSummary
            side="opponent"
            plays={opponentVisiblePlays}
            onOpenZone={setZoneDialogState}
          />

          <section
            className="match-replay-stackbox"
            aria-label={`Game ${gameNumber} current action`}
          >
            <div className="match-replay-stackbox-head">
              <p className="match-replay-sidebox-label">Current Action</p>
              <p className="match-replay-sidebox-total">
                #{selectedActionIndex + 1}
              </p>
            </div>
            <div className="match-replay-stackbox-body">
              <MatchReplayCard
                play={currentAction}
                preview={currentPreview}
                active
                size="stack"
              />
              <div className="match-replay-stackbox-copy">
                <p className="match-replay-stackbox-player">
                  {timelinePlayerLabel(currentAction.playerSide)}
                </p>
                <h5>{currentName}</h5>
                <p>{timelineZoneLabel(currentAction.firstPublicZone)}</p>
                <p>{boardPlayMeta(currentAction)}</p>
                <p>
                  {currentAction.playedAt
                    ? formatDateTime(currentAction.playedAt)
                    : "Unknown time"}
                </p>
              </div>
            </div>
          </section>

          <MatchReplaySideSummary
            side="self"
            plays={selfVisiblePlays}
            onOpenZone={setZoneDialogState}
          />
        </aside>

        <div className="match-replay-board is-observed-board">
          <MatchReplayBattlefield
            side="opponent"
            plays={opponentVisiblePlays}
            activePlayID={currentAction.id}
            previewByCardID={previewByCardID}
          />

          <section
            className="match-replay-centerline"
            aria-label="Replay status"
          >
            <p className="match-replay-centerline-title">
              {replayMomentLabel(currentAction)}
            </p>
            <p className="match-replay-centerline-copy">
              {timelinePlayerLabel(currentAction.playerSide)} first showed{" "}
              {currentName} in{" "}
              {timelineZoneLabel(currentAction.firstPublicZone)}.
            </p>
            {unknownVisiblePlays.length > 0 ? (
              <p className="match-replay-centerline-copy">
                {unknownVisiblePlays.length} observation
                {unknownVisiblePlays.length === 1 ? "" : "s"} still have an
                unknown owner.
              </p>
            ) : null}
          </section>

          <MatchReplayBattlefield
            side="self"
            plays={selfVisiblePlays}
            activePlayID={currentAction.id}
            previewByCardID={previewByCardID}
          />
        </div>
      </div>

      <MatchReplayZoneDialog
        state={zoneDialogState}
        previewByCardID={previewByCardID}
        onClose={() => setZoneDialogState(null)}
      />
    </article>
  );
}
