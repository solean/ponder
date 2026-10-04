import { useEffect, useRef, useState, type CSSProperties } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { OverlaySaveQueue } from "../lib/overlaySaveQueue";
import { api } from "../lib/api";
import { DEFAULT_OVERLAY_SETTINGS, overlayPanelWidth, overlayShortcutLabel, type OverlaySettings } from "../lib/overlaySettings";
import { StatusMessage } from "./StatusMessage";
import { CardPreviewName } from "./CardPreviewName";
import { ManaSymbol } from "./ManaSymbol";
import { SettingToggle } from "./SettingToggle";
import "./OverlaySettingsPanel.css";

export function OverlaySettingsPanel() {
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
  const shortcutDirty = shortcut !== (persisted.current ?? query.data ?? DEFAULT_OVERLAY_SETTINGS).shortcut;
  const savedSettings = persisted.current ?? query.data;
  const unsaved = Boolean(savedSettings && JSON.stringify({ ...settings, shortcut: savedSettings.shortcut }) !== JSON.stringify(savedSettings));
  const previewStyle = { "--sample-opacity": settings.opacity, "--sample-width": `${overlayPanelWidth(settings.panelSize)}rem`, "--sample-font-size": `${{ compact: .6875, default: .75, large: .875 }[settings.panelSize]}rem`, "--sample-row-height": `${{ compact: 1.75, default: 2, large: 2.3 }[settings.panelSize]}rem` } as CSSProperties;

  return (
    <section className="panel overlay-settings" aria-labelledby="overlay-settings-title">
      <div className="panel-head panel-head--stacked">
        <h3 id="overlay-settings-title">Overlay</h3>
        <p>Adjust the deck tracker shown during gameplay. Changes save automatically. Apply shortcut changes separately.</p>
      </div>
      {query.isPending ? <StatusMessage>Loading overlay settings…</StatusMessage> : query.isError ? (
        <><StatusMessage tone="error">{(query.error as Error).message}</StatusMessage><button className="control-button" type="button" onClick={() => void query.refetch()}>Try again</button></>
      ) : (
        <form onSubmit={(event) => { event.preventDefault(); if (shortcutDirty && !queue.current!.running) enqueue({ ...(latest.current ?? settings), shortcut }, true); }}>
          <fieldset className="overlay-settings-fields">
            <div className="overlay-settings-controls">
              <div>
                <SettingToggle checked={settings.showDeck} onChange={(value) => change("showDeck", value)}>Show your deck</SettingToggle>
                <SettingToggle checked={settings.showOpponent} onChange={(value) => change("showOpponent", value)}>Show opponent cards</SettingToggle>
              </div>
              <label className="settings-field"><span>Panel size</span><select className="settings-input" value={settings.panelSize} onChange={(event) => change("panelSize", event.target.value as OverlaySettings["panelSize"])}><option value="compact">Compact</option><option value="default">Default</option><option value="large">Large</option></select></label>
              <label className="settings-field"><span>Background opacity · {Math.round(settings.opacity * 100)}%</span><input type="range" min="30" max="100" step="5" value={Math.round(settings.opacity * 100)} onChange={(event) => change("opacity", Number(event.target.value) / 100, false)} onPointerUp={commitSlider} onPointerCancel={commitSlider} onKeyUp={commitSlider} onBlur={commitSlider} /></label>
              <div>
                <SettingToggle checked={settings.cardPreviews} onChange={(value) => change("cardPreviews", value)}>Show card previews on hover</SettingToggle>
                <label className="settings-field overlay-settings-delay"><span>Preview delay · {settings.hoverDelayMs} ms</span><input type="range" min="0" max="1500" step="50" disabled={!settings.cardPreviews} value={settings.hoverDelayMs} onChange={(event) => change("hoverDelayMs", Number(event.target.value), false)} onPointerUp={commitSlider} onPointerCancel={commitSlider} onKeyUp={commitSlider} onBlur={commitSlider} /><small>A longer delay helps avoid accidental previews.</small></label>
              </div>
              <div className="settings-field"><label htmlFor="overlay-shortcut">Show / hide shortcut</label><input id="overlay-shortcut" className="settings-input" value={shortcut} maxLength={80} required autoComplete="off" spellCheck={false} onChange={(event) => setShortcutDraft(event.target.value.replace(/\+[a-z]$/, (key) => key.toUpperCase()))} aria-describedby="overlay-shortcut-help" /><small id="overlay-shortcut-help">Use CmdOrCtrl, Cmd, Ctrl, Alt, Shift, or Super with a letter or digit, such as CmdOrCtrl+Shift+O. Preview: {overlayShortcutLabel(shortcut)}</small><button type="submit" className="control-button" disabled={!shortcutDirty || status === "saving"}>Apply shortcut</button></div>
            </div>
            <div className="overlay-settings-preview" style={previewStyle}>
              <span className="overlay-settings-preview-label">Preview</span>
              <div className="overlay-settings-samples">
                {settings.showDeck ? <SamplePanel title="Your deck" opponent={false} previews={settings.cardPreviews} delay={settings.hoverDelayMs} /> : null}
                {settings.showOpponent ? <SamplePanel title="Opponent cards" opponent previews={settings.cardPreviews} delay={settings.hoverDelayMs} /> : null}
                {!settings.showDeck && !settings.showOpponent ? <p className="settings-note">Both panels are hidden.</p> : null}
              </div>
              <small className="overlay-settings-preview-note">Sample cards · Hover a card to try the preview.</small>
            </div>
          </fieldset>
          <div className="settings-action-row">
            <button type="button" className="control-button" disabled={status === "saving"} onClick={() => { undoPending.current = false; setUndo({ ...(persisted.current ?? query.data ?? DEFAULT_OVERLAY_SETTINGS) }); replaceSettings(DEFAULT_OVERLAY_SETTINGS); }}>Reset to defaults</button>
            {undo ? <button type="button" className="control-button" disabled={status === "saving"} onClick={() => { undoPending.current = true; replaceSettings(undo); }}>Undo reset</button> : null}
            <span className="settings-note" role="status" aria-live="polite">{status === "saving" ? "Saving…" : status === "error" ? "" : unsaved ? "Release slider to save" : status === "saved" ? "Saved" : ""}</span>
          </div>
          {status === "error" ? <div><StatusMessage tone="error">{error}</StatusMessage><button type="button" className="control-button" onClick={() => void queue.current!.drain()}>Retry</button></div> : null}
        </form>
      )}
    </section>
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
