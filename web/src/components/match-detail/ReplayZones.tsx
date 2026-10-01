import { useEffect, useId, useMemo, useRef } from "react";
import { createPortal } from "react-dom";
import magicCardbackURL from "../../assets/magic-cardback.jpg";
import {
  boardZoneKind,
  boardZoneLabel,
  isInspectableZoneKind,
  sortReplayObjects,
  summarizeReplayFrameZones,
  summarizeReplayZones,
  timelinePlayerLabel,
  type BoardZoneKind,
} from "../../lib/replay";
import type { CardPreview } from "../../lib/scryfall";
import type { MatchCardPlay, MatchReplayFrameObject } from "../../lib/types";
import { MatchReplayCard, MatchReplayObjectCard } from "./CardPreviews";
import type { MatchReplayZoneDialogState } from "./types";

export function MatchReplayZoneDialog({
  state,
  previewByCardID,
  onClose,
}: {
  state: MatchReplayZoneDialogState | null;
  previewByCardID: Map<number, CardPreview | null>;
  onClose: () => void;
}) {
  const titleId = useId();
  const descriptionId = useId();
  const dialogRef = useRef<HTMLDivElement | null>(null);
  const closeButtonRef = useRef<HTMLButtonElement | null>(null);
  const previousFocusRef = useRef<HTMLElement | null>(null);

  useEffect(() => {
    if (!state) {
      return;
    }

    previousFocusRef.current =
      document.activeElement instanceof HTMLElement
        ? document.activeElement
        : null;
    const originalOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";

    const focusFrameID = window.requestAnimationFrame(() => {
      closeButtonRef.current?.focus();
    });

    const handleKeyDown = (event: globalThis.KeyboardEvent) => {
      if (!dialogRef.current) {
        return;
      }

      if (event.key === "Escape") {
        event.preventDefault();
        onClose();
        return;
      }

      if (event.key !== "Tab") {
        return;
      }

      const focusable = Array.from(
        dialogRef.current.querySelectorAll<HTMLElement>(
          'button:not([disabled]), a[href], [tabindex]:not([tabindex="-1"])',
        ),
      ).filter(
        (element) =>
          !element.hasAttribute("disabled") &&
          element.getAttribute("aria-hidden") !== "true",
      );

      if (focusable.length === 0) {
        event.preventDefault();
        dialogRef.current.focus();
        return;
      }

      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      const activeElement =
        document.activeElement instanceof HTMLElement
          ? document.activeElement
          : null;

      if (event.shiftKey) {
        if (!activeElement || activeElement === first) {
          event.preventDefault();
          last.focus();
        }
        return;
      }

      if (activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    };

    document.addEventListener("keydown", handleKeyDown);
    return () => {
      document.removeEventListener("keydown", handleKeyDown);
      window.cancelAnimationFrame(focusFrameID);
      document.body.style.overflow = originalOverflow;
      previousFocusRef.current?.focus();
    };
  }, [onClose, state]);

  if (!state || typeof document === "undefined") {
    return null;
  }

  const title = `${timelinePlayerLabel(state.side)} ${boardZoneLabel(state.zone)}`;
  const cardCount =
    state.source === "replay" ? state.objects.length : state.plays.length;
  const subtitle =
    state.source === "replay"
      ? `${cardCount} card${cardCount === 1 ? "" : "s"} currently in ${boardZoneLabel(state.zone).toLowerCase()} this step.`
      : `${cardCount} observed card${cardCount === 1 ? "" : "s"} first seen in ${boardZoneLabel(state.zone).toLowerCase()} in this game.`;
  const replayObjects =
    state.source === "replay" ? [...state.objects].sort(sortReplayObjects) : [];
  const observedPlays = state.source === "observed" ? [...state.plays] : [];

  return createPortal(
    <div
      className="match-replay-zone-dialog-backdrop"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) {
          onClose();
        }
      }}
    >
      <section
        ref={dialogRef}
        className="match-replay-zone-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={descriptionId}
        tabIndex={-1}
      >
        <div className="match-replay-zone-dialog-head">
          <div className="match-replay-zone-dialog-head-copy">
            <p className="match-replay-sidebox-label">Zone Viewer</p>
            <h5 id={titleId}>{title}</h5>
            <p
              id={descriptionId}
              className="match-replay-zone-dialog-description"
            >
              {subtitle}
            </p>
          </div>
          <button
            ref={closeButtonRef}
            type="button"
            className="match-replay-zone-dialog-close"
            onClick={onClose}
          >
            Close
          </button>
        </div>

        <div className="match-replay-zone-dialog-body">
          {state.source === "replay" ? (
            replayObjects.length === 0 ? (
              <p className="match-replay-empty">No cards in this zone.</p>
            ) : (
              <div
                className="match-replay-zone-dialog-grid"
                aria-label={`${title} cards`}
              >
                {replayObjects.map((object) => (
                  <div
                    className="match-replay-zone-dialog-card"
                    key={object.instanceId}
                  >
                    <MatchReplayObjectCard
                      object={object}
                      preview={previewByCardID.get(object.cardId) ?? null}
                      size="hand"
                    />
                  </div>
                ))}
              </div>
            )
          ) : observedPlays.length === 0 ? (
            <p className="match-replay-empty">No cards in this zone.</p>
          ) : (
            <div
              className="match-replay-zone-dialog-grid"
              aria-label={`${title} cards`}
            >
              {observedPlays.map((play) => (
                <div className="match-replay-zone-dialog-card" key={play.id}>
                  <MatchReplayCard
                    play={play}
                    preview={previewByCardID.get(play.cardId) ?? null}
                  />
                </div>
              ))}
            </div>
          )}
        </div>
      </section>
    </div>,
    document.body,
  );
}

