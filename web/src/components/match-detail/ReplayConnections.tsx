import {
  useEffect,
  useId,
  useState,
  type MutableRefObject,
  type RefObject,
} from "react";
import {
  type ReplayBoardConnection,
  type ReplayConnectionKind,
} from "../../lib/replay";

export function MatchReplayConnectionOverlay({
  surfaceRef,
  cardShellsRef,
  connections,
  focusedInstanceId,
}: {
  surfaceRef: RefObject<HTMLDivElement | null>;
  cardShellsRef: MutableRefObject<Map<number, HTMLElement>>;
  connections: ReplayBoardConnection[];
  focusedInstanceId: number | null;
}) {
  const combatMarkerId = useId();
  const spellTargetMarkerId = useId();
  const [snapshot, setSnapshot] = useState<{
    width: number;
    height: number;
    paths: Array<{
      key: string;
      d: string;
      kind: ReplayConnectionKind;
      highlighted: boolean;
    }>;
  } | null>(null);

  useEffect(() => {
    if (connections.length === 0) {
      setSnapshot(null);
      return;
    }

    const surfaceElement = surfaceRef.current;
    if (!surfaceElement) {
      setSnapshot(null);
      return;
    }

    let frameID = 0;
    let resizeObserver: ResizeObserver | null = null;

    const measure = () => {
      frameID = 0;
      const root = surfaceRef.current;
      if (!root) {
        setSnapshot(null);
        return;
      }

      const surfaceRect = root.getBoundingClientRect();
      if (surfaceRect.width <= 0 || surfaceRect.height <= 0) {
        setSnapshot(null);
        return;
      }

      const paths = connections.flatMap((connection, connectionIndex) => {
        const highlighted =
          focusedInstanceId !== null &&
          (connection.sourceId === focusedInstanceId ||
            connection.targetId === focusedInstanceId);
        if (connection.hiddenUnlessFocused && !highlighted) {
          return [];
        }
        const sourceElement = cardShellsRef.current.get(connection.sourceId);
        const targetElement = cardShellsRef.current.get(connection.targetId);
        if (!sourceElement || !targetElement) {
          return [];
        }

        const sourceRect = sourceElement.getBoundingClientRect();
        const targetRect = targetElement.getBoundingClientRect();
        const startX =
          sourceRect.left + sourceRect.width / 2 - surfaceRect.left;
        const startY = sourceRect.top + sourceRect.height / 2 - surfaceRect.top;
        const endX = targetRect.left + targetRect.width / 2 - surfaceRect.left;
        const endY = targetRect.top + targetRect.height / 2 - surfaceRect.top;
        const d =
          connection.kind === "spellTarget" ||
          connection.kind === "abilityTarget" ||
          connection.kind === "trigger"
            ? (() => {
                const deltaX = endX - startX;
                const direction = deltaX >= 0 ? 1 : -1;
                const horizontalPull =
                  Math.max(Math.abs(deltaX) * 0.38, 72) * direction;
                return `M ${startX} ${startY} C ${startX + horizontalPull} ${startY}, ${endX - horizontalPull} ${endY}, ${endX} ${endY}`;
              })()
            : (() => {
                const deltaY = endY - startY;
                return `M ${startX} ${startY} C ${startX} ${startY + deltaY * 0.34}, ${endX} ${endY - deltaY * 0.34}, ${endX} ${endY}`;
              })();

        return [
          {
            key: `${connection.kind}-${connection.sourceId}-${connection.targetId}-${connectionIndex}`,
            d,
            kind: connection.kind,
            highlighted,
          },
        ];
      });

      setSnapshot({
        width: surfaceRect.width,
        height: surfaceRect.height,
        paths,
      });
    };

    const scheduleMeasure = () => {
      if (frameID !== 0) {
        return;
      }
      frameID = window.requestAnimationFrame(measure);
    };

    scheduleMeasure();
    if (typeof ResizeObserver !== "undefined") {
      resizeObserver = new ResizeObserver(() => {
        scheduleMeasure();
      });
      resizeObserver.observe(surfaceElement);
      for (const element of cardShellsRef.current.values()) {
        resizeObserver.observe(element);
      }
    }
    window.addEventListener("resize", scheduleMeasure);

    return () => {
      if (frameID !== 0) {
        window.cancelAnimationFrame(frameID);
      }
      resizeObserver?.disconnect();
      window.removeEventListener("resize", scheduleMeasure);
    };
  }, [surfaceRef, cardShellsRef, connections, focusedInstanceId]);

  if (!snapshot || snapshot.paths.length === 0) {
    return null;
  }

  const shouldMuteIdleLines =
    focusedInstanceId === null && snapshot.paths.length > 3;

  return (
    <svg
      className={`match-replay-connection-overlay ${shouldMuteIdleLines ? "is-muted" : ""}`}
      viewBox={`0 0 ${snapshot.width} ${snapshot.height}`}
      preserveAspectRatio="none"
      aria-hidden="true"
    >
      <defs>
        <marker
          id={combatMarkerId}
          markerWidth="10"
          markerHeight="10"
          refX="9"
          refY="5"
          orient="auto"
          markerUnits="strokeWidth"
        >
          <path
            d="M 0 0 L 10 5 L 0 10 z"
            fill="var(--combat-connection-line)"
          />
        </marker>
        <marker
          id={spellTargetMarkerId}
          markerWidth="10"
          markerHeight="10"
          refX="9"
          refY="5"
          orient="auto"
          markerUnits="strokeWidth"
        >
          <path
            d="M 0 0 L 10 5 L 0 10 z"
            fill="var(--spell-target-connection-line)"
          />
        </marker>
      </defs>
      {snapshot.paths.map((path) => (
        <path
          key={path.key}
          className={`match-replay-connection-path is-${path.kind.replace(/[A-Z]/g, (letter) => `-${letter.toLowerCase()}`)} ${path.highlighted ? "is-highlighted" : ""}`}
          d={path.d}
          markerEnd={
            path.kind === "spellTarget" || path.kind === "abilityTarget"
              ? `url(#${spellTargetMarkerId})`
              : path.kind === "combat" || path.kind === "attackTarget"
                ? `url(#${combatMarkerId})`
                : undefined
          }
        />
      ))}
    </svg>
  );
}
