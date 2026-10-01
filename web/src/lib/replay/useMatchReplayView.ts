import { useQueries } from "@tanstack/react-query";
import { useMemo } from "react";
import type { CardPreview } from "../scryfall";
import { fetchCardPreview } from "../scryfall";
import type {
  GameAnalytics,
  Match,
  MatchCardPlay,
  MatchReplayFrame,
} from "../types";
import {
  buildReplayGameGroups,
  type PreviewCard,
  type ReplayGameGroup,
} from "./index";
import { cardPreviewQueryKey } from "./cardPreviewQuery";

export function useMatchReplayView({
  timelineRows,
  replayFrames,
  matchResult,
  games,
  selectedTimelineGameNumber,
}: {
  timelineRows: MatchCardPlay[];
  replayFrames: MatchReplayFrame[];
  matchResult: Match["result"] | undefined;
  games: GameAnalytics[] | undefined;
  selectedTimelineGameNumber: number | null;
}) {
  const replayGroups = useMemo<ReplayGameGroup[]>(
    () => buildReplayGameGroups(replayFrames, matchResult ?? "unknown"),
    [matchResult, replayFrames],
  );
  const visibleReplayFrames = useMemo(
    () => replayGroups.flatMap((group) => group.frames),
    [replayGroups],
  );
  const hasReplayFrames = visibleReplayFrames.length > 0;
  const boardPreviewCards = useMemo<PreviewCard[]>(() => {
    const uniqueCards = new Map<number, PreviewCard>();

    if (hasReplayFrames) {
      for (const frame of visibleReplayFrames) {
        for (const object of frame.objects ?? []) {
          if (!uniqueCards.has(object.cardId)) {
            uniqueCards.set(object.cardId, {
              cardId: object.cardId,
              cardName: object.cardName,
            });
          }
        }
        for (const change of frame.changes ?? []) {
          if (!uniqueCards.has(change.cardId)) {
            uniqueCards.set(change.cardId, {
              cardId: change.cardId,
              cardName: change.cardName,
            });
          }
        }
      }
    } else {
      for (const play of timelineRows) {
        if (!uniqueCards.has(play.cardId)) {
          uniqueCards.set(play.cardId, {
            cardId: play.cardId,
            cardName: play.cardName,
          });
        }
      }
    }

    return Array.from(uniqueCards.values());
  }, [hasReplayFrames, timelineRows, visibleReplayFrames]);
  const boardCardPreviewQueries = useQueries({
    queries: boardPreviewCards.map((card) => ({
      queryKey: cardPreviewQueryKey(card),
      queryFn: () => fetchCardPreview(card.cardId, card.cardName),
      enabled: card.cardId > 0,
      staleTime: 1000 * 60 * 60 * 24,
      gcTime: 1000 * 60 * 60 * 24,
      retry: 1,
    })),
  });
  const boardPreviewByCardID = useMemo(() => {
    const out = new Map<number, CardPreview | null>();
    for (let index = 0; index < boardPreviewCards.length; index += 1) {
      const card = boardPreviewCards[index];
      out.set(card.cardId, boardCardPreviewQueries[index]?.data ?? null);
    }
    return out;
  }, [boardPreviewCards, boardCardPreviewQueries]);
  const isBoardCardPreviewLoading = boardCardPreviewQueries.some(
    (queryRow) => queryRow.isPending,
  );
  const timelineGroups = useMemo(() => {
    const byGame = new Map<number, MatchCardPlay[]>();
    for (const play of timelineRows) {
      const gameNumber =
        play.gameNumber && play.gameNumber > 0 ? play.gameNumber : 1;
      const rows = byGame.get(gameNumber);
      if (rows) {
        rows.push(play);
      } else {
        byGame.set(gameNumber, [play]);
      }
    }

    return Array.from(byGame.entries()).sort((a, b) => a[0] - b[0]);
  }, [timelineRows]);
  const timelineGameNumbers = useMemo(() => {
    if (hasReplayFrames) {
      return replayGroups.map((group) => group.gameNumber);
    }
    return timelineGroups.map(([gameNumber]) => gameNumber);
  }, [hasReplayFrames, replayGroups, timelineGroups]);
  const activeTimelineGameNumber =
    selectedTimelineGameNumber ?? timelineGameNumbers[0] ?? null;
  const gameAnalyticsByNumber = useMemo(
    () => new Map((games ?? []).map((game) => [game.gameNumber, game])),
    [games],
  );
  const activeTimelineGameAnalytics =
    activeTimelineGameNumber === null
      ? null
      : (gameAnalyticsByNumber.get(activeTimelineGameNumber) ?? null);
  const activeReplayGroup =
    activeTimelineGameNumber === null
      ? null
      : (replayGroups.find(
          (group) => group.gameNumber === activeTimelineGameNumber,
        ) ?? null);
  const activeTimelineGroup =
    activeTimelineGameNumber === null
      ? null
      : (timelineGroups.find(
          ([gameNumber]) => gameNumber === activeTimelineGameNumber,
        ) ?? null);
  return {
    timelineRows,
    hasReplayFrames,
    boardPreviewCards,
    boardPreviewByCardID,
    isBoardCardPreviewLoading,
    timelineGameNumbers,
    activeTimelineGameNumber,
    activeTimelineGameAnalytics,
    activeReplayGroup,
    activeTimelineGroup,
  };
}

export type MatchReplayView = ReturnType<typeof useMatchReplayView>;
