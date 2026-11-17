import { OverlayManaPanel } from "./OverlayManaPanel";
import { useEffect, useRef, useState, type CSSProperties } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { OverlaySaveQueue } from "../lib/overlaySaveQueue";
import { api } from "../lib/api";
import { DEFAULT_OVERLAY_SETTINGS, overlayPanelWidth, overlayShortcutLabel, type OverlaySettings } from "../lib/overlaySettings";
import { StatusMessage } from "./StatusMessage";
import { CardPreviewName } from "./CardPreviewName";
import { ManaSymbol } from "./ManaSymbol";
import { SettingRow, ToggleRow } from "./SettingRow";
import "./OverlaySettingsPanel.css";

const badgeCalibrationFields = [
  { key: "draftBadgeLeft", label: "First card left", min: 0, max: 90 },
  { key: "draftBadgeTop", label: "First card top", min: 0, max: 90 },
  { key: "draftBadgeWidth", label: "Card width", min: 1, max: 60 },
  { key: "draftBadgeHeight", label: "Card height", min: 1, max: 60 },
  { key: "draftBadgeGapX", label: "Horizontal gap", min: 0, max: 20 },
  { key: "draftBadgeGapY", label: "Vertical gap", min: 0, max: 20 },
  { key: "draftBadgeColumns", label: "Columns", min: 1, max: 15 },
] as const;

