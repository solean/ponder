import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";

import { BackLink, NotFoundState, QueryErrorState } from "../components/ErrorState";
import { LoadingMessage } from "../components/LoadingMessage";
import { useBreadcrumbLabel } from "../components/Breadcrumbs";
import { useDraftCardStats } from "../components/CardStats";
import { DraftPickLog } from "../components/DraftPickLog";
import { DraftPoolPanel } from "../components/DraftPoolPanel";
import { DraftSessionOverview } from "../components/DraftSessionOverview";
import { api } from "../lib/api";
import { useQueryError } from "../lib/useQueryError";
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
  const sessionsError = useQueryError(sessionsQuery);
  const picksError = useQueryError(picksQuery);
  const session = (sessionsQuery.data ?? []).find((row) => row.id === draftId);
  const draftBreadcrumbLabel = session
    ? `Draft #${session.id} · ${parseEventName(session.eventName).kindLabel}`
    : null;
  useBreadcrumbLabel(draftBreadcrumbLabel);
  const cardStats = useDraftCardStats(session?.eventName ?? "");

  if (!isValidDraftID) {
    return <NotFoundState title="This draft doesn't exist." backTo="/drafts" backLabel="Back to drafts" />;
  }
  if (sessionsError.error) {
    return <QueryErrorState view={sessionsError} size="page" actions={<BackLink to="/drafts">Back to drafts</BackLink>} />;
  }
  if (picksError.error) {
    return <QueryErrorState view={picksError} size="page" actions={<BackLink to="/drafts">Back to drafts</BackLink>} />;
  }

  if (sessionsQuery.isLoading || picksQuery.isLoading) {
    return <LoadingMessage page>Loading draft report…</LoadingMessage>;
  }
  if (!session) {
    return (
      <NotFoundState
        title="This draft doesn't exist."
        detail={`Ponder has no record of draft #${draftId}. The link may be out of date.`}
        backTo="/drafts"
        backLabel="Back to drafts"
      />
    );
  }

  const picks = picksQuery.data ?? [];

  return (
    <div className="stack-lg">
      <DraftSessionOverview session={session} picks={picks} />
      <DraftPoolPanel eventName={session.eventName} picks={picks} />
      <DraftPickLog picks={picks} cardStats={cardStats} />
    </div>
  );
}
