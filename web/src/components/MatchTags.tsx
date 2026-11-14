import { useCallback, useEffect, useId, useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent, type SyntheticEvent } from "react";
import { createPortal } from "react-dom";
import { useQuery } from "@tanstack/react-query";

import { api } from "../lib/api";
import { MAX_MATCH_TAG_LENGTH, MAX_MATCH_TAGS, matchTagSuggestions, withMatchTag } from "../lib/matchTags";
import { useSetMatchTags } from "../lib/useSetMatchTags";

const POPOVER_WIDTH_PX = 272;
const POPOVER_GAP_PX = 6;
const VIEWPORT_MARGIN_PX = 8;

/** Read-only tag chips. */
export function MatchTagChips({ tags, maxVisible }: { tags: string[]; maxVisible?: number }) {
  const visible = maxVisible === undefined ? tags : tags.slice(0, maxVisible);
  const hidden = tags.length - visible.length;
  return (
    <span className="match-tag-chips">
      {visible.map((tag) => (
        <span key={tag} className="match-tag-chip">
          {tag}
        </span>
      ))}
      {hidden > 0 ? (
        <span className="match-tag-chip is-more" title={tags.slice(visible.length).join(", ")}>
          +{hidden}
        </span>
      ) : null}
    </span>
  );
}

function TagIcon() {
  return (
    <svg className="match-tag-icon" viewBox="0 0 24 24" aria-hidden="true">
      <path
        d="M3.5 12.2V4.5a1 1 0 0 1 1-1h7.7l8.3 8.3a1 1 0 0 1 0 1.4l-7 7a1 1 0 0 1-1.4 0z"
        fill="none"
        stroke="currentColor"
        strokeWidth={1.6}
        strokeLinejoin="round"
      />
      <circle cx="8.2" cy="8.2" r="1.5" fill="currentColor" />
    </svg>
  );
}

/**
 * A match's tags plus a button that opens the tag editor. Used in match table
 * rows (compact) and the match detail header.
 */
export function MatchTagsControl({
  matchId,
  tags,
  maxVisible,
  className = "",
}: {
  matchId: number;
  tags: string[];
  /** Chips beyond this collapse into "+n". */
  maxVisible?: number;
  className?: string;
}) {
  const [open, setOpen] = useState(false);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const label = tags.length > 0 ? `Edit tags: ${tags.join(", ")}` : "Add tags";

  return (
    <span className={`match-tags-control${className ? ` ${className}` : ""}`}>
      <button
        ref={buttonRef}
        type="button"
        className={`match-tags-button${tags.length === 0 ? " is-empty" : ""}`}
        aria-label={label}
        title={label}
        aria-haspopup="dialog"
        aria-expanded={open}
        onClick={() => setOpen((current) => !current)}
      >
        {tags.length > 0 ? <MatchTagChips tags={tags} maxVisible={maxVisible} /> : <TagIcon />}
      </button>
      {open ? (
        <MatchTagEditorPopover
          anchor={buttonRef}
          matchId={matchId}
          tags={tags}
          onClose={(restoreFocus) => {
            setOpen(false);
            if (restoreFocus) buttonRef.current?.focus();
          }}
        />
      ) : null}
    </span>
  );
}

function stopPropagation(event: SyntheticEvent) {
  // The popover renders in a portal, but React still bubbles its events to
  // the table row, which would open the match.
  event.stopPropagation();
}