export function OverlaySettingsPanel({ id, onStatusChange }: { id?: string; onStatusChange?: (status: "idle" | "saving" | "error") => void }) {
  const queryClient = useQueryClient();
  const query = useQuery({ queryKey: ["overlay-settings"], queryFn: api.overlaySettings });
  const [draft, setDraft] = useState<OverlaySettings | null>(null);
  const settings = draft ?? query.data ?? DEFAULT_OVERLAY_SETTINGS;
  const latest = useRef<OverlaySettings | null>(null);
  const persisted = useRef<OverlaySettings | null>(null);
  const queryData = useRef(query.data);
  queryData.current = query.data;
  const queue = useRef<OverlaySaveQueue | null>(null);
  const [status, setStatus] = useState<"idle" | "saving" | "saved" | "error">("idle");
  const [error, setError] = useState("");
  const [shortcutDraft, setShortcutDraft] = useState<string | null>(null);
  const undoPending = useRef(false);
  const [undo, setUndo] = useState<OverlaySettings | null>(null);
  useEffect(() => {
    if (status !== "saved") return;
    const timer = window.setTimeout(() => setStatus("idle"), 2000);
    return () => window.clearTimeout(timer);
  }, [status]);
  useEffect(() => {
    onStatusChange?.(status === "saved" ? "idle" : status);
  }, [status]);
  if (!queue.current) queue.current = new OverlaySaveQueue(async (request) => {
    const value = { ...request.settings, shortcut: request.shortcut ? request.settings.shortcut : (persisted.current ?? queryData.current ?? DEFAULT_OVERLAY_SETTINGS).shortcut };
    await queryClient.cancelQueries({ queryKey: ["overlay-settings"] });
    const existing = persisted.current ?? queryData.current;
    const saved = existing && JSON.stringify(existing) === JSON.stringify(value) ? existing : await api.saveOverlaySettings(value);
    persisted.current = saved;
    if (undoPending.current) { setUndo(null); undoPending.current = false; }
    queryClient.setQueryData(["overlay-settings"], saved);
    if (latest.current) {
      latest.current = { ...latest.current, shortcut: saved.shortcut };
      setDraft(latest.current);
    }
  }, (cause) => {
    setError(cause instanceof Error ? cause.message : "Could not save overlay settings.");
    setStatus("error");
  }, () => { setStatus("saving"); setError(""); }, () => setStatus("saved"));
  const enqueue = (value: OverlaySettings, shortcut = false) => queue.current!.enqueue({ settings: value, shortcut });
  const change = <K extends keyof OverlaySettings>(key: K, value: OverlaySettings[K], commit = true) => {
    const next = { ...(latest.current ?? settings), [key]: value };
    latest.current = next;
    setDraft(next);
    if (commit) enqueue(next);
    else if (status !== "saving" && status !== "error") setStatus("idle");
  };
  const commitSlider = () => enqueue(latest.current ?? settings);
  const replaceSettings = (value: OverlaySettings) => {
    latest.current = { ...value };
    setDraft(latest.current);
    setShortcutDraft(value.shortcut);
    enqueue(value, true);
  };
  const shortcut = shortcutDraft ?? settings.shortcut;
  const savedSettings = persisted.current ?? query.data;
  const unsaved = Boolean(savedSettings && JSON.stringify({ ...settings, shortcut: savedSettings.shortcut }) !== JSON.stringify(savedSettings));
  const previewStyle = { "--sample-opacity": settings.opacity, "--sample-width": `${overlayPanelWidth(settings.panelSize)}rem`, "--sample-font-size": `${{ compact: .6875, default: .75, large: .875 }[settings.panelSize]}rem`, "--sample-row-height": `${{ compact: 1.75, default: 2, large: 2.3 }[settings.panelSize]}rem` } as CSSProperties;

  const patch = (values: Partial<OverlaySettings>) => {
    const next = { ...(latest.current ?? settings), ...values };
    latest.current = next;
    setDraft(next);
    enqueue(next);
  };
  const saveShortcut = (value: string) => {
    setShortcutDraft(value);
    enqueue({ ...(latest.current ?? settings), shortcut: value }, true);
  };
  const delayOptions = [0, 250, 500, 1000];
  if (!delayOptions.includes(settings.hoverDelayMs)) delayOptions.push(settings.hoverDelayMs);
  delayOptions.sort((a, b) => a - b);
  const sliderEvents = { onPointerUp: commitSlider, onPointerCancel: commitSlider, onKeyUp: commitSlider, onBlur: commitSlider };
  const statusText = status === "saving" ? "Saving…" : status === "error" ? "" : unsaved ? "Release slider to save" : "";

  return (
    <section id={id} className="settings-section overlay-settings" aria-labelledby="overlay-settings-title">
      <div className="settings-section-head">
        <h3 id="overlay-settings-title">Overlay</h3>
        <p>The in-game deck tracker and draft ratings.</p>
        {query.data ? (
          <div className="settings-section-actions">
            <span className="settings-save-status" role="status" aria-live="polite">{statusText}</span>
            {undo ? <button type="button" className="settings-text-button" disabled={status === "saving"} onClick={() => { undoPending.current = true; replaceSettings(undo); }}>Undo reset</button> : null}
            <button type="button" className="settings-text-button" disabled={status === "saving"} onClick={() => { undoPending.current = false; setUndo({ ...(persisted.current ?? query.data ?? DEFAULT_OVERLAY_SETTINGS) }); replaceSettings(DEFAULT_OVERLAY_SETTINGS); }}>Reset to defaults</button>
          </div>
        ) : null}
      </div>
      {query.isPending ? <StatusMessage>Loading overlay settings…</StatusMessage> : query.isError ? (
        <><StatusMessage tone="error">{(query.error as Error).message}</StatusMessage><button className="settings-button" type="button" onClick={() => void query.refetch()}>Try again</button></>
      ) : (
        <>
          {status === "error" ? (
            <StatusMessage tone="error">
              Overlay settings could not be saved: {error}{" "}
              <button type="button" className="settings-text-button" onClick={() => void queue.current!.drain()}>Retry</button>
            </StatusMessage>
          ) : null}
          <div className="overlay-settings-fields">
            <div className="panel settings-card">
              <h4 className="settings-group-label">Show during a match</h4>
              <ToggleRow compact label="Mana summary" checked={settings.showMana} onChange={(value) => change("showMana", value)} />
              <ToggleRow compact label="Your deck" checked={settings.showDeck} onChange={(value) => change("showDeck", value)} />
              <ToggleRow compact label="Opponent cards" checked={settings.showOpponent} onChange={(value) => change("showOpponent", value)} />

              <h4 className="settings-group-label has-rule">Show while drafting</h4>
              <ToggleRow compact label="17Lands ratings" checked={settings.showDraft} onChange={(value) => change("showDraft", value)} />
              <ToggleRow compact label="Ratings on the cards" help="Experimental. Assumes Arena’s default rarity order and 16:9 draft layout, and needs local Arena card data."
                checked={settings.draftCardBadges} onChange={(value) => change("draftCardBadges", value)} />
              <details className="settings-expander overlay-badge-calibration">
                <summary>
                  <span>Adjust card positions</span>
                  <small>If the ratings do not line up with the cards</small>
                </summary>
                <div className="settings-expander-body">
                  {badgeCalibrationFields.map(({ key, label, min, max }) => (
                    <SettingRow compact key={key} label={label} htmlFor={`overlay-${key}`}>
                      <input id={`overlay-${key}`} type="range" min={min} max={max} step={key === "draftBadgeColumns" ? 1 : 0.05} value={settings[key]} disabled={!settings.draftCardBadges} onChange={(event) => change(key, Number(event.target.value), false)} {...sliderEvents} />
                      <span className="settings-range-value">{settings[key]}{key === "draftBadgeColumns" ? "" : "%"}</span>
                    </SettingRow>
                  ))}
                  <SettingRow compact label={<span className="settings-row-help">Positions and sizes are a percentage of the game window.</span>}>
                    <button type="button" className="settings-button" disabled={status === "saving"} onClick={() => {
                      const next = { ...(latest.current ?? settings) };
                      for (const { key } of badgeCalibrationFields) next[key] = DEFAULT_OVERLAY_SETTINGS[key];
                      latest.current = next;
                      setDraft(next);
                      enqueue(next);
                    }}>Reset positions</button>
                  </SettingRow>
                </div>
              </details>

              <SettingRow label={<span id="overlay-size-label">Panel size</span>}>
                <div className="settings-segmented" role="radiogroup" aria-labelledby="overlay-size-label">
                  {(["compact", "default", "large"] as const).map((size) => (
                    <button key={size} type="button" role="radio" aria-checked={settings.panelSize === size}
                      className={`settings-segment${settings.panelSize === size ? " is-active" : ""}`}
                      onClick={() => change("panelSize", size)}>
                      {size[0].toUpperCase() + size.slice(1)}
                    </button>
                  ))}
                </div>
              </SettingRow>
              <SettingRow label="Background opacity" htmlFor="overlay-opacity">
                <input id="overlay-opacity" type="range" min="30" max="100" step="5" value={Math.round(settings.opacity * 100)} onChange={(event) => change("opacity", Number(event.target.value) / 100, false)} {...sliderEvents} />
                <span className="settings-range-value">{Math.round(settings.opacity * 100)}%</span>
              </SettingRow>
              <SettingRow label="Card preview on hover" htmlFor="overlay-preview">
                <select id="overlay-preview" className="settings-select" value={settings.cardPreviews ? String(settings.hoverDelayMs) : "off"}
                  onChange={(event) => patch(event.target.value === "off" ? { cardPreviews: false } : { cardPreviews: true, hoverDelayMs: Number(event.target.value) })}>
                  {delayOptions.map((ms) => <option key={ms} value={String(ms)}>{ms === 0 ? "Instantly" : `After ${ms} ms`}</option>)}
                  <option value="off">Off</option>
                </select>
              </SettingRow>
              <SettingRow label="Show or hide shortcut" help="Click the keys, then press a modifier with a letter or digit.">
                <ShortcutField value={shortcut} disabled={status === "saving"} onChange={saveShortcut} />
              </SettingRow>
            </div>
            <div className="overlay-settings-preview" style={previewStyle}>
              <span className="settings-group-label">Live preview</span>
              <div className="overlay-settings-samples">
                {settings.showDeck ? <SamplePanel title="Your deck" opponent={false} previews={settings.cardPreviews} delay={settings.hoverDelayMs} /> : null}
                {settings.showOpponent ? <SamplePanel title="Opponent cards" opponent previews={settings.cardPreviews} delay={settings.hoverDelayMs} /> : null}
                {settings.showMana && <OverlayManaPanel mana={{ landsKnown: true, lands: 5, available: 4, byColor: { U: 3, B: 2 }, floating: 0, poolKnown: true, unresolved: 0 }} />}
                {!settings.showDeck && !settings.showOpponent && !settings.showMana ? <p className="settings-row-help">All match panels are hidden.</p> : null}
              </div>
              <small className="overlay-settings-preview-note">Sample cards. Hover one to try the preview.</small>
            </div>
          </div>
        </>
      )}
    </section>
  );
}

