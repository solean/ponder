import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import ReactMarkdown, { type Components } from "react-markdown";

import { CardPreviewName } from "./CardPreviewName";
import { api, generateGameReview } from "../lib/api";
import { primerCardIdFromHref, remarkPrimerCardNames, type PrimerCard } from "../lib/primerCards";
import type { GameReview } from "../lib/types";

type GenerationState = "idle" | "generating" | "error";

function formatGeneratedAt(iso: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) {
    return iso;
  }
  return date.toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
}

export function GameReviewPanel({
  matchId,
  gameNumber,
  cards,
  hasReplayFrames,
}: {
  matchId: number;
  gameNumber: number;
  cards: readonly PrimerCard[];
  hasReplayFrames: boolean;
}) {
  const queryClient = useQueryClient();
  const [generation, setGeneration] = useState<GenerationState>("idle");
  const [streamText, setStreamText] = useState("");
  const [errorMessage, setErrorMessage] = useState("");
  const abortRef = useRef<AbortController | null>(null);
  const streamEndRef = useRef<HTMLDivElement | null>(null);

  const statusQuery = useQuery({
    queryKey: ["ai-status"],
    queryFn: api.aiStatus,
    staleTime: 1000 * 60 * 10,
  });
  const reviewQuery = useQuery({
    queryKey: ["game-review", matchId, gameNumber],
    queryFn: () => api.gameReview(matchId, gameNumber),
    enabled: Number.isFinite(matchId) && gameNumber > 0,
  });

  useEffect(() => () => abortRef.current?.abort(), []);

  useEffect(() => {
    if (generation === "generating") {
      streamEndRef.current?.scrollIntoView({ block: "nearest" });
    }
  }, [streamText, generation]);

  const available = statusQuery.data?.available ?? false;
  const providerStatus = statusQuery.data?.providers.find(
    (provider) => provider.id === statusQuery.data?.provider,
  );
  const modelName =
    providerStatus?.models.find((model) => model.id === statusQuery.data?.model)?.name ?? statusQuery.data?.model;
  const review = reviewQuery.data ?? null;
  const cardsById = useMemo(() => {
    const result = new Map<number, PrimerCard>();
    for (const card of cards) {
      if (card.cardName?.trim() && card.cardId > 0 && !result.has(card.cardId)) {
        result.set(card.cardId, card);
      }
    }
    return result;
  }, [cards]);
  const markdownComponents = useMemo<Components>(
    () => ({
      a: ({ href, children, node: _node, ...props }) => {
        const cardId = primerCardIdFromHref(href);
        const card = cardId == null ? null : cardsById.get(cardId);
        return card ? (
          <CardPreviewName cardId={card.cardId} cardName={card.cardName} label={children} inline />
        ) : (
          <a href={href} {...props}>
            {children}
          </a>
        );
      },
    }),
    [cardsById],
  );


  const startGeneration = () => {
    const controller = new AbortController();
    abortRef.current = controller;
    setGeneration("generating");
    setStreamText("");
    setErrorMessage("");

    void generateGameReview(
      matchId,
      gameNumber,
      {
        onDelta: (text) => setStreamText((current) => current + text),
        onDone: (saved: GameReview) => {
          queryClient.setQueryData(["game-review", matchId, gameNumber], saved);
          setGeneration("idle");
          setStreamText("");
        },
        onError: (message) => {
          setErrorMessage(message);
          void queryClient.invalidateQueries({ queryKey: ["ai-status"] });
          setGeneration("error");
        },
      },
      controller.signal,
    );
  };

  const cancelGeneration = () => {
    abortRef.current?.abort();
    setGeneration("idle");
    setStreamText("");
  };

  const isGenerating = generation === "generating";

  return (
    <section className="panel ai-content-panel">
      <div className="panel-head">
        <div>
          <h3>AI Game Review</h3>
          <p>
            {review
              ? `Game ${gameNumber} • generated ${formatGeneratedAt(review.createdAt)}${review.stale ? " • replay data has changed" : ""}`
              : `Mistakes, better lines, and practice priorities for game ${gameNumber}`}
          </p>
        </div>
        <div className="ai-content-actions">
          {review?.stale && !isGenerating ? <span className="ai-content-stale-badge">Outdated</span> : null}
          {isGenerating ? (
            <button type="button" className="tab" onClick={cancelGeneration}>
              Cancel
            </button>
          ) : (
            <button type="button" className="tab" onClick={startGeneration} disabled={!available || !hasReplayFrames}>
              {review ? "Regenerate" : "Review game"}
            </button>
          )}
        </div>
      </div>

      {generation === "error" ? <div className="ai-content-error">{errorMessage}</div> : null}

      {isGenerating ? (
        <div className="ai-content-stream" aria-live="polite">
          <div className="ai-content-stream-note">
            Reviewing game {gameNumber} with {statusQuery.data?.providerName ?? "AI"}
            {modelName ? ` (${modelName})` : ""}…
          </div>
          {streamText ? <pre>{streamText}</pre> : <div className="ai-content-stream-note">Waiting for response…</div>}
          <div ref={streamEndRef} />
        </div>
      ) : review ? (
        <div className="ai-content-body">
          <ReactMarkdown remarkPlugins={[[remarkPrimerCardNames, { cards }]]} components={markdownComponents}>
            {review.content}
          </ReactMarkdown>
        </div>
      ) : !hasReplayFrames ? (
        <div className="ai-content-stream-note">A game review needs recorded replay frames for this game.</div>
      ) : !available ? (
        <div className="ai-content-stream-note">{statusQuery.data?.detail}</div>
      ) : (
        <div className="ai-content-stream-note">
          No review yet. Generation uses your local {statusQuery.data?.providerName ?? "AI"} subscription login and
          evaluates only the game information Arena recorded.
        </div>
      )}
    </section>
  );
}
