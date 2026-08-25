import { useEffect, useMemo, useRef, useState } from "react";
import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";

import { api } from "../lib/api";
import {
  DECK_COLOR_ORDER,
  MANA_CURVE_BUCKETS,
  addCard,
  adjustQuantity,
  colorCounts,
  deckWarnings,
  manaCurve,
  moveCard,
  parseManaCostSymbols,
  removeCard,
  sectionCards,
  sectionTotal,
  sortProjectCards,
} from "../lib/deckBuilder";
import type { CardDefinition, DeckProjectCard, DeckProjectSection } from "../lib/types";
import { useBreadcrumbLabel } from "../components/Breadcrumbs";
import { CardPreviewName } from "../components/CardPreviewName";
import { ManaSymbol } from "../components/ManaSymbol";
import { RarityDot } from "../components/RarityDot";
import { StatusMessage } from "../components/StatusMessage";
import type { CardRarity } from "../lib/scryfall";

const AUTOSAVE_DELAY_MS = 800;
const SEARCH_DEBOUNCE_MS = 250;

type SaveStatus = "idle" | "dirty" | "saving" | "saved" | "error";

const SAVE_STATUS_LABELS: Record<SaveStatus, string> = {
  idle: "",
  dirty: "Unsaved changes",
  saving: "Saving…",
  saved: "Saved",
  error: "Save failed",
};

function useDebouncedValue<T>(value: T, delayMs: number): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const timer = window.setTimeout(() => setDebounced(value), delayMs);
    return () => window.clearTimeout(timer);
  }, [value, delayMs]);
  return debounced;
}

function ManaCost({ manaCost }: { manaCost: string }) {
  const symbols = parseManaCostSymbols(manaCost);
  if (symbols.length === 0) return null;
  return (
    <span className="deck-card-mana-icons builder-mana">
      {symbols.map((symbol, index) => (
        <ManaSymbol key={`${symbol}-${index}`} token={symbol} />
      ))}
    </span>
  );
}

function asKnownRarity(rarity: string): CardRarity | undefined {
  return rarity === "common" || rarity === "uncommon" || rarity === "rare" || rarity === "mythic"
    ? rarity
    : undefined;
}

