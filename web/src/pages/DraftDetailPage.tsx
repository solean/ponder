import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";

import { DraftJourneyPanel, DraftPackReplayPanel } from "../components/DraftJourneyPanel";
import { useBreadcrumbLabel } from "../components/Breadcrumbs";
import { DraftPickLog } from "../components/DraftPickLog";
import { DraftPoolPanel } from "../components/DraftPoolPanel";
import { DraftSessionOverview } from "../components/DraftSessionOverview";
import { LimitedMatchupsPanel } from "../components/MatchupPanels";
import { StatusMessage } from "../components/StatusMessage";
import { api } from "../lib/api";
import { parseEventName } from "../lib/events";

export function DraftDetailPage() {
  const params = useParams();
  const draftId = Number(params.draftId);
  const isValidDraftID = Number.isInteger(draftId) && draftId > 0;

  const picksQuery = useQuery({
    queryKey: ["draft-picks", draftId],
    queryFn: () => api.draftPicks(draftId),
    enabled: isValidDraftID,
  });
  const sessionsQuery = useQuery({
    queryKey: ["drafts"],
    queryFn: api.drafts,
    enabled: isValidDraftID,
  });
  const session = (sessionsQuery.data ?? []).find((row) => row.id === draftId);
  const draftBreadcrumbLabel = session
    ? `Draft #${session.id} · ${parseEventName(session.eventName).kindLabel}`
    : null;
  useBreadcrumbLabel(draftBreadcrumbLabel);

  if (!isValidDraftID) {
    return <StatusMessage tone="error">Invalid draft id.</StatusMessage>;
  }
  if (sessionsQuery.isLoading || picksQuery.isLoading) {
    return <StatusMessage>Loading draft report…</StatusMessage>;
  }
  if (sessionsQuery.error) {
    return <StatusMessage tone="error">{(sessionsQuery.error as Error).message}</StatusMessage>;
  }
  if (picksQuery.error) {
    return <StatusMessage tone="error">{(picksQuery.error as Error).message}</StatusMessage>;
  }

  if (!session) {
    return <StatusMessage tone="error">Draft session not found.</StatusMessage>;
  }

  const picks = picksQuery.data ?? [];
  const setCode = parseEventName(session.eventName).setCode ?? "";

  return (
    <div className="stack-lg">
      <DraftSessionOverview session={session} picks={picks} />
      <DraftPoolPanel eventName={session.eventName} picks={picks} />
      <DraftPickLog picks={picks} />
      <DraftJourneyPanel picks={picks} />
      <DraftPackReplayPanel picks={picks} />
      <LimitedMatchupsPanel
        setCode={setCode}
        title="Set Metagame"
        description="Your record against opponent color pairs across every locally recorded draft of this set. These results provide set-wide context, not only this session."
      />
    </div>
  );
}
