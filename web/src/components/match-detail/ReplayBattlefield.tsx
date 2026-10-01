import { useMemo, type CSSProperties, type ReactNode } from "react";
import {
  battlefieldSectionKind,
  battlefieldSectionLabel,
  battlefieldSectionOrder,
  boardZoneKind,
  boardZoneLabel,
  cardDisplayName,
  cardFallbackHref,
  groupBattlefieldCardStacks,
  replayObjectIsAttacking,
  replayObjectIsBlocking,
  replayObjectName,
  replayTargetListLabel,
  shouldRenderOnBattlefield,
  sortBattlefieldSectionObjects,
  sortReplayObjects,
  summarizeReplayFrameZones,
  summarizeReplayZones,
  timelinePlayerLabel,
  type BattlefieldSectionKind,
  type BoardZoneKind,
  type ReplayTargetLookup,
} from "../../lib/replay";
import type { CardPreview } from "../../lib/scryfall";
import type {
  MatchCardPlay,
  MatchReplayFrame,
  MatchReplayFrameObject,
} from "../../lib/types";
import {
  ManaCostDisplay,
  MatchReplayCard,
  MatchReplayObjectCard,
  ReplayCardPreviewAnchor,
  SummoningSicknessGlyph,
} from "./CardPreviews";

function CompactBattlefieldRows<T extends { kind: BattlefieldSectionKind }>({
  side,
  sections,
  renderSection,
}: {
  side: "self" | "opponent";
  sections: T[];
  renderSection: (section: T) => ReactNode;
}) {
  const creatures = sections.find((section) => section.kind === "creatures");
  const lands = sections.find((section) => section.kind === "lands");
  const utilitySections = sections.filter(
    (section) => section.kind !== "creatures" && section.kind !== "lands",
  );

  const creatureRow = creatures ? (
    <div className="match-replay-battlefield-row is-creature-row">
      {renderSection(creatures)}
    </div>
  ) : null;
  const backlineRow =
    lands || utilitySections.length > 0 ? (
      <div className="match-replay-battlefield-row is-backline-row">
        {lands ? renderSection(lands) : null}
        {utilitySections.length > 0 ? (
          <div
            className="match-replay-utility-groups"
            role="group"
            aria-label={`${timelinePlayerLabel(side)} utility permanents`}
          >
            {utilitySections.map(renderSection)}
          </div>
        ) : null}
      </div>
    ) : null;

  return (
    <div className="match-replay-zone-groups">
      {side === "opponent" ? (
        <>
          {backlineRow}
          {creatureRow}
        </>
      ) : (
        <>
          {creatureRow}
          {backlineRow}
        </>
      )}
    </div>
  );
}