/** A button that records the next key combination as the overlay shortcut. */
function ShortcutField({ value, disabled, onChange }: { value: string; disabled?: boolean; onChange: (value: string) => void }) {
  const [recording, setRecording] = useState(false);
  const [needsModifier, setNeedsModifier] = useState(false);
  const mac = typeof navigator !== "undefined" && navigator.platform.startsWith("Mac");
  const stop = () => { setRecording(false); setNeedsModifier(false); };
  return (
    <button type="button" className={`settings-shortcut${recording ? " is-recording" : ""}`} disabled={disabled}
      aria-label={recording ? "Press a new shortcut, or Escape to cancel" : `Change shortcut, currently ${value.split("+").join(" ")}`}
      onClick={() => (recording ? stop() : setRecording(true))}
      onBlur={stop}
      onKeyDown={(event) => {
        if (!recording || event.key === "Tab") return;
        event.preventDefault();
        event.stopPropagation();
        if (event.key === "Escape") { stop(); return; }
        const key = /^(?:Key([A-Z])|Digit([0-9]))$/.exec(event.code);
        if (!key) return;
        const modifiers: string[] = [];
        if (mac ? event.metaKey : event.ctrlKey) modifiers.push("CmdOrCtrl");
        if (mac && event.ctrlKey) modifiers.push("Ctrl");
        if (!mac && event.metaKey) modifiers.push("Super");
        if (event.altKey) modifiers.push("Alt");
        if (event.shiftKey) modifiers.push("Shift");
        if (!modifiers.length) { setNeedsModifier(true); return; }
        stop();
        const next = [...modifiers, key[1] ?? key[2]].join("+");
        if (next !== value) onChange(next);
      }}>
      {recording
        ? <span>{needsModifier ? "Add a modifier key…" : "Press keys…"}</span>
        : value.split("+").map((part) => <kbd key={part}>{overlayShortcutLabel(part, mac)}</kbd>)}
    </button>
  );
}

