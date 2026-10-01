import { useEffect, useMemo, useRef, useState } from "react";
import { usePersistedFlag } from "../../lib/persistedFlag";
import {
  boardZoneKind,
  buildReplayAttachmentState,
  buildReplayBeat,
  buildReplayBoardCensus,
  buildReplayLifeSeries,
  buildReplayRelationshipIndex,
  buildReplayTickKinds,
  buildReplayTurnBoundaries,
  findReplayKeyMoments,
  preferredReplayFrameIndex,
  replayAnnotationDetailIntValue,
  replayAnnotationHasType,
  replayFrameAnnotations,
  replayFrameCrewEvents,
  replayFrameMomentLabel,
  replayLifeDelta,
  replayObjectBlockAttackerIDs,
  replayObjectIsAttacking,
  replayObjectIsBlocking,
  replayPlayerConnectionId,
  replayRelationshipTargetForId,
  replayTurnValue,
  sortReplayObjects,
  type ReplayBoardConnection,
  type ReplayGameSummary,
} from "../../lib/replay";
import { useReplayKeyboard } from "../../lib/replay/useReplayKeyboard";
import { useReplayPlayer } from "../../lib/replay/useReplayPlayer";
import type { CardPreview } from "../../lib/scryfall";
import type { MatchReplayFrame, MatchReplayFrameObject } from "../../lib/types";
import { ResultPill } from "../ResultPill";
import { StatusMessage } from "../StatusMessage";
import {
  MatchReplayFrameBattlefield,
  MatchReplayHand,
  MatchReplayStack,
} from "./ReplayBattlefield";
import { MatchReplayConnectionOverlay } from "./ReplayConnections";
import type { ReplayScrubberSnapshot } from "./ReplayControls";
import {
  MatchReplayHud,
  MatchReplayMoveList,
  MatchReplayScrubber,
  MatchReplaySpeedControl,
} from "./ReplayControls";
import {
  MatchReplayFrameSideSummary,
  MatchReplayZoneDialog,
} from "./ReplayZones";
import type { MatchReplayZoneDialogState } from "./types";

/** One preference for every replay, so it survives game switches and reloads. */
const REPLAY_MOVELIST_COLLAPSED_KEY = "ponder.replayMoveListCollapsed";