export function MatchReplayFrameBattlefield({
  side,
  objects,
  previewByCardID,
  highlightedInstanceIDs,
  onRegisterCardShell,
  connectionHighlightedInstanceIDs,
  connectionInteractiveInstanceIDs,
  onConnectionFocusChange,
  linkedExileObjectsByParentId,
  relationshipLabelsByObjectId,
}: {
  side: "self" | "opponent";
  objects: MatchReplayFrameObject[];
  previewByCardID: Map<number, CardPreview | null>;
  highlightedInstanceIDs: Set<number>;
  onRegisterCardShell?: (
    instanceId: number,
    element: HTMLDivElement | null,
  ) => void;
  connectionHighlightedInstanceIDs?: Set<number>;
  connectionInteractiveInstanceIDs?: Set<number>;
  onConnectionFocusChange?: (instanceId: number | null) => void;
  linkedExileObjectsByParentId?: Map<number, MatchReplayFrameObject[]>;
  relationshipLabelsByObjectId?: Map<number, string[]>;
}) {
  const sideObjects = useMemo(
    () => objects.filter((object) => object.playerSide === side),
    [objects, side],
  );
  const battlefieldObjects = useMemo(
    () =>
      sideObjects
        .filter((object) => boardZoneKind(object.zoneType) === "battlefield")
        .sort(sortReplayObjects),
    [sideObjects],
  );
  const battlefieldSections = useMemo(() => {
    const sectionOrder = battlefieldSectionOrder(side);
    const grouped = new Map<BattlefieldSectionKind, MatchReplayFrameObject[]>();
    for (const kind of sectionOrder) {
      grouped.set(kind, []);
    }

    for (const object of battlefieldObjects) {
      const preview = previewByCardID.get(object.cardId) ?? null;
      grouped.get(battlefieldSectionKind(preview, object))?.push(object);
    }

    return sectionOrder
      .map((kind) => ({
        kind,
        label: battlefieldSectionLabel(kind),
        objects: sortBattlefieldSectionObjects(
          kind,
          grouped.get(kind) ?? [],
          previewByCardID,
        ),
      }))
      .filter((section) => section.objects.length > 0);
  }, [battlefieldObjects, previewByCardID, side]);
  const zoneCounts = useMemo(
    () => summarizeReplayFrameZones(sideObjects),
    [sideObjects],
  );
  const tappedCount = battlefieldObjects.filter(
    (object) => object.isTapped,
  ).length;
  const attackingCount = battlefieldObjects.filter(
    replayObjectIsAttacking,
  ).length;
  const blockingCount = battlefieldObjects.filter(
    replayObjectIsBlocking,
  ).length;
  const sideBadges = (
    ["graveyard", "exile", "revealed"] as BoardZoneKind[]
  ).filter((kind) => (zoneCounts.get(kind) ?? 0) > 0);

  return (
    <section
      className={`match-replay-lane is-${side}`}
      aria-label={`${timelinePlayerLabel(side)} battlefield`}
    >
      <div className="match-replay-lane-head">
        <div>
          <p className="match-replay-lane-title">
            {timelinePlayerLabel(side)} Battlefield
          </p>
          <p className="match-replay-lane-subtitle">
            {battlefieldObjects.length} current on board
            {tappedCount > 0 ? ` • ${tappedCount} tapped` : ""}
            {attackingCount > 0 ? ` • ${attackingCount} attacking` : ""}
            {blockingCount > 0 ? ` • ${blockingCount} blocking` : ""}
            {sideBadges.length > 0
              ? ` • ${sideBadges.map((kind) => `${zoneCounts.get(kind)} ${boardZoneLabel(kind).toLowerCase()}`).join(" • ")}`
              : ""}
          </p>
        </div>
      </div>
      {battlefieldObjects.length === 0 ? (
        <p className="match-replay-empty">
          No battlefield cards in this frame.
        </p>
      ) : (
        <CompactBattlefieldRows
          side={side}
          sections={battlefieldSections}
          renderSection={(section) => {
            const summoningSickCount = section.objects.filter(
              (object) => object.hasSummoningSickness,
            ).length;

            return (
              <section
                key={section.kind}
                className={`match-replay-zone-group is-${section.kind}`}
                aria-label={`${timelinePlayerLabel(side)} ${section.label.toLowerCase()}`}
              >
                <div className="match-replay-zone-group-head">
                  <p className="match-replay-zone-group-title">
                    {section.label}
                  </p>
                  <p className="match-replay-zone-group-count">
                    {section.objects.length}
                  </p>
                  {summoningSickCount > 0 ? (
                    <span
                      className="match-replay-zone-group-summoning-sick"
                      title={`${summoningSickCount} summoning sick`}
                    >
                      <SummoningSicknessGlyph />
                      {summoningSickCount === section.objects.length
                        ? "All summoning sick"
                        : `${summoningSickCount} summoning sick`}
                    </span>
                  ) : null}
                </div>
                {section.kind === "lands" ||
                section.kind === "artifacts_enchantments" ? (
                  <MatchReplayCardStackRow
                    objects={section.objects}
                    previewByCardID={previewByCardID}
                    highlightedInstanceIDs={highlightedInstanceIDs}
                    onRegisterCardShell={onRegisterCardShell}
                    connectionHighlightedInstanceIDs={
                      connectionHighlightedInstanceIDs
                    }
                    connectionInteractiveInstanceIDs={
                      connectionInteractiveInstanceIDs
                    }
                    onConnectionFocusChange={onConnectionFocusChange}
                    linkedExileObjectsByParentId={linkedExileObjectsByParentId}
                    relationshipLabelsByObjectId={relationshipLabelsByObjectId}
                    stackEligible={
                      // Lands stack whenever identical; other permanents only
                      // consolidate duplicate tokens (e.g. a row of Mutagens).
                      section.kind === "lands" ? undefined : stackTokensOnly
                    }
                  />
                ) : (
                  <div className="match-replay-card-row is-sectioned">
                    {section.objects.map((object) => (
                      <MatchReplayObjectCard
                        key={object.instanceId}
                        object={object}
                        preview={previewByCardID.get(object.cardId) ?? null}
                        previewByCardID={previewByCardID}
                        active={highlightedInstanceIDs.has(object.instanceId)}
                        shellRef={
                          onRegisterCardShell
                            ? (element) =>
                                onRegisterCardShell(object.instanceId, element)
                            : undefined
                        }
                        connectionHighlighted={
                          connectionHighlightedInstanceIDs?.has(
                            object.instanceId,
                          ) ?? false
                        }
                        onConnectionFocusChange={
                          connectionInteractiveInstanceIDs?.has(
                            object.instanceId,
                          )
                            ? onConnectionFocusChange
                            : undefined
                        }
                        linkedExileObjects={
                          linkedExileObjectsByParentId?.get(
                            object.instanceId,
                          ) ?? []
                        }
                        relationshipLabels={
                          relationshipLabelsByObjectId?.get(
                            object.instanceId,
                          ) ?? []
                        }
                      />
                    ))}
                  </div>
                )}
              </section>
            );
          }}
        />
      )}
    </section>
  );
}

