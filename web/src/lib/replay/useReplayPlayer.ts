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
): ReplayPlayer {
  const [index, setIndex] = useState(initialIndex);
  const [isPlaying, setIsPlaying] = useState(false);
  const [speed, setSpeed] = useState(1);
  const lastIndex = length > 0 ? length - 1 : 0;
  const safeIndex = Math.min(index, lastIndex);
  const canPlay = length > 1;
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
      setIndex(0);
      setIsPlaying(false);
      return;
    }
    setIndex((current) => Math.min(current, length - 1));
  }, [length]);

  useEffect(() => {
    if (!isPlaying) {
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
  }, [isPlaying, lastIndex, safeIndex, speed, decisionIndices, length]);

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
  };
}
