import { useQuery } from "@tanstack/react-query";
import { useEffect, useId, useState, type KeyboardEvent } from "react";
import { useParams } from "react-router-dom";
import { useBreadcrumbLabel } from "../components/Breadcrumbs";
import { MatchAnalyticsPanel } from "../components/match-detail/MatchAnalyticsPanel";
import {
  SectionTabs,
  sectionPanelID,
  sectionTabID,
} from "../components/SectionTabs";
import { StatusMessage } from "../components/StatusMessage";
import { api } from "../lib/api";
import { useMatchReplay } from "../lib/replay/useMatchReplay";
import { useEventSets } from "../lib/useEventSets";

import type { MatchGameTabState } from "../components/match-detail/MatchGameTabs";
import { MatchOverviewPanel } from "../components/match-detail/MatchOverviewPanel";
import { MatchReplayPanel } from "../components/match-detail/MatchReplayPanel";
import { MatchReviewSection } from "../components/match-detail/MatchReviewSection";
import {
  OpponentCardsPanel,
  useOpponentCardsMetadata,
} from "../components/match-detail/OpponentCardsPanel";
import { useMatchReplayView } from "../lib/replay/useMatchReplayView";

const MATCH_SECTIONS = [
  { id: "replay", label: "Replay" },
  { id: "analytics", label: "Game Analytics" },
  { id: "opponent", label: "Opponent Cards" },
  { id: "review", label: "AI Game Review" },
] as const;

type MatchSection = (typeof MATCH_SECTIONS)[number]["id"];

