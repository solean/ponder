import { useNavigate } from "react-router-dom";
import type { KeyboardEvent, MouseEvent } from "react";

const ROW_INTERACTIVE_SELECTOR =
  "a, button, input, select, textarea, summary, [role='button'], [role='link']";

function targetIsInteractive(target: EventTarget | null, currentTarget: EventTarget | null): boolean {
  if (!(target instanceof Element)) return false;
  const interactiveTarget = target.closest(ROW_INTERACTIVE_SELECTOR);
  return interactiveTarget != null && interactiveTarget !== currentTarget;
}

/**
 * Row click/keyboard handlers for making an entire <tr> navigate to `href`,
 * while still letting nested links/buttons handle their own clicks.
 */
export function useRowLink(href: string, state?: unknown) {
  const navigate = useNavigate();

  function open(newTab: boolean) {
    if (newTab) {
      window.open(href, "_blank", "noopener,noreferrer");
      return;
    }
    navigate(href, { state });
  }

  return {
    onClick: (event: MouseEvent<HTMLTableRowElement>) => {
      if (event.defaultPrevented || targetIsInteractive(event.target, event.currentTarget)) return;
      open(event.metaKey || event.ctrlKey);
    },
    onAuxClick: (event: MouseEvent<HTMLTableRowElement>) => {
      if (
        event.defaultPrevented ||
        targetIsInteractive(event.target, event.currentTarget) ||
        event.button !== 1
      ) {
        return;
      }
      open(true);
    },
    onKeyDown: (event: KeyboardEvent<HTMLTableRowElement>) => {
      if (event.defaultPrevented || targetIsInteractive(event.target, event.currentTarget)) return;
      if (event.key !== "Enter" && event.key !== " ") return;
      event.preventDefault();
      open(false);
    },
    role: "link" as const,
    tabIndex: 0,
    className: "data-table-row-link",
  };
}