function SamplePanel({ title, opponent, previews, delay }: { title: string; opponent: boolean; previews: boolean; delay: number }) {
  return <div className="overlay-settings-sample">
    <strong>{title}</strong>
    <small>{opponent ? "3 cards seen" : "Library · 53 cards"}</small>
    <SampleCard name={opponent ? "Lightning Strike" : "Llanowar Elves"} quantity={4} mana={opponent ? ["1", "R"] : ["G"]} previews={previews} delay={delay} />
    <SampleCard name={opponent ? "Monastery Swiftspear" : "Elvish Archdruid"} quantity={3} mana={opponent ? ["R"] : ["1", "G", "G"]} previews={previews} delay={delay} />
    {!opponent ? <div className="overlay-settings-sample-menus"><span>Lands <span>›</span></span><span>Sideboard <span>›</span></span></div> : null}
  </div>;
}

function SampleCard({ name, quantity, mana, previews, delay }: {
  name: string; quantity: number; mana: string[]; previews: boolean; delay: number;
}) {
  const [hovered, setHovered] = useState(false);
  const [focused, setFocused] = useState(false);
  const [ready, setReady] = useState(false);
  const active = hovered || focused;
  useEffect(() => {
    setReady(false);
    if (!previews || !active) return;
    const timer = window.setTimeout(() => setReady(true), delay);
    return () => window.clearTimeout(timer);
  }, [previews, active, delay]);

  return (
    <div className="overlay-settings-sample-card" tabIndex={previews ? 0 : undefined}
      onMouseEnter={() => setHovered(true)} onMouseLeave={() => setHovered(false)}
      onFocus={() => setFocused(true)} onBlur={() => setFocused(false)}>
      <span>{quantity}</span>
      <CardPreviewName cardId={0} cardName={name} label={<span>{name}</span>}
        passiveHover={previews && active && ready} />
      <span className="overlay-settings-mana">{mana.map((token, index) => <ManaSymbol key={index} token={token} />)}</span>
    </div>
  );
}