function SearchPanel({ onAdd }: { onAdd: (definition: CardDefinition, section: DeckProjectSection) => void }) {
  const [query, setQuery] = useState("");
  const [colors, setColors] = useState<string[]>([]);
  const [typeText, setTypeText] = useState("");
  const [rarity, setRarity] = useState("");

  const debouncedQuery = useDebouncedValue(query, SEARCH_DEBOUNCE_MS);
  const debouncedType = useDebouncedValue(typeText, SEARCH_DEBOUNCE_MS);
  const hasFilters =
    debouncedQuery.trim() !== "" || colors.length > 0 || debouncedType.trim() !== "" || rarity !== "";

  const searchQuery = useQuery({
    queryKey: ["card-search", debouncedQuery, colors.join(""), debouncedType, rarity],
    queryFn: () =>
      api.cardSearch({
        q: debouncedQuery.trim() || undefined,
        colors: colors.join("") || undefined,
        type: debouncedType.trim() || undefined,
        rarity: rarity || undefined,
        limit: 50,
      }),
    enabled: hasFilters,
    staleTime: 1000 * 60 * 5,
    placeholderData: (previous) => previous,
  });

  return (
    <section className="panel builder-search-panel">
      <div className="panel-head">
        <h3>Card search</h3>
        <p>{hasFilters && searchQuery.data ? `${searchQuery.data.total} cards` : "Search the Arena catalog"}</p>
      </div>
      <div className="builder-search-controls">
        <input
          type="search"
          className="settings-input"
          placeholder="Card name…"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          autoFocus
        />
        <div className="builder-search-filters">
          <div className="builder-color-filters" role="group" aria-label="Color filter">
            {DECK_COLOR_ORDER.map((color) => (
              <button
                key={color}
                type="button"
                className={`control-button match-filter-toggle builder-color-toggle ${colors.includes(color) ? "is-active" : ""}`}
                aria-pressed={colors.includes(color)}
                onClick={() =>
                  setColors((current) =>
                    current.includes(color) ? current.filter((c) => c !== color) : [...current, color],
                  )
                }
              >
                <ManaSymbol token={color} />
              </button>
            ))}
          </div>
          <input
            type="search"
            className="settings-input builder-type-filter"
            placeholder="Type (e.g. creature)…"
            value={typeText}
            onChange={(event) => setTypeText(event.target.value)}
          />
          <select
            className="settings-input builder-rarity-filter"
            value={rarity}
            onChange={(event) => setRarity(event.target.value)}
            aria-label="Rarity filter"
          >
            <option value="">Any rarity</option>
            <option value="common">Common</option>
            <option value="uncommon">Uncommon</option>
            <option value="rare">Rare</option>
            <option value="mythic">Mythic</option>
            <option value="land">Basic land</option>
          </select>
        </div>
      </div>
      {!hasFilters ? (
        <p className="state">Type a card name or pick a filter to search.</p>
      ) : searchQuery.isLoading ? (
        <StatusMessage>Searching…</StatusMessage>
      ) : searchQuery.isError ? (
        <StatusMessage tone="error">{(searchQuery.error as Error).message}</StatusMessage>
      ) : (
        <ul className="builder-search-results">
          {(searchQuery.data?.cards ?? []).map((card) => (
            <li key={card.arenaId} className="builder-search-result">
              <span className="builder-result-rarity">
                <RarityDot rarity={asKnownRarity(card.rarity)} />
              </span>
              <span className="builder-result-name">
                <CardPreviewName cardId={card.arenaId} cardName={card.name} inline />
              </span>
              <ManaCost manaCost={card.manaCost} />
              <span className="builder-result-type" title={card.typeLine}>
                {card.typeLine}
              </span>
              <span className="builder-result-set">{card.setCode}</span>
              <span className="builder-result-actions">
                <button
                  type="button"
                  className="control-button control-button--quiet builder-add-button"
                  onClick={() => onAdd(card, "main")}
                  title="Add to main deck"
                >
                  + Main
                </button>
                <button
                  type="button"
                  className="control-button control-button--quiet builder-add-button"
                  onClick={() => onAdd(card, "sideboard")}
                  title="Add to sideboard"
                >
                  + Side
                </button>
              </span>
            </li>
          ))}
          {hasFilters && searchQuery.data && searchQuery.data.cards.length === 0 ? (
            <li className="state">No cards match.</li>
          ) : null}
        </ul>
      )}
    </section>
  );
}

