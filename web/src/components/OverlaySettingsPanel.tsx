import { useEffect, useState, type CSSProperties } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../lib/api";
import { DEFAULT_OVERLAY_SETTINGS, overlayPanelWidth, overlayShortcutLabel, type OverlaySettings } from "../lib/overlaySettings";
import { StatusMessage } from "./StatusMessage";
import { CardPreviewName } from "./CardPreviewName";
import { ManaSymbol } from "./ManaSymbol";
import "./OverlaySettingsPanel.css";

export function OverlaySettingsPanel() {
  const queryClient = useQueryClient();
  const query = useQuery({ queryKey: ["overlay-settings"], queryFn: api.overlaySettings });
  const [draft, setDraft] = useState<OverlaySettings | null>(null);
  const settings = draft ?? query.data ?? DEFAULT_OVERLAY_SETTINGS;
  const save = useMutation({
    mutationFn: api.saveOverlaySettings,
    onSuccess: (saved) => {
      queryClient.setQueryData(["overlay-settings"], saved);
      setDraft(null);
    },
  });
  const change = <K extends keyof OverlaySettings>(key: K, value: OverlaySettings[K]) => {
    save.reset();
    setDraft({ ...settings, [key]: value });
  };
  const dirty = Boolean(query.data && JSON.stringify(settings) !== JSON.stringify(query.data));
  const previewStyle = { "--sample-opacity": settings.opacity, "--sample-width": `${overlayPanelWidth(settings.panelSize)}rem`, "--sample-font-size": `${{ compact: .6875, default: .75, large: .875 }[settings.panelSize]}rem`, "--sample-row-height": `${{ compact: 1.75, default: 2, large: 2.3 }[settings.panelSize]}rem` } as CSSProperties;

  return (
    <section className="panel overlay-settings" aria-labelledby="overlay-settings-title">
      <div className="panel-head panel-head--stacked">
        <h3 id="overlay-settings-title">Overlay</h3>
        <p>Adjust the deck tracker shown during gameplay. Changes apply when you save.</p>
      </div>
      {query.isPending ? <StatusMessage>Loading overlay settings…</StatusMessage> : query.isError ? (
        <><StatusMessage tone="error">{(query.error as Error).message}</StatusMessage><button className="control-button" type="button" onClick={() => void query.refetch()}>Try again</button></>
      ) : (
        <form onSubmit={(event) => { event.preventDefault(); save.mutate(settings); }}>
          <fieldset className="overlay-settings-fields" disabled={save.isPending}>
            <div className="overlay-settings-controls">
              <div>
                <label className="settings-checkbox"><input type="checkbox" checked={settings.showDeck} onChange={(event) => change("showDeck", event.target.checked)} /><span>Show your deck</span></label>
                <label className="settings-checkbox"><input type="checkbox" checked={settings.showOpponent} onChange={(event) => change("showOpponent", event.target.checked)} /><span>Show opponent cards</span></label>
              </div>
              <label className="settings-field"><span>Panel size</span><select className="settings-input" value={settings.panelSize} onChange={(event) => change("panelSize", event.target.value as OverlaySettings["panelSize"])}><option value="compact">Compact</option><option value="default">Default</option><option value="large">Large</option></select></label>
              <label className="settings-field"><span>Background opacity · {Math.round(settings.opacity * 100)}%</span><input type="range" min="30" max="100" step="5" value={Math.round(settings.opacity * 100)} onChange={(event) => change("opacity", Number(event.target.value) / 100)} /></label>
              <div>
                <label className="settings-checkbox"><input type="checkbox" checked={settings.cardPreviews} onChange={(event) => change("cardPreviews", event.target.checked)} /><span>Show card previews on hover</span></label>
                <label className="settings-field overlay-settings-delay"><span>Preview delay · {settings.hoverDelayMs} ms</span><input type="range" min="0" max="1500" step="50" disabled={!settings.cardPreviews} value={settings.hoverDelayMs} onChange={(event) => change("hoverDelayMs", Number(event.target.value))} /><small>A longer delay helps avoid accidental previews.</small></label>
              </div>
              <label className="settings-field"><span>Show / hide shortcut</span><input className="settings-input" value={settings.shortcut} maxLength={80} required autoComplete="off" spellCheck={false} onChange={(event) => change("shortcut", event.target.value.replace(/\+[a-z]$/, (key) => key.toUpperCase()))} aria-describedby="overlay-shortcut-help" /><small id="overlay-shortcut-help">Use CmdOrCtrl, Cmd, Ctrl, Alt, Shift, or Super with a letter or digit, such as CmdOrCtrl+Shift+O. Preview: {overlayShortcutLabel(settings.shortcut)}</small></label>
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
            <button type="submit" className="control-button" disabled={!dirty || save.isPending}>{save.isPending ? "Saving…" : "Save overlay settings"}</button>
            <button type="button" className="control-button" disabled={save.isPending} onClick={() => { save.reset(); setDraft({ ...DEFAULT_OVERLAY_SETTINGS }); }}>Reset to defaults</button>
          </div>
          {save.error ? <StatusMessage tone="error">{(save.error as Error).message}</StatusMessage> : save.isSuccess ? <StatusMessage>Overlay settings saved.</StatusMessage> : dirty ? <p className="settings-note">You have unsaved changes.</p> : null}
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