const stackTokensOnly = (object: MatchReplayFrameObject) => object.isToken;

function MatchReplayCardStackRow({
  objects,
  previewByCardID,
  highlightedInstanceIDs,
  onRegisterCardShell,
  connectionHighlightedInstanceIDs,
  connectionInteractiveInstanceIDs,
  onConnectionFocusChange,
  linkedExileObjectsByParentId,
  relationshipLabelsByObjectId,
  stackEligible,
}: {
  objects: MatchReplayFrameObject[];
  previewByCardID: Map<number, CardPreview | null>;
  highlightedInstanceIDs: Set<number>;
  onRegisterCardShell?: (
    instanceId: number,
    element: HTMLDivElement | null,
  ) => void;
  connectionHighlightedInstanceIDs?: Set<number>;
  connectionInteractiveInstanceIDs?: Set<number>;
  onConnectionFocusChange?: (instanceId: number | null) => void;
  linkedExileObjectsByParentId?: Map<number, MatchReplayFrameObject[]>;
  relationshipLabelsByObjectId?: Map<number, string[]>;
  stackEligible?: (object: MatchReplayFrameObject) => boolean;
}) {
  const stacks = useMemo(
    () =>
      groupBattlefieldCardStacks(
        objects,
        previewByCardID,
        (object) =>
          (linkedExileObjectsByParentId?.get(object.instanceId) ?? [])
            .length === 0 &&
          (stackEligible?.(object) ?? true),
      ),
    [objects, previewByCardID, linkedExileObjectsByParentId, stackEligible],
  );

  const renderObjectCard = (object: MatchReplayFrameObject) => (
    <MatchReplayObjectCard
      key={object.instanceId}
      object={object}
      preview={previewByCardID.get(object.cardId) ?? null}
      previewByCardID={previewByCardID}
      active={highlightedInstanceIDs.has(object.instanceId)}
      shellRef={
        onRegisterCardShell
          ? (element) => onRegisterCardShell(object.instanceId, element)
          : undefined
      }
      connectionHighlighted={
        connectionHighlightedInstanceIDs?.has(object.instanceId) ?? false
      }
      onConnectionFocusChange={
        connectionInteractiveInstanceIDs?.has(object.instanceId)
          ? onConnectionFocusChange
          : undefined
      }
      linkedExileObjects={
        linkedExileObjectsByParentId?.get(object.instanceId) ?? []
      }
      relationshipLabels={
        relationshipLabelsByObjectId?.get(object.instanceId) ?? []
      }
    />
  );

  return (
    <div className="match-replay-card-row is-sectioned is-lands">
      {stacks.map((stack) => {
        if (stack.objects.length === 1) {
          return renderObjectCard(stack.objects[0]);
        }

        // Cards involved in the current step (or a hovered connection) move to
        // the front of the pile so their highlight stays visible.
        const isFront = (object: MatchReplayFrameObject) =>
          highlightedInstanceIDs.has(object.instanceId) ||
          (connectionHighlightedInstanceIDs?.has(object.instanceId) ?? false);
        const ordered = [...stack.objects].sort(
          (a, b) => Number(isFront(b)) - Number(isFront(a)),
        );
        const front = ordered[0];
        const frontName = replayObjectName(
          front,
          previewByCardID.get(front.cardId) ?? null,
        );

        return (
          <div
            key={stack.key}
            className={`match-replay-land-stack ${front.isTapped ? "is-tapped" : ""}`}
            style={{ "--land-stack-size": ordered.length } as CSSProperties}
            role="group"
            aria-label={`${frontName} ×${ordered.length}${front.isTapped ? " tapped" : ""}`}
          >
            {ordered.map((object, index) => (
              <div
                key={object.instanceId}
                className="match-replay-land-stack-item"
                style={{ "--land-stack-index": index } as CSSProperties}
              >
                {renderObjectCard(object)}
              </div>
            ))}
          </div>
        );
      })}
    </div>
  );
}