function DeckSection({
  title,
  section,
  cards,
  onAdjust,
  onMove,
  onRemove,
}: {
  title: string;
  section: DeckProjectSection;
  cards: DeckProjectCard[];
  onAdjust: (arenaId: number, delta: number) => void;
  onMove: (arenaId: number) => void;
  onRemove: (arenaId: number) => void;
}) {
  const rows = sortProjectCards(sectionCards(cards, section));
  const total = sectionTotal(cards, section);

  return (
    <div className="builder-deck-section">
      <div className="builder-deck-section-head">
        <h4>{title}</h4>
        <span className="deck-card-qty">{total}</span>
      </div>
      {rows.length === 0 ? (
        <p className="state">Empty. Add cards from search.</p>
      ) : (
        <ul className="builder-deck-list">
          {rows.map((card) => (
            <li key={card.arenaId} className={`builder-deck-row ${card.missing ? "is-missing" : ""}`}>
              <span className="builder-row-controls">
                <button
                  type="button"
                  className="builder-qty-button"
                  onClick={() => onAdjust(card.arenaId, -1)}
                  aria-label={`Remove one ${card.name}`}
                >
                  −
                </button>
                <span className="deck-card-qty builder-row-qty">{card.quantity}</span>
                <button
                  type="button"
                  className="builder-qty-button"
                  onClick={() => onAdjust(card.arenaId, 1)}
                  aria-label={`Add one ${card.name}`}
                >
                  +
                </button>
              </span>
              <span className="builder-row-name">
                <CardPreviewName cardId={card.arenaId} cardName={card.name} inline />
                {card.missing ? <span className="builder-missing-tag">missing</span> : null}
              </span>
              <ManaCost manaCost={card.manaCost} />
              <span className="builder-row-actions">
                <button
                  type="button"
                  className="builder-qty-button"
                  onClick={() => onMove(card.arenaId)}
                  title={section === "main" ? "Move to sideboard" : "Move to main deck"}
                  aria-label={`Move ${card.name} to ${section === "main" ? "sideboard" : "main deck"}`}
                >
                  ⇄
                </button>
                <button
                  type="button"
                  className="builder-qty-button"
                  onClick={() => onRemove(card.arenaId)}
                  title="Remove all copies"
                  aria-label={`Remove all copies of ${card.name}`}
                >
                  ×
                </button>
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function CurvePanel({ cards }: { cards: DeckProjectCard[] }) {
  const curve = manaCurve(cards);
  const maxCount = Math.max(...curve, 1);
  const colors = colorCounts(cards);

  return (
    <div className="builder-curve">
      <div className="builder-curve-bars" role="img" aria-label="Mana curve">
        {curve.map((count, index) => (
          <div key={MANA_CURVE_BUCKETS[index]} className="builder-curve-column" title={`${count} cards`}>
            <span className="builder-curve-count">{count > 0 ? count : ""}</span>
            <div className="builder-curve-bar" style={{ height: `${(count / maxCount) * 100}%` }} />
            <span className="builder-curve-label">{MANA_CURVE_BUCKETS[index]}</span>
          </div>
        ))}
      </div>
      <div className="builder-color-summary" aria-label="Color distribution">
        {DECK_COLOR_ORDER.filter((color) => (colors[color] ?? 0) > 0).map((color) => (
          <span key={color} className="builder-color-count">
            <ManaSymbol token={color} /> {colors[color]}
          </span>
        ))}
      </div>
    </div>
  );
}

function CopyForArenaButton({ projectId }: { projectId: number }) {
  const [label, setLabel] = useState("Copy for Arena");

  const copy = async () => {
    try {
      const exported = await api.exportDeckProject(projectId);
      try {
        await navigator.clipboard.writeText(exported.text);
      } catch {
        const textarea = document.createElement("textarea");
        textarea.value = exported.text;
        document.body.appendChild(textarea);
        textarea.select();
        try {
          document.execCommand("copy");
        } finally {
          textarea.remove();
        }
      }
      setLabel(exported.unresolved.length > 0 ? "Copied (some cards skipped)" : "Copied!");
    } catch {
      setLabel("Copy failed");
    }
    window.setTimeout(() => setLabel("Copy for Arena"), 2500);
  };

  return (
    <button type="button" className="control-button" onClick={() => void copy()}>
      {label}
    </button>
  );
}

export function DeckBuilderEditorPage() {
  const { projectId } = useParams();
  const id = Number(projectId);

  const projectQuery = useQuery({
    queryKey: ["deck-project", id],
    queryFn: () => api.deckProject(id),
    enabled: Number.isFinite(id) && id > 0,
    refetchOnWindowFocus: false,
  });

  const [name, setName] = useState("");
  const [format, setFormat] = useState("");
  const [cards, setCards] = useState<DeckProjectCard[]>([]);
  const [saveStatus, setSaveStatus] = useState<SaveStatus>("idle");
  const initializedRef = useRef(false);
  const saveTimerRef = useRef<number | null>(null);

  useEffect(() => {
    if (initializedRef.current || !projectQuery.data) return;
    initializedRef.current = true;
    setName(projectQuery.data.name);
    setFormat(projectQuery.data.format);
    setCards(projectQuery.data.cards);
  }, [projectQuery.data]);

  // Debounced autosave: local edits mark the project dirty, then one PUT
  // replaces the full project state.
  useEffect(() => {
    if (!initializedRef.current) return;
    if (saveStatus !== "dirty") return;

    if (saveTimerRef.current != null) window.clearTimeout(saveTimerRef.current);
    saveTimerRef.current = window.setTimeout(() => {
      setSaveStatus("saving");
      api
        .saveDeckProject(id, {
          name,
          format,
          cards: cards.map((card) => ({ section: card.section, arenaId: card.arenaId, quantity: card.quantity })),
        })
        .then(() => setSaveStatus((current) => (current === "saving" ? "saved" : current)))
        .catch(() => setSaveStatus("error"));
    }, AUTOSAVE_DELAY_MS);

    return () => {
      if (saveTimerRef.current != null) window.clearTimeout(saveTimerRef.current);
    };
  }, [saveStatus, name, format, cards, id]);

  const markDirty = () => setSaveStatus("dirty");

  const warnings = useMemo(() => deckWarnings(name, cards), [name, cards]);

  useBreadcrumbLabel(initializedRef.current ? name.trim() || `Deck #${id}` : null);

  if (!Number.isFinite(id) || id <= 0) {
    return <StatusMessage tone="error">Invalid deck project id.</StatusMessage>;
  }
  if (projectQuery.isLoading) return <StatusMessage>Loading deck project…</StatusMessage>;
  if (projectQuery.isError) {
    return <StatusMessage tone="error">{(projectQuery.error as Error).message}</StatusMessage>;
  }

  const handleAdd = (definition: CardDefinition, section: DeckProjectSection) => {
    setCards((current) => addCard(current, definition, section));
    markDirty();
  };

  return (
    <div className="builder-editor">
      <section className="panel builder-header-panel">
        <div className="builder-header">
          <div className="builder-header-fields">
            <input
              type="text"
              className="settings-input builder-name-input"
              value={name}
              placeholder="Deck name"
              aria-label="Deck name"
              onChange={(event) => {
                setName(event.target.value);
                markDirty();
              }}
            />
            <input
              type="text"
              className="settings-input builder-format-input"
              value={format}
              placeholder="Format (e.g. standard)"
              aria-label="Format"
              onChange={(event) => {
                setFormat(event.target.value);
                markDirty();
              }}
            />
          </div>
          <div className="builder-header-actions">
            <span
              className={`builder-save-status ${saveStatus === "error" ? "is-error" : ""}`}
              role={saveStatus === "error" ? "alert" : "status"}
            >
              {SAVE_STATUS_LABELS[saveStatus]}
            </span>
            <CopyForArenaButton projectId={id} />
          </div>
        </div>
        {warnings.length > 0 ? (
          <ul className="builder-warnings">
            {warnings.map((warning) => (
              <li key={warning}>{warning}</li>
            ))}
          </ul>
        ) : null}
      </section>

      <div className="builder-columns">
        <SearchPanel onAdd={handleAdd} />
        <section className="panel builder-deck-panel">
          <div className="panel-head">
            <h3>Deck</h3>
            <p>
              {sectionTotal(cards, "main")} main / {sectionTotal(cards, "sideboard")} side
            </p>
          </div>
          <CurvePanel cards={cards} />
          <DeckSection
            title="Main deck"
            section="main"
            cards={cards}
            onAdjust={(arenaId, delta) => {
              setCards((current) => adjustQuantity(current, arenaId, "main", delta));
              markDirty();
            }}
            onMove={(arenaId) => {
              setCards((current) => moveCard(current, arenaId, "main"));
              markDirty();
            }}
            onRemove={(arenaId) => {
              setCards((current) => removeCard(current, arenaId, "main"));
              markDirty();
            }}
          />
          <DeckSection
            title="Sideboard"
            section="sideboard"
            cards={cards}
            onAdjust={(arenaId, delta) => {
              setCards((current) => adjustQuantity(current, arenaId, "sideboard", delta));
              markDirty();
            }}
            onMove={(arenaId) => {
              setCards((current) => moveCard(current, arenaId, "sideboard"));
              markDirty();
            }}
            onRemove={(arenaId) => {
              setCards((current) => removeCard(current, arenaId, "sideboard"));
              markDirty();
            }}
          />
        </section>
      </div>
    </div>
  );
}