export function MatchReplayFrameSideSummary({
  side,
  objects,
  lifeTotal,
  includeHand = false,
  onOpenZone,
  variant = "box",
  endpointId,
  onRegisterEndpoint,
  attackSummary,
  relationshipTargeted = false,
}: {
  side: "self" | "opponent";
  objects: MatchReplayFrameObject[];
  lifeTotal?: number;
  includeHand?: boolean;
  onOpenZone?: (state: MatchReplayZoneDialogState) => void;
  variant?: "box" | "rail";
  endpointId?: number;
  onRegisterEndpoint?: (
    instanceId: number,
    element: HTMLElement | null,
  ) => void;
  attackSummary?: { attackers: number; power: number } | null;
  relationshipTargeted?: boolean;
}) {
  const sideObjects = useMemo(
    () => objects.filter((object) => object.playerSide === side),
    [objects, side],
  );
  const zoneCounts = useMemo(
    () => summarizeReplayFrameZones(sideObjects),
    [sideObjects],
  );

  if (variant === "rail") {
    const railZones: BoardZoneKind[] = ["graveyard", "exile", "revealed"];
    const visibleZones = railZones.filter(
      (kind) => (zoneCounts.get(kind) ?? 0) > 0,
    );

    return (
      <section
        className={`match-replay-zonerail is-${side} ${attackSummary ? "is-under-attack" : ""} ${relationshipTargeted ? "is-targeted" : ""}`}
        aria-label={`${timelinePlayerLabel(side)} off-board zones`}
        ref={
          typeof endpointId === "number" && onRegisterEndpoint
            ? (element) => onRegisterEndpoint(endpointId, element)
            : undefined
        }
      >
        <div className="match-replay-zonerail-player">
          <span className="match-replay-zonerail-label">
            {timelinePlayerLabel(side)}
          </span>
          {side === "opponent" ? (
            <div
              className="match-replay-compact-hand"
              aria-label={`Opponent's hand, ${zoneCounts.get("hand") ?? 0} cards`}
            >
              <div className="match-replay-compact-hand-cards">
                {sideObjects
                  .filter((object) => boardZoneKind(object.zoneType) === "hand")
                  .map((object) => (
                    <div
                      key={object.instanceId}
                      className="match-replay-card is-cardback"
                      role="img"
                      aria-label="Face-down card"
                    >
                      <img src={magicCardbackURL} alt="" aria-hidden="true" />
                    </div>
                  ))}
              </div>
              <span className="match-replay-compact-hand-count">
                {zoneCounts.get("hand") ?? 0}
              </span>
            </div>
          ) : null}
        </div>
        {attackSummary ? (
          <span
            className="match-replay-zonerail-attack"
            aria-label={`Under attack, ${attackSummary.attackers} attacker${attackSummary.attackers === 1 ? "" : "s"}, ${attackSummary.power} power`}
          >
            <span
              className="match-replay-zonerail-attack-icon"
              aria-hidden="true"
            >
              <svg
                viewBox="0 0 16 16"
                fill="none"
                stroke="currentColor"
                strokeWidth="1.4"
              >
                <path d="M8 1.5 13 3.3v4.1c0 3.1-2 5.6-5 7.1-3-1.5-5-4-5-7.1V3.3L8 1.5Z" />
                <path d="M8 4.5v4.1" />
                <circle
                  cx="8"
                  cy="11.1"
                  r="0.6"
                  fill="currentColor"
                  stroke="none"
                />
              </svg>
            </span>
            <strong>Under attack</strong>
            <span className="match-replay-zonerail-attack-meta">
              {attackSummary.attackers} attacker
              {attackSummary.attackers === 1 ? "" : "s"}
              <span aria-hidden="true"> · </span>
              {attackSummary.power} power
            </span>
          </span>
        ) : null}
        <div className="match-replay-zonerail-zones">
          {visibleZones.map((kind) => {
            const count = zoneCounts.get(kind) ?? 0;
            const canOpen = isInspectableZoneKind(kind) && onOpenZone;
            const inner = (
              <>
                <span className="match-replay-zonechip-term">
                  {boardZoneLabel(kind)}
                </span>
                <span className="match-replay-zonechip-value">{count}</span>
              </>
            );

            if (canOpen) {
              return (
                <button
                  type="button"
                  className="match-replay-zonechip is-button"
                  key={kind}
                  aria-haspopup="dialog"
                  aria-label={`View ${timelinePlayerLabel(side)} ${boardZoneLabel(kind).toLowerCase()}, ${count} card${count === 1 ? "" : "s"}`}
                  onClick={() =>
                    onOpenZone({
                      source: "replay",
                      side,
                      zone: kind,
                      objects: sideObjects.filter(
                        (object) => boardZoneKind(object.zoneType) === kind,
                      ),
                    })
                  }
                >
                  {inner}
                </button>
              );
            }

            return (
              <span className="match-replay-zonechip" key={kind}>
                {inner}
              </span>
            );
          })}
        </div>
      </section>
    );
  }

  const stats: BoardZoneKind[] = includeHand
    ? ["hand", "battlefield", "graveyard", "exile", "revealed"]
    : ["battlefield", "graveyard", "exile", "revealed"];

  return (
    <section
      className={`match-replay-sidebox is-${side}`}
      aria-label={`${timelinePlayerLabel(side)} visible summary`}
    >
      <div className="match-replay-sidebox-head">
        <div className="match-replay-sidebox-head-copy">
          <p className="match-replay-sidebox-label">
            {timelinePlayerLabel(side)}
          </p>
          <p className="match-replay-sidebox-total">
            {sideObjects.length} visible card
            {sideObjects.length === 1 ? "" : "s"}
          </p>
        </div>
        {typeof lifeTotal === "number" ? (
          <div
            className="match-replay-sidebox-life-stat"
            aria-label={`${timelinePlayerLabel(side)} life total ${lifeTotal}`}
          >
            <span className="match-replay-sidebox-life-label">Life</span>
            <span className="match-replay-sidebox-life-value">{lifeTotal}</span>
          </div>
        ) : null}
      </div>
      <dl className="match-replay-stats">
        {stats.map((kind) => {
          const count = zoneCounts.get(kind) ?? 0;
          const canOpen =
            isInspectableZoneKind(kind) && count > 0 && onOpenZone;
          const content = (
            <>
              <span className="match-replay-stat-term">
                {boardZoneLabel(kind)}
              </span>
              <span className="match-replay-stat-value">{count}</span>
              {canOpen ? (
                <span className="match-replay-stat-hint">View cards</span>
              ) : null}
            </>
          );

          if (canOpen) {
            return (
              <button
                type="button"
                className="match-replay-stat match-replay-stat-button"
                key={kind}
                aria-haspopup="dialog"
                aria-label={`View ${timelinePlayerLabel(side)} ${boardZoneLabel(kind).toLowerCase()}, ${count} card${count === 1 ? "" : "s"}`}
                onClick={() =>
                  onOpenZone({
                    source: "replay",
                    side,
                    zone: kind,
                    objects: sideObjects.filter(
                      (object) => boardZoneKind(object.zoneType) === kind,
                    ),
                  })
                }
              >
                {content}
              </button>
            );
          }

          return (
            <div className="match-replay-stat" key={kind}>
              {content}
            </div>
          );
        })}
      </dl>
    </section>
  );
}