export function MatchReplayHand({
  objects,
  previewByCardID,
  highlightedInstanceIDs,
}: {
  objects: MatchReplayFrameObject[];
  previewByCardID: Map<number, CardPreview | null>;
  highlightedInstanceIDs: Set<number>;
}) {
  const handObjects = useMemo(
    () =>
      objects
        .filter(
          (object) =>
            object.playerSide === "self" &&
            boardZoneKind(object.zoneType) === "hand",
        )
        .sort(sortReplayObjects),
    [objects],
  );

  return (
    <section className="match-replay-lane is-hand" aria-label="Your hand">
      <div className="match-replay-lane-head">
        <div>
          <p className="match-replay-lane-title">Your Hand</p>
          <p className="match-replay-lane-subtitle">
            {handObjects.length} card{handObjects.length === 1 ? "" : "s"}{" "}
            currently in hand
          </p>
        </div>
      </div>
      {handObjects.length === 0 ? (
        <p className="match-replay-empty">No cards in hand in this step.</p>
      ) : (
        <div
          className="match-replay-card-row is-hand"
          aria-label="Current hand"
        >
          {handObjects.map((object) => (
            <MatchReplayObjectCard
              key={object.instanceId}
              object={object}
              preview={previewByCardID.get(object.cardId) ?? null}
              active={highlightedInstanceIDs.has(object.instanceId)}
              size="hand"
            />
          ))}
        </div>
      )}
    </section>
  );
}