export function MatchDetailPage() {
  const params = useParams();
  const matchId = Number(params.matchId);
  const isValidMatchID = Number.isFinite(matchId);
  const [activeSection, setActiveSection] = useState<MatchSection>("replay");
  const [selectedTimelineGameNumber, setSelectedTimelineGameNumber] = useState<
    number | null
  >(null);
  const timelineGameTabBaseId = useId();
  const sectionTabBaseId = useId();

  const query = useQuery({
    queryKey: ["match-detail", matchId],
    queryFn: () => api.matchDetail(matchId),
    enabled: isValidMatchID,
  });
  const timelineQuery = useQuery({
    queryKey: ["match-timeline", matchId],
    queryFn: () => api.matchTimeline(matchId),
    enabled: isValidMatchID,
  });
  const replayQuery = useMatchReplay(
    matchId,
    isValidMatchID &&
      (activeSection === "replay" || activeSection === "review"),
  );
  const { lookup: setLookup } = useEventSets([query.data?.match.eventName]);
  useBreadcrumbLabel(
    query.data
      ? `Match #${query.data.match.id} · ${query.data.match.opponent || "Unknown opponent"}`
      : null,
  );

  const opponentCards = useOpponentCardsMetadata(
    query.data?.opponentObservedCards ?? [],
  );

  const replayView = useMatchReplayView({
    timelineRows: timelineQuery.data ?? query.data?.cardPlays ?? [],
    replayFrames: replayQuery.data ?? [],
    matchResult: query.data?.match.result,
    games: query.data?.games,
    selectedTimelineGameNumber,
  });
  const { timelineGameNumbers, activeTimelineGameNumber } = replayView;
  const showTimelineGameTabs = timelineGameNumbers.length > 1;
  const activeTimelineGameTabID =
    activeTimelineGameNumber === null
      ? undefined
      : `${timelineGameTabBaseId}-tab-${activeTimelineGameNumber}`;
  const activeTimelineGamePanelID =
    activeTimelineGameNumber === null
      ? undefined
      : `${timelineGameTabBaseId}-panel-${activeTimelineGameNumber}`;

  useEffect(() => {
    if (timelineGameNumbers.length === 0) {
      setSelectedTimelineGameNumber(null);
      return;
    }

    setSelectedTimelineGameNumber((currentGameNumber) =>
      currentGameNumber !== null &&
      timelineGameNumbers.includes(currentGameNumber)
        ? currentGameNumber
        : timelineGameNumbers[0],
    );
  }, [timelineGameNumbers]);

  function handleTimelineGameTabKeyDown(
    event: KeyboardEvent<HTMLButtonElement>,
    gameNumber: number,
  ) {
    const currentIndex = timelineGameNumbers.indexOf(gameNumber);
    if (currentIndex === -1) return;

    switch (event.key) {
      case "ArrowLeft":
      case "ArrowUp":
        event.preventDefault();
        setSelectedTimelineGameNumber(
          timelineGameNumbers[
            (currentIndex + timelineGameNumbers.length - 1) %
              timelineGameNumbers.length
          ],
        );
        break;
      case "ArrowRight":
      case "ArrowDown":
        event.preventDefault();
        setSelectedTimelineGameNumber(
          timelineGameNumbers[(currentIndex + 1) % timelineGameNumbers.length],
        );
        break;
      case "Home":
        event.preventDefault();
        setSelectedTimelineGameNumber(timelineGameNumbers[0]);
        break;
      case "End":
        event.preventDefault();
        setSelectedTimelineGameNumber(
          timelineGameNumbers[timelineGameNumbers.length - 1],
        );
        break;
      default:
        break;
    }
  }

  const gameTabs: MatchGameTabState = {
    timelineGameNumbers,
    activeTimelineGameNumber,
    showTimelineGameTabs,
    activeTimelineGameTabID,
    activeTimelineGamePanelID,
    timelineGameTabBaseId,
    setSelectedTimelineGameNumber,
    handleTimelineGameTabKeyDown,
  };

  if (!isValidMatchID)
    return <StatusMessage tone="error">Invalid match id.</StatusMessage>;
  if (query.isLoading) return <StatusMessage>Loading match…</StatusMessage>;
  if (query.error)
    return (
      <StatusMessage tone="error">
        {(query.error as Error).message}
      </StatusMessage>
    );
  if (!query.data) return <StatusMessage>Match not found.</StatusMessage>;

  const { match } = query.data;

  return (
    <div className="stack-lg">
      <MatchOverviewPanel match={match} setLookup={setLookup} />

      <SectionTabs
        sections={MATCH_SECTIONS}
        activeSection={activeSection}
        baseId={sectionTabBaseId}
        label="Match detail sections"
        onSelect={setActiveSection}
      />

      {activeSection === "review" ? (
        <div
          id={sectionPanelID(sectionTabBaseId, "review")}
          role="tabpanel"
          aria-labelledby={sectionTabID(sectionTabBaseId, "review")}
          className="stack-lg"
        >
          <MatchReviewSection
            matchId={matchId}
            replayQuery={replayQuery}
            view={replayView}
            gameTabs={gameTabs}
          />
        </div>
      ) : null}

      {activeSection === "analytics" ? (
        <div
          id={sectionPanelID(sectionTabBaseId, "analytics")}
          role="tabpanel"
          aria-labelledby={sectionTabID(sectionTabBaseId, "analytics")}
        >
          <MatchAnalyticsPanel games={query.data.games ?? []} />
        </div>
      ) : null}

      {activeSection === "replay" ? (
        <div
          id={sectionPanelID(sectionTabBaseId, "replay")}
          role="tabpanel"
          aria-labelledby={sectionTabID(sectionTabBaseId, "replay")}
        >
          <MatchReplayPanel
            bestOf={match.bestOf}
            replayQuery={replayQuery}
            timelineError={timelineQuery.error}
            view={replayView}
            gameTabs={gameTabs}
          />
        </div>
      ) : null}

      {activeSection === "opponent" ? (
        <div
          id={sectionPanelID(sectionTabBaseId, "opponent")}
          role="tabpanel"
          aria-labelledby={sectionTabID(sectionTabBaseId, "opponent")}
        >
          <OpponentCardsPanel cards={opponentCards} />
        </div>
      ) : null}
    </div>
  );
}