export function MatchReplaySideSummary({
  side,
  plays,
  onOpenZone,
}: {
  side: "self" | "opponent";
  plays: MatchCardPlay[];
  onOpenZone?: (state: MatchReplayZoneDialogState) => void;
}) {
  const zoneCounts = useMemo(() => summarizeReplayZones(plays), [plays]);
  const stats: BoardZoneKind[] = [
    "battlefield",
    "graveyard",
    "exile",
    "revealed",
  ];

  return (
    <section
      className={`match-replay-sidebox is-${side}`}
      aria-label={`${timelinePlayerLabel(side)} observed summary`}
    >
      <div className="match-replay-sidebox-head">
        <div>
          <p className="match-replay-sidebox-label">
            {timelinePlayerLabel(side)}
          </p>
          <p className="match-replay-sidebox-total">
            {plays.length} observed card{plays.length === 1 ? "" : "s"}
          </p>
        </div>
      </div>
      <dl className="match-replay-stats">
        {stats.map((kind) => {
          const count = zoneCounts.get(kind) ?? 0;
          const canOpen =
            isInspectableZoneKind(kind) && count > 0 && onOpenZone;
          const content = (
            <>
              <span className="match-replay-stat-term">
                {boardZoneLabel(kind)}
              </span>
              <span className="match-replay-stat-value">{count}</span>
              {canOpen ? (
                <span className="match-replay-stat-hint">View cards</span>
              ) : null}
            </>
          );

          if (canOpen) {
            return (
              <button
                type="button"
                className="match-replay-stat match-replay-stat-button"
                key={kind}
                aria-haspopup="dialog"
                aria-label={`View ${timelinePlayerLabel(side)} ${boardZoneLabel(kind).toLowerCase()}, ${count} card${count === 1 ? "" : "s"}`}
                onClick={() =>
                  onOpenZone({
                    source: "observed",
                    side,
                    zone: kind,
                    plays: plays.filter(
                      (play) => boardZoneKind(play.firstPublicZone) === kind,
                    ),
                  })
                }
              >
                {content}
              </button>
            );
          }

          return (
            <div className="match-replay-stat" key={kind}>
              {content}
            </div>
          );
        })}
      </dl>
    </section>
  );
}