export function MatchReplayStack({
  frame,
  targetsBySourceId,
  previewByCardID,
  highlightedInstanceIDs,
  onRegisterCardShell,
  connectionHighlightedInstanceIDs,
  connectionInteractiveInstanceIDs,
  onConnectionFocusChange,
}: {
  frame: MatchReplayFrame;
  targetsBySourceId: ReplayTargetLookup;
  previewByCardID: Map<number, CardPreview | null>;
  highlightedInstanceIDs: Set<number>;
  onRegisterCardShell?: (
    instanceId: number,
    element: HTMLDivElement | null,
  ) => void;
  connectionHighlightedInstanceIDs?: Set<number>;
  connectionInteractiveInstanceIDs?: Set<number>;
  onConnectionFocusChange?: (instanceId: number | null) => void;
}) {
  // Stack zone positions count down from the top: position 1 is the spell
  // that resolves next, so ascending order is already top-first.
  const stackObjects = useMemo(
    () =>
      [...(frame.objects ?? [])]
        .filter((object) => boardZoneKind(object.zoneType) === "stack")
        .sort(sortReplayObjects),
    [frame],
  );

  if (stackObjects.length === 0) {
    return null;
  }

  return (
    <section className="match-replay-stackstrip" aria-label="Current stack">
      <p className="match-replay-stackstrip-label">
        Stack
        {stackObjects.length > 1 ? ` · ${stackObjects.length}` : ""}
      </p>
      <ol
        className="match-replay-stack-tickets"
        aria-label="Current stack ordered top to bottom"
      >
        {stackObjects.map((object, index) => {
          const preview = previewByCardID.get(object.cardId) ?? null;
          const card = { cardId: object.cardId, cardName: object.cardName };
          const name = preview?.name ?? cardDisplayName(card);
          const isTop = index === 0;
          const manaCost = preview?.manaCost?.trim() ?? "";
          const targets = targetsBySourceId.get(object.instanceId) ?? [];
          const targetLabel = replayTargetListLabel(targets);
          const connectionFocusable =
            connectionInteractiveInstanceIDs?.has(object.instanceId) &&
            onConnectionFocusChange;

          return (
            <li
              className={`match-replay-stack-ticket is-${object.playerSide} ${isTop ? "is-top" : ""} ${
                highlightedInstanceIDs.has(object.instanceId) ? "is-active" : ""
              } ${
                connectionHighlightedInstanceIDs?.has(object.instanceId)
                  ? "is-connection-highlighted"
                  : ""
              }`}
              style={{ "--stack-depth": index } as CSSProperties}
              key={object.instanceId}
              onMouseEnter={
                connectionFocusable
                  ? () => onConnectionFocusChange(object.instanceId)
                  : undefined
              }
              onMouseLeave={
                connectionFocusable
                  ? () => onConnectionFocusChange(null)
                  : undefined
              }
              onFocus={
                connectionFocusable
                  ? () => onConnectionFocusChange(object.instanceId)
                  : undefined
              }
              onBlur={
                connectionFocusable
                  ? () => onConnectionFocusChange(null)
                  : undefined
              }
            >
              <div
                className="match-replay-stack-ticket-shell"
                ref={
                  onRegisterCardShell
                    ? (element) =>
                        onRegisterCardShell(object.instanceId, element)
                    : undefined
                }
              >
                <ReplayCardPreviewAnchor preview={preview}>
                  <a
                    className="match-replay-stack-ticket-link"
                    href={preview?.scryfallUrl ?? cardFallbackHref(card)}
                    target="_blank"
                    rel="noreferrer"
                    aria-label={`Open ${name} on Scryfall${targetLabel ? `, targeting ${targetLabel}` : ""}`}
                    title={`${name} • ${timelinePlayerLabel(object.playerSide)}${isTop ? " • Top of stack" : ""}${targetLabel ? ` • Targets ${targetLabel}` : ""}`}
                  >
                    <span
                      className="match-replay-stack-ticket-edge"
                      aria-hidden="true"
                    />
                    {preview?.artCropUrl ? (
                      <img
                        className="match-replay-stack-ticket-art"
                        src={preview.artCropUrl}
                        alt=""
                        loading="eager"
                        decoding="async"
                        width={48}
                        height={34}
                      />
                    ) : null}
                    <span className="match-replay-stack-ticket-name">
                      {name}
                    </span>
                    {manaCost ? <ManaCostDisplay manaCost={manaCost} /> : null}
                    <span className="match-replay-stack-ticket-owner">
                      {object.playerSide === "self" ? "You" : "Opp"}
                    </span>
                    {targetLabel ? (
                      <span
                        className="match-replay-stack-ticket-target"
                        title={`Targets ${targetLabel}`}
                      >
                        → {targetLabel}
                      </span>
                    ) : null}
                    {isTop ? (
                      <span className="match-replay-stack-ticket-tag">Top</span>
                    ) : (
                      <span className="match-replay-stack-ticket-index">
                        {index + 1}
                      </span>
                    )}
                  </a>
                </ReplayCardPreviewAnchor>
              </div>
            </li>
          );
        })}
      </ol>
    </section>
  );
}