function MatchTagEditorPopover({
  anchor,
  matchId,
  tags,
  onClose,
}: {
  anchor: React.RefObject<HTMLElement>;
  matchId: number;
  tags: string[];
  onClose: (restoreFocus: boolean) => void;
}) {
  const popoverRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const listId = useId();
  const [query, setQuery] = useState("");
  const [highlight, setHighlight] = useState(-1);
  const [position, setPosition] = useState<{ top: number; left: number } | null>(null);
  const mutation = useSetMatchTags();

  const usageQuery = useQuery({ queryKey: ["match-tags"], queryFn: api.matchTagUsage });
  // No suggestions until usage loads, so the starters don't flash up when
  // tags already exist.
  const suggestions = useMemo(
    () =>
      usageQuery.data
        ? matchTagSuggestions(
            usageQuery.data.map((entry) => entry.tag),
            tags,
            query,
          )
        : [],
    [usageQuery.data, tags, query],
  );
  const atLimit = tags.length >= MAX_MATCH_TAGS;

  const onCloseRef = useRef(onClose);
  onCloseRef.current = onClose;

  // Places the popover under its anchor (above when there's no room), and
  // reports false once the anchor has scrolled out of its scroll container.
  const place = useCallback((): boolean => {
    const anchorEl = anchor.current;
    if (!anchorEl?.isConnected) return false;
    const anchorRect = anchorEl.getBoundingClientRect();
    const bounds = anchorEl.closest(".table-wrap")?.getBoundingClientRect() ?? {
      top: 0,
      bottom: window.innerHeight,
    };
    if (anchorRect.bottom < bounds.top || anchorRect.top > bounds.bottom) return false;
    const popoverHeight = popoverRef.current?.offsetHeight ?? 0;
    const left = Math.min(
      Math.max(VIEWPORT_MARGIN_PX, anchorRect.left),
      window.innerWidth - POPOVER_WIDTH_PX - VIEWPORT_MARGIN_PX,
    );
    const below = anchorRect.bottom + POPOVER_GAP_PX;
    const fitsBelow = below + popoverHeight <= window.innerHeight - VIEWPORT_MARGIN_PX;
    const top = fitsBelow ? below : Math.max(VIEWPORT_MARGIN_PX, anchorRect.top - POPOVER_GAP_PX - popoverHeight);
    setPosition((current) => (current?.top === top && current.left === left ? current : { top, left }));
    return true;
  }, [anchor]);

  useLayoutEffect(() => {
    place();
  }, [place, tags.length, suggestions.length]);

  // Focus once placed: the popover is hidden until then and can't take focus.
  const placed = position !== null;
  useEffect(() => {
    if (placed) inputRef.current?.focus();
  }, [placed]);

  useEffect(() => {
    function handlePointerDown(event: PointerEvent) {
      const target = event.target as Node;
      if (popoverRef.current?.contains(target) || anchor.current?.contains(target)) return;
      onCloseRef.current(false);
    }
    // Follow the anchor as the table or page scrolls; close once it's gone.
    function handleReflow(event: Event) {
      if (event.target instanceof Node && popoverRef.current?.contains(event.target)) return;
      if (!place()) onCloseRef.current(false);
    }
    document.addEventListener("pointerdown", handlePointerDown, true);
    window.addEventListener("scroll", handleReflow, true);
    window.addEventListener("resize", handleReflow);
    return () => {
      document.removeEventListener("pointerdown", handlePointerDown, true);
      window.removeEventListener("scroll", handleReflow, true);
      window.removeEventListener("resize", handleReflow);
    };
  }, [anchor, place]);

  function save(next: string[]) {
    if (next === tags) return;
    mutation.mutate({ matchId, tags: next });
  }

  function addTag(raw: string) {
    save(withMatchTag(tags, raw));
    setQuery("");
    setHighlight(-1);
  }

  function removeTag(tag: string) {
    save(tags.filter((value) => value !== tag));
    inputRef.current?.focus();
  }

  function handleKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.key === "Escape") {
      event.preventDefault();
      onClose(true);
    } else if (event.key === "ArrowDown") {
      event.preventDefault();
      setHighlight((current) => (suggestions.length === 0 ? -1 : (current + 1) % suggestions.length));
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      setHighlight((current) =>
        suggestions.length === 0 ? -1 : (current <= 0 ? suggestions.length : current) - 1,
      );
    } else if (event.key === "Enter") {
      event.preventDefault();
      const picked = highlight >= 0 ? suggestions[highlight] : query;
      if (picked) addTag(picked);
    } else if (event.key === "Backspace" && query === "" && tags.length > 0) {
      event.preventDefault();
      removeTag(tags[tags.length - 1]);
    }
  }

  return createPortal(
    <div
      ref={popoverRef}
      className="match-tag-popover"
      role="dialog"
      aria-label="Match tags"
      style={{
        width: POPOVER_WIDTH_PX,
        top: position?.top ?? 0,
        left: position?.left ?? 0,
        visibility: position ? "visible" : "hidden",
      }}
      onClick={stopPropagation}
      onAuxClick={stopPropagation}
      onKeyDown={stopPropagation}
      onPointerDown={stopPropagation}
    >
      {tags.length > 0 ? (
        <ul className="match-tag-popover-current" aria-label="Current tags">
          {tags.map((tag) => (
            <li key={tag} className="match-tag-chip is-removable">
              <span>{tag}</span>
              <button type="button" aria-label={`Remove tag ${tag}`} onClick={() => removeTag(tag)}>
                ×
              </button>
            </li>
          ))}
        </ul>
      ) : null}
      <input
        ref={inputRef}
        className="settings-input match-tag-input"
        type="text"
        value={query}
        maxLength={MAX_MATCH_TAG_LENGTH}
        disabled={atLimit}
        placeholder={atLimit ? `${MAX_MATCH_TAGS} tags max` : "Add a tag…"}
        spellCheck={false}
        autoComplete="off"
        role="combobox"
        aria-expanded={suggestions.length > 0}
        aria-controls={listId}
        aria-autocomplete="list"
        aria-activedescendant={highlight >= 0 ? `${listId}-${highlight}` : undefined}
        onChange={(event) => {
          setQuery(event.target.value);
          setHighlight(-1);
        }}
        onKeyDown={handleKeyDown}
      />
      {suggestions.length > 0 && !atLimit ? (
        <ul id={listId} className="match-tag-suggestions" role="listbox" aria-label="Suggested tags">
          {suggestions.map((tag, index) => (
            <li
              key={tag}
              id={`${listId}-${index}`}
              role="option"
              aria-selected={index === highlight}
              className={`match-tag-suggestion${index === highlight ? " is-highlighted" : ""}`}
              onPointerDown={(event) => event.preventDefault()}
              onClick={() => addTag(tag)}
              onPointerEnter={() => setHighlight(index)}
            >
              {tag}
            </li>
          ))}
        </ul>
      ) : null}
      {mutation.isError ? (
        <p className="match-tag-error" role="alert">
          Couldn't save tags: {mutation.error.message}
        </p>
      ) : null}
    </div>,
    document.body,
  );
}