export function MatchReplayFrameBoard({
  gameNumber,
  frames,
  gameSummary,
  previewByCardID,
}: {
  gameNumber: number;
  frames: MatchReplayFrame[];
  gameSummary: ReplayGameSummary | null;
  previewByCardID: Map<number, CardPreview | null>;
}) {
  const {
    index: safeSelectedFrameIndex,
    setIndex: setSelectedFrameIndex,
    isPlaying,
    setIsPlaying,
    speed,
    setSpeed,
    lastIndex: lastFrameIndex,
  } = useReplayPlayer(frames.length, preferredReplayFrameIndex(frames));
  const [zoneDialogState, setZoneDialogState] =
    useState<MatchReplayZoneDialogState | null>(null);
  const [focusedConnectionInstanceId, setFocusedConnectionInstanceId] =
    useState<number | null>(null);
  const [moveListCollapsed, setMoveListCollapsed] = usePersistedFlag(
    REPLAY_MOVELIST_COLLAPSED_KEY,
    false,
  );
  const canvasRef = useRef<HTMLDivElement | null>(null);
  const replayCardShellsRef = useRef(new Map<number, HTMLElement>());

  const currentFrame = frames[safeSelectedFrameIndex] ?? null;
  const previousFrame =
    safeSelectedFrameIndex > 0
      ? (frames[safeSelectedFrameIndex - 1] ?? null)
      : null;
  const turnBoundaries = useMemo(
    () => buildReplayTurnBoundaries(frames),
    [frames],
  );
  const lifeSeries = useMemo(() => buildReplayLifeSeries(frames), [frames]);
  const tickKinds = useMemo(() => buildReplayTickKinds(frames), [frames]);
  const keyMoments = useMemo(() => findReplayKeyMoments(frames), [frames]);
  const relationships = useMemo(
    () => buildReplayRelationshipIndex(frames),
    [frames],
  );
  const targetsBySourceId = relationships.spellTargetsBySourceId;

  // Lazily-built, cached per index: users sweep the scrubber back and forth
  // over the same frames, so each snapshot is computed at most once.
  const scrubberSnapshotForIndex = useMemo(() => {
    const cache = new Map<number, ReplayScrubberSnapshot>();
    return (frameIndex: number): ReplayScrubberSnapshot | null => {
      const frame = frames[frameIndex];
      if (!frame) {
        return null;
      }
      const cached = cache.get(frameIndex);
      if (cached) {
        return cached;
      }
      const prev = frameIndex > 0 ? (frames[frameIndex - 1] ?? null) : null;
      const life = lifeSeries[frameIndex];
      const snapshot: ReplayScrubberSnapshot = {
        momentLabel: replayFrameMomentLabel(frame),
        stepLabel: `Step ${frameIndex + 1}/${frames.length}`,
        selfLife: life?.self ?? null,
        opponentLife: life?.opponent ?? null,
        selfLifeDelta: replayLifeDelta(prev, frame, "self"),
        opponentLifeDelta: replayLifeDelta(prev, frame, "opponent"),
        census: buildReplayBoardCensus(frame),
        beat: buildReplayBeat(frame, prev, relationships),
      };
      cache.set(frameIndex, snapshot);
      return snapshot;
    };
  }, [frames, lifeSeries, relationships]);

  const currentTurnBoundaryIndex = currentFrame
    ? turnBoundaries.findIndex(
        (boundary) =>
          boundary.turnKey === replayTurnValue(currentFrame.turnNumber),
      )
    : -1;
  const currentObjects = currentFrame?.objects ?? [];
  const combatConnections = useMemo(() => {
    const battlefieldByID = new Map<number, MatchReplayFrameObject>();
    for (const object of currentObjects) {
      if (boardZoneKind(object.zoneType) !== "battlefield") {
        continue;
      }
      battlefieldByID.set(object.instanceId, object);
    }

    const next: ReplayBoardConnection[] = [];
    for (const object of battlefieldByID.values()) {
      if (!replayObjectIsBlocking(object)) {
        continue;
      }
      for (const attackerId of replayObjectBlockAttackerIDs(object)) {
        const attacker = battlefieldByID.get(attackerId);
        if (!attacker || !replayObjectIsAttacking(attacker)) {
          continue;
        }
        next.push({
          kind: "combat",
          sourceId: object.instanceId,
          targetId: attackerId,
        });
      }
    }
    return next;
  }, [currentObjects]);
  const relationshipPresentation = useMemo(() => {
    const visibleIds = new Set<number>();
    const stackByID = new Set<number>();
    for (const object of currentObjects) {
      const kind = boardZoneKind(object.zoneType);
      if (kind === "battlefield") visibleIds.add(object.instanceId);
      if (kind === "stack") {
        stackByID.add(object.instanceId);
        visibleIds.add(object.instanceId);
      }
    }

    const connections: ReplayBoardConnection[] = [];
    const relationshipLabelsByObjectId = new Map<number, string[]>();
    const targetedPlayerSides = new Set<"self" | "opponent">();
    const attackSummaries = new Map<
      "self" | "opponent",
      { attackers: number; power: number }
    >();
    const addLabel = (id: number, label: string) => {
      const labels = relationshipLabelsByObjectId.get(id) ?? [];
      if (!labels.includes(label)) labels.push(label);
      relationshipLabelsByObjectId.set(id, labels);
    };

    for (const [sourceId, targets] of targetsBySourceId) {
      if (!stackByID.has(sourceId)) {
        continue;
      }
      for (const target of targets) {
        if (target.playerSide) targetedPlayerSides.add(target.playerSide);
        connections.push({
          kind: "spellTarget",
          sourceId,
          targetId: target.connectionId,
        });
      }
    }

    if (currentFrame) {
      for (const event of relationships.targetEventsByFrameId.get(
        currentFrame.id,
      ) ?? []) {
        if (event.sourceKind !== "ability" || !visibleIds.has(event.sourceId))
          continue;
        for (const target of event.targets) {
          if (target.playerSide) targetedPlayerSides.add(target.playerSide);
          connections.push({
            kind: "abilityTarget",
            sourceId: event.sourceId,
            targetId: target.connectionId,
          });
        }
      }

      for (const event of relationships.damageEventsByFrameId.get(
        currentFrame.id,
      ) ?? []) {
        if (event.target.playerSide)
          targetedPlayerSides.add(event.target.playerSide);
        connections.push({
          kind: "damage",
          sourceId: event.sourceId,
          targetId: event.target.connectionId,
        });
      }

      for (const event of relationships.triggerEventsByFrameId.get(
        currentFrame.id,
      ) ?? []) {
        connections.push({
          kind: "trigger",
          sourceId: event.triggeringId,
          targetId: event.sourceId,
        });
      }

      for (const event of buildReplayAttachmentState(
        frames,
        safeSelectedFrameIndex,
        relationships,
      )) {
        if (
          !visibleIds.has(event.attachmentId) ||
          !visibleIds.has(event.hostId)
        )
          continue;
        connections.push({
          kind: "attachment",
          sourceId: event.attachmentId,
          targetId: event.hostId,
          hiddenUnlessFocused: true,
        });
        addLabel(event.attachmentId, `Attached to ${event.hostLabel}`);
        addLabel(event.hostId, `Attached: ${event.attachmentLabel}`);
      }

      for (const event of replayFrameCrewEvents(currentFrame, relationships)) {
        for (const crewId of event.crewIds) {
          connections.push({
            kind: "crew",
            sourceId: crewId,
            targetId: event.vehicleId,
          });
        }
      }
    }

    for (const attacker of currentObjects) {
      if (
        !replayObjectIsAttacking(attacker) ||
        typeof attacker.attackTargetId !== "number"
      ) {
        continue;
      }
      const target = replayRelationshipTargetForId(
        relationships,
        attacker.attackTargetId,
      );
      connections.push({
        kind: "attackTarget",
        sourceId: attacker.instanceId,
        targetId: target.connectionId,
        hiddenUnlessFocused: Boolean(target.playerSide),
      });
      if (target.playerSide) {
        const summary = attackSummaries.get(target.playerSide) ?? {
          attackers: 0,
          power: 0,
        };
        summary.attackers += 1;
        summary.power += attacker.power ?? 0;
        attackSummaries.set(target.playerSide, summary);
      }
    }

    return {
      connections,
      relationshipLabelsByObjectId,
      targetedPlayerSides,
      attackSummaries,
    };
  }, [
    currentFrame,
    currentObjects,
    frames,
    relationships,
    safeSelectedFrameIndex,
    targetsBySourceId,
  ]);
  const overlayConnections = useMemo(
    () => [...combatConnections, ...relationshipPresentation.connections],
    [combatConnections, relationshipPresentation.connections],
  );
  const linkedExileObjectsByParentId = useMemo(() => {
    const currentObjectsById = new Map<number, MatchReplayFrameObject>();
    for (const object of currentObjects) {
      currentObjectsById.set(object.instanceId, object);
    }

    const linkedIdsByParentId = new Map<number, Set<number>>();
    for (
      let frameIndex = 0;
      frameIndex <= safeSelectedFrameIndex;
      frameIndex += 1
    ) {
      const frame = frames[frameIndex] ?? null;
      for (const annotation of replayFrameAnnotations(frame)) {
        if (
          !replayAnnotationHasType(
            annotation,
            "AnnotationType_DisplayCardUnderCard",
          )
        ) {
          continue;
        }
        if (typeof annotation.affectorId !== "number") {
          continue;
        }
        const affectedIds = Array.isArray(annotation.affectedIds)
          ? annotation.affectedIds.filter(
              (value): value is number => typeof value === "number",
            )
          : [];
        if (affectedIds.length === 0) {
          continue;
        }

        const isDisabled =
          replayAnnotationDetailIntValue(annotation, "Disable") === 1;
        if (isDisabled) {
          const existing = linkedIdsByParentId.get(annotation.affectorId);
          if (!existing) {
            continue;
          }
          for (const affectedId of affectedIds) {
            existing.delete(affectedId);
          }
          if (existing.size === 0) {
            linkedIdsByParentId.delete(annotation.affectorId);
          }
          continue;
        }

        let nextLinkedIds = linkedIdsByParentId.get(annotation.affectorId);
        if (!nextLinkedIds) {
          nextLinkedIds = new Set<number>();
          linkedIdsByParentId.set(annotation.affectorId, nextLinkedIds);
        }
        for (const affectedId of affectedIds) {
          nextLinkedIds.add(affectedId);
        }
      }
    }

    const next = new Map<number, MatchReplayFrameObject[]>();
    for (const [parentId, linkedIds] of linkedIdsByParentId) {
      const parentObject = currentObjectsById.get(parentId);
      if (
        !parentObject ||
        boardZoneKind(parentObject.zoneType) !== "battlefield"
      ) {
        continue;
      }

      const linkedObjects = [...linkedIds]
        .map((linkedId) => currentObjectsById.get(linkedId) ?? null)
        .filter(
          (linkedObject): linkedObject is MatchReplayFrameObject =>
            linkedObject !== null &&
            boardZoneKind(linkedObject.zoneType) === "exile",
        )
        .sort(sortReplayObjects);
      if (linkedObjects.length > 0) {
        next.set(parentId, linkedObjects);
      }
    }

    return next;
  }, [currentObjects, frames, safeSelectedFrameIndex]);
  const overlayInteractiveInstanceIDs = useMemo(() => {
    const ids = new Set<number>();
    for (const connection of overlayConnections) {
      ids.add(connection.sourceId);
      ids.add(connection.targetId);
    }
    return ids;
  }, [overlayConnections]);
  const overlayHighlightedInstanceIDs = useMemo(() => {
    if (focusedConnectionInstanceId === null) {
      return new Set<number>();
    }

    const ids = new Set<number>([focusedConnectionInstanceId]);
    for (const connection of overlayConnections) {
      if (
        connection.sourceId === focusedConnectionInstanceId ||
        connection.targetId === focusedConnectionInstanceId
      ) {
        ids.add(connection.sourceId);
        ids.add(connection.targetId);
      }
    }
    return ids;
  }, [overlayConnections, focusedConnectionInstanceId]);
  const currentFrameChanges = currentFrame?.changes ?? [];
  const changedInstanceIDs = new Set(
    currentFrameChanges.map((change) => change.instanceId),
  );
  const currentBeat = currentFrame
    ? buildReplayBeat(currentFrame, previousFrame, relationships)
    : { text: "" };
  const canStepBackward = safeSelectedFrameIndex > 0;
  const canStepForward = safeSelectedFrameIndex < lastFrameIndex;
  const canJumpPrevTurn = currentTurnBoundaryIndex > 0;
  const canJumpNextTurn =
    currentTurnBoundaryIndex >= 0 &&
    currentTurnBoundaryIndex < turnBoundaries.length - 1;

  const goToFirstStep = () => {
    setIsPlaying(false);
    setSelectedFrameIndex(0);
  };
  const goToLastStep = () => {
    setIsPlaying(false);
    setSelectedFrameIndex(lastFrameIndex);
  };
  const goToPrevStep = () => {
    setIsPlaying(false);
    setSelectedFrameIndex(Math.max(safeSelectedFrameIndex - 1, 0));
  };
  const goToNextStep = () => {
    setIsPlaying(false);
    setSelectedFrameIndex(Math.min(safeSelectedFrameIndex + 1, lastFrameIndex));
  };
  const goToPrevTurn = () => {
    setIsPlaying(false);
    setSelectedFrameIndex(
      turnBoundaries[currentTurnBoundaryIndex - 1]?.firstIndex ?? 0,
    );
  };
  const goToNextTurn = () => {
    setIsPlaying(false);
    setSelectedFrameIndex(
      turnBoundaries[currentTurnBoundaryIndex + 1]?.firstIndex ??
        frames.length - 1,
    );
  };
  const togglePlay = () => setIsPlaying((currentValue) => !currentValue);

  useReplayKeyboard({
    onStepBackward: goToPrevStep,
    onStepForward: goToNextStep,
    onPrevTurn: goToPrevTurn,
    onNextTurn: goToNextTurn,
    onTogglePlay: togglePlay,
    onFirst: goToFirstStep,
    onLast: goToLastStep,
  });

  useEffect(() => {
    setFocusedConnectionInstanceId(null);
  }, [currentFrame?.id]);

  function registerCardShell(instanceId: number, element: HTMLElement | null) {
    if (element) {
      replayCardShellsRef.current.set(instanceId, element);
      return;
    }
    replayCardShellsRef.current.delete(instanceId);
  }

  if (!currentFrame) {
    return (
      <article className="panel inner match-replay-game">
        <div className="match-replay-head">
          <div className="match-replay-head-copy">
            <h4>Game {gameNumber}</h4>
            {gameSummary ? (
              <div className="match-replay-result">
                <ResultPill result={gameSummary.result} />
                <p className="match-replay-result-copy">{gameSummary.detail}</p>
              </div>
            ) : null}
          </div>
          <p className="match-replay-kicker">Replay</p>
        </div>
        <StatusMessage>No replay steps for this game.</StatusMessage>
      </article>
    );
  }

  return (
    <article className="panel inner match-replay-game">
      <div className="match-replay-command-deck">
        <div className="match-replay-head">
          <div className="match-replay-head-copy">
            <h4>Game {gameNumber}</h4>
            {gameSummary ? (
              <div className="match-replay-result">
                <ResultPill result={gameSummary.result} />
                <p className="match-replay-result-copy">{gameSummary.detail}</p>
              </div>
            ) : null}
          </div>
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
              onClick={goToPrevTurn}
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
              onClick={goToPrevStep}
              disabled={!canStepBackward}
              aria-label="Previous step"
            >
              <span>Prev Step</span>
              <kbd className="match-replay-button-shortcut" aria-hidden="true">
                ←
              </kbd>
            </button>
            <button
              type="button"
              className="match-replay-button"
              onClick={goToNextStep}
              disabled={!canStepForward}
            >
              <span>Next Step</span>
              <kbd className="match-replay-button-shortcut" aria-hidden="true">
                →
              </kbd>
            </button>
            <button
              type="button"
              className="match-replay-button"
              onClick={goToNextTurn}
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
            length={frames.length}
            index={safeSelectedFrameIndex}
            onSeek={(nextIndex) => {
              setIsPlaying(false);
              setSelectedFrameIndex(nextIndex);
            }}
            turnBoundaries={turnBoundaries}
            itemLabel="step"
            lifeSeries={lifeSeries}
            tickKinds={tickKinds}
            keyMoments={keyMoments}
            snapshotForIndex={scrubberSnapshotForIndex}
          />
        </div>
      </div>

      <MatchReplayHud
        currentFrame={currentFrame}
        previousFrame={previousFrame}
        stepNumber={safeSelectedFrameIndex + 1}
        stepCount={frames.length}
        beat={currentBeat}
      />

      <div
        className={`match-replay-canvas is-arena ${
          moveListCollapsed ? "is-movelist-collapsed" : ""
        }`}
      >
        <div className="match-replay-arena" ref={canvasRef}>
          <MatchReplayConnectionOverlay
            surfaceRef={canvasRef}
            cardShellsRef={replayCardShellsRef}
            connections={overlayConnections}
            focusedInstanceId={focusedConnectionInstanceId}
          />

          <div className="match-replay-arena-top">
            <MatchReplayFrameSideSummary
              side="opponent"
              objects={currentObjects}
              variant="rail"
              onOpenZone={setZoneDialogState}
              endpointId={replayPlayerConnectionId("opponent")}
              onRegisterEndpoint={registerCardShell}
              attackSummary={
                relationshipPresentation.attackSummaries.get("opponent") ?? null
              }
              relationshipTargeted={relationshipPresentation.targetedPlayerSides.has(
                "opponent",
              )}
            />
            <div className="match-replay-arena-stack">
              <MatchReplayStack
                frame={currentFrame}
                targetsBySourceId={targetsBySourceId}
                previewByCardID={previewByCardID}
                highlightedInstanceIDs={changedInstanceIDs}
                onRegisterCardShell={registerCardShell}
                connectionHighlightedInstanceIDs={overlayHighlightedInstanceIDs}
                connectionInteractiveInstanceIDs={overlayInteractiveInstanceIDs}
                onConnectionFocusChange={setFocusedConnectionInstanceId}
              />
            </div>
          </div>

          <MatchReplayFrameBattlefield
            side="opponent"
            objects={currentObjects}
            previewByCardID={previewByCardID}
            highlightedInstanceIDs={changedInstanceIDs}
            onRegisterCardShell={registerCardShell}
            connectionHighlightedInstanceIDs={overlayHighlightedInstanceIDs}
            connectionInteractiveInstanceIDs={overlayInteractiveInstanceIDs}
            onConnectionFocusChange={setFocusedConnectionInstanceId}
            linkedExileObjectsByParentId={linkedExileObjectsByParentId}
            relationshipLabelsByObjectId={
              relationshipPresentation.relationshipLabelsByObjectId
            }
          />

          <MatchReplayFrameBattlefield
            side="self"
            objects={currentObjects}
            previewByCardID={previewByCardID}
            highlightedInstanceIDs={changedInstanceIDs}
            onRegisterCardShell={registerCardShell}
            connectionHighlightedInstanceIDs={overlayHighlightedInstanceIDs}
            connectionInteractiveInstanceIDs={overlayInteractiveInstanceIDs}
            onConnectionFocusChange={setFocusedConnectionInstanceId}
            linkedExileObjectsByParentId={linkedExileObjectsByParentId}
            relationshipLabelsByObjectId={
              relationshipPresentation.relationshipLabelsByObjectId
            }
          />

          <MatchReplayFrameSideSummary
            side="self"
            objects={currentObjects}
            variant="rail"
            onOpenZone={setZoneDialogState}
            endpointId={replayPlayerConnectionId("self")}
            onRegisterEndpoint={registerCardShell}
            attackSummary={
              relationshipPresentation.attackSummaries.get("self") ?? null
            }
            relationshipTargeted={relationshipPresentation.targetedPlayerSides.has(
              "self",
            )}
          />

          <MatchReplayHand
            objects={currentObjects}
            previewByCardID={previewByCardID}
            highlightedInstanceIDs={changedInstanceIDs}
          />
        </div>

        <MatchReplayMoveList
          frames={frames}
          relationships={relationships}
          turnBoundaries={turnBoundaries}
          currentIndex={safeSelectedFrameIndex}
          collapsed={moveListCollapsed}
          onToggleCollapsed={() => setMoveListCollapsed((value) => !value)}
          onSeek={(nextIndex) => {
            setIsPlaying(false);
            setSelectedFrameIndex(nextIndex);
          }}
        />
      </div>

      <MatchReplayZoneDialog
        state={zoneDialogState}
        previewByCardID={previewByCardID}
        onClose={() => setZoneDialogState(null)}
      />
    </article>
  );
}