export function MatchReplayBattlefield({
  side,
  plays,
  activePlayID,
  previewByCardID,
}: {
  side: "self" | "opponent";
  plays: MatchCardPlay[];
  activePlayID: number;
  previewByCardID: Map<number, CardPreview | null>;
}) {
  const battlefieldPlays = useMemo(
    () =>
      plays.filter((play) =>
        shouldRenderOnBattlefield(
          play,
          previewByCardID.get(play.cardId) ?? null,
          activePlayID,
        ),
      ),
    [activePlayID, plays, previewByCardID],
  );
  const battlefieldSections = useMemo(() => {
    const sectionOrder = battlefieldSectionOrder(side);
    const grouped = new Map<BattlefieldSectionKind, MatchCardPlay[]>();
    for (const kind of sectionOrder) {
      grouped.set(kind, []);
    }

    for (const play of battlefieldPlays) {
      const preview = previewByCardID.get(play.cardId) ?? null;
      grouped.get(battlefieldSectionKind(preview))?.push(play);
    }

    return sectionOrder
      .map((kind) => ({
        kind,
        label: battlefieldSectionLabel(kind),
        plays: grouped.get(kind) ?? [],
      }))
      .filter((section) => section.plays.length > 0);
  }, [battlefieldPlays, previewByCardID, side]);
  const zoneCounts = useMemo(() => summarizeReplayZones(plays), [plays]);
  const sideBadges = (
    ["graveyard", "exile", "revealed"] as BoardZoneKind[]
  ).filter((kind) => (zoneCounts.get(kind) ?? 0) > 0);

  return (
    <section
      className={`match-replay-lane is-${side}`}
      aria-label={`${timelinePlayerLabel(side)} battlefield`}
    >
      <div className="match-replay-lane-head">
        <div>
          <p className="match-replay-lane-title">
            {timelinePlayerLabel(side)} Battlefield
          </p>
          <p className="match-replay-lane-subtitle">
            {battlefieldPlays.length} observed on board
            {sideBadges.length > 0
              ? ` • ${sideBadges.map((kind) => `${zoneCounts.get(kind)} ${boardZoneLabel(kind).toLowerCase()}`).join(" • ")}`
              : ""}
          </p>
        </div>
      </div>
      {battlefieldPlays.length === 0 ? (
        <p className="match-replay-empty">No battlefield cards observed yet.</p>
      ) : (
        <CompactBattlefieldRows
          side={side}
          sections={battlefieldSections}
          renderSection={(section) => (
            <section
              key={section.kind}
              className={`match-replay-zone-group is-${section.kind}`}
              aria-label={`${timelinePlayerLabel(side)} ${section.label.toLowerCase()}`}
            >
              <div className="match-replay-zone-group-head">
                <p className="match-replay-zone-group-title">{section.label}</p>
                <p className="match-replay-zone-group-count">
                  {section.plays.length}
                </p>
              </div>
              <div className="match-replay-card-row is-sectioned">
                {section.plays.map((play) => (
                  <MatchReplayCard
                    key={play.id}
                    play={play}
                    preview={previewByCardID.get(play.cardId) ?? null}
                    active={play.id === activePlayID}
                  />
                ))}
              </div>
            </section>
          )}
        />
      )}
    </section>
  );
}
