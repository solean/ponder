import { type KeyboardEvent } from "react";

export function MatchGameTabs({
  gameNumbers,
  activeGameNumber,
  baseId,
  ariaLabel,
  onSelect,
  onKeyDown,
}: {
  gameNumbers: readonly number[];
  activeGameNumber: number;
  baseId: string;
  ariaLabel: string;
  onSelect: (gameNumber: number) => void;
  onKeyDown: (
    event: KeyboardEvent<HTMLButtonElement>,
    gameNumber: number,
  ) => void;
}) {
  return (
    <div
      className="tabs tabs--sm match-timeline-game-tabs"
      role="tablist"
      aria-label={ariaLabel}
    >
      {gameNumbers.map((gameNumber) => (
        <button
          key={gameNumber}
          type="button"
          id={`${baseId}-tab-${gameNumber}`}
          role="tab"
          aria-selected={activeGameNumber === gameNumber}
          aria-controls={`${baseId}-panel-${gameNumber}`}
          tabIndex={activeGameNumber === gameNumber ? 0 : -1}
          className={`tab ${activeGameNumber === gameNumber ? "is-active" : ""}`}
          onClick={() => onSelect(gameNumber)}
          onKeyDown={(event) => onKeyDown(event, gameNumber)}
        >
          Game {gameNumber}
        </button>
      ))}
    </div>
  );
}

export type MatchGameTabState = {
  timelineGameNumbers: number[];
  activeTimelineGameNumber: number | null;
  showTimelineGameTabs: boolean;
  activeTimelineGameTabID: string | undefined;
  activeTimelineGamePanelID: string | undefined;
  timelineGameTabBaseId: string;
  setSelectedTimelineGameNumber: (gameNumber: number) => void;
  handleTimelineGameTabKeyDown: (
    event: KeyboardEvent<HTMLButtonElement>,
    gameNumber: number,
  ) => void;
};
