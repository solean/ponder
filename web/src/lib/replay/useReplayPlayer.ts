import { readReplayPosition, saveReplayPosition, type ReplayPositionOptions } from "./position";
import { useEffect, useState } from "react";
import { replayNavigationIndex } from "./navigation";

/** Autoplay cadence at 1× speed; divided by the selected speed multiplier. */
const BASE_AUTOPLAY_INTERVAL_MS = 1200;

/** Selectable autoplay speed multipliers, slowest to fastest. */
export const REPLAY_SPEED_OPTIONS = [0.5, 1, 2, 4] as const;

type ReplayPlayer = {
  /** Current step, always clamped to the valid range for `length`. */
  index: number;
  setIndex: (next: number | ((current: number) => number)) => void;
  isPlaying: boolean;
  togglePlay: () => void;
  canPlay: boolean;
  playLabel: "Play" | "Pause" | "Replay";
  setIsPlaying: (next: boolean | ((current: boolean) => boolean)) => void;
  /** Autoplay speed multiplier (1 = the original 1200ms cadence). */
  speed: number;
  setSpeed: (next: number) => void;
  /** Highest valid index (0 when the timeline is empty). */
  lastIndex: number;
  missingPosition: boolean;
};

/**
 * Owns the shared replay transport state — selected step, play/pause, autoplay
 * advance, and re-clamping when the underlying timeline shrinks. Extracted from
 * the previously duplicated state machines in the frame board and the observed
 * timeline board so both stay in lockstep (and so the scrubber/keyboard work in
 * the redesign hangs off one place).
 */
export function useReplayPlayer(
  length: number,
  initialIndex: number,
  decisionIndices?: number[],
  position?: ReplayPositionOptions,
): ReplayPlayer {
  const [saved] = useState(() => position ? readReplayPosition(position.key) : null);
  const [index, setRawIndex] = useState(initialIndex);
  const [frameId, setFrameId] = useState<number | undefined>(() => saved?.frameId ?? position?.ids[initialIndex]);
  const [isPlaying, setIsPlaying] = useState(false);
  const [speed, setSpeed] = useState(saved?.speed ?? 1);
  const lastIndex = length > 0 ? length - 1 : 0;
  const identifiedIndex = frameId !== undefined && position ? position.ids.indexOf(frameId) : -1;
  const missingPosition = Boolean(position && frameId !== undefined && identifiedIndex < 0);
  const safeIndex = position && frameId !== undefined ? Math.max(0, identifiedIndex) : Math.max(0, Math.min(index, lastIndex));
  const canPlay = length > 1 && !missingPosition;
  const setIndex = (next: number | ((current: number) => number)) => {
    const value = Math.max(0, Math.min(lastIndex, typeof next === "function" ? next(safeIndex) : next));
    setRawIndex(value);
    if (position) setFrameId(position.ids[value]);
  };
  useEffect(() => {
    if (position && !missingPosition && position.ids[safeIndex] !== undefined) {
      saveReplayPosition(position.key, { frameId: position.ids[safeIndex]!, speed });
    }
  }, [position?.key, position?.ids[safeIndex], speed, missingPosition]);
  useEffect(() => { if (missingPosition) setIsPlaying(false); }, [missingPosition]);
  const playLabel = isPlaying
    ? "Pause"
    : canPlay && safeIndex === lastIndex
      ? "Replay"
      : "Play";

  const togglePlay = () => {
    if (!canPlay) {
      return;
    }
    // Completed games open on the final board; starting playback replays them.
    if (!isPlaying && safeIndex === lastIndex) {
      setIndex(0);
    }
    setIsPlaying((current) => !current);
  };

  useEffect(() => {
    if (length === 0) {
      if (!position) setRawIndex(0);
      setIsPlaying(false);
      return;
    }
    if (!position) setRawIndex((current) => Math.min(current, length - 1));
  }, [length]);

  useEffect(() => {
    if (!isPlaying || missingPosition) {
      return;
    }
    if (safeIndex >= lastIndex) {
      setIsPlaying(false);
      return;
    }

    const timeoutID = window.setTimeout(() => {
      setIndex(replayNavigationIndex(safeIndex, 1, length, decisionIndices));
    }, BASE_AUTOPLAY_INTERVAL_MS / speed);

    return () => window.clearTimeout(timeoutID);
  }, [isPlaying, lastIndex, safeIndex, speed, decisionIndices, length, missingPosition]);

  return {
    index: safeIndex,
    setIndex,
    isPlaying,
    togglePlay,
    canPlay,
    playLabel,
    setIsPlaying,
    speed,
    setSpeed,
    lastIndex,
    missingPosition,
  };
}
