import { useEffect, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { LoadingLabel, LoadingMessage } from "../components/LoadingMessage";
import { SettingRow, ToggleRow } from "../components/SettingRow";
import { PageHeader } from "../components/PageHeader";
import { StatusMessage } from "../components/StatusMessage";
import { OverlaySettingsPanel } from "../components/OverlaySettingsPanel";
import { AppIconPicker } from "../components/AppIconPicker";
import { api } from "../lib/api";
import { RuntimeConfigSaveQueue } from "../lib/runtimeConfigSaveQueue";
import { copyTextToClipboard } from "../lib/clipboard";
import { APP_NAME } from "../lib/branding";
import { SEVENTEEN_LANDS_FORMAT_LABELS, SEVENTEEN_LANDS_USER_GROUP_LABELS } from "../lib/seventeenLands";
import { formatBytes, formatRelativeTime, formatShortDateTime, shortenHomePath } from "../lib/format";
import { CHART_THEME_TOKENS } from "../lib/chartTheme";
import { COLOR_SCHEMES, SCHEME_LABELS, THEME_COLORS, useThemeControls, type ModePreference } from "../lib/theme";
import type {
  AiModelOption,
  AiProvider,
  AiProviderStatus,
  RuntimeConfig,
  RuntimeOperation,
  RuntimeStatus,
  SeventeenLandsFormat,
  SeventeenLandsUserGroup,
  UpdateCheck,
} from "../lib/types";

function CopyPathButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (!copied) {
      return;
    }
    const timer = window.setTimeout(() => setCopied(false), 1500);
    return () => window.clearTimeout(timer);
  }, [copied]);

  const copy = async () => {
    try {
      await copyTextToClipboard(text);
      setCopied(true);
    } catch {
      // Leave the button unchanged when both clipboard mechanisms are unavailable.
    }
  };

  return (
    <button type="button" className={`settings-button${copied ? " is-done" : ""}`} onClick={() => void copy()}>
      {copied ? "Copied" : "Copy path"}
    </button>
  );
}

function RevealButton({ path }: { path: string }) {
  const revealMutation = useMutation({ mutationFn: () => api.revealPath(path) });
  const failed = Boolean(revealMutation.error);
  return (
    <button
      type="button"
      className={`settings-button${failed ? " is-failed" : ""}`}
      onClick={() => revealMutation.mutate()}
      disabled={revealMutation.isPending}
      title={failed ? `Could not show the file: ${(revealMutation.error as Error).message}` : undefined}
    >
      Show in Finder
    </button>
  );
}

/** A file the app uses: its name, its path, and buttons to copy or reveal it. */
function PathRow({ label, detail, path, canReveal }: { label: string; detail?: string; path: string; canReveal: boolean }) {
  return (
    <SettingRow
      label={
        <>
          {label}
          {detail ? <span className="settings-row-detail">{detail}</span> : null}
        </>
      }
      help={
        <code className="settings-path" title={path}>
          {shortenHomePath(path)}
        </code>
      }
    >
      <CopyPathButton text={path} />
      {canReveal ? <RevealButton path={path} /> : null}
    </SettingRow>
  );
}

function Chevron() {
  return (
    <svg className="settings-chevron" viewBox="0 0 12 12" fill="none" stroke="currentColor" strokeWidth="1.6" aria-hidden="true">
      <path d="M4 2 8 6 4 10" />
    </svg>
  );
}

function MoonIcon() {
  return (
    <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.4" aria-hidden="true">
      <path d="M13.5 9.5A5.75 5.75 0 0 1 6.5 2.5a5.75 5.75 0 1 0 7 7Z" strokeLinejoin="round" />
    </svg>
  );
}

function SunIcon() {
  return (
    <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.4" aria-hidden="true">
      <circle cx="8" cy="8" r="3" />
      <path d="M8 1.2v1.8M8 13v1.8M1.2 8H3M13 8h1.8M3.2 3.2l1.3 1.3M11.5 11.5l1.3 1.3M12.8 3.2l-1.3 1.3M4.5 11.5l-1.3 1.3" strokeLinecap="round" />
    </svg>
  );
}

function MonitorIcon() {
  return (
    <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.4" aria-hidden="true">
      <rect x="1.8" y="2.8" width="12.4" height="8.4" rx="1" />
      <path d="M6 14h4M8 11.2V14" strokeLinecap="round" />
    </svg>
  );
}

const modeOptions: Array<{ value: ModePreference; label: string; icon: () => JSX.Element }> = [
  { value: "dark", label: "Dark", icon: MoonIcon },
  { value: "light", label: "Light", icon: SunIcon },
  { value: "system", label: "System", icon: MonitorIcon },
];

const sections = [
  { id: "settings-tracking", label: "Tracking" },
  { id: "settings-overlay", label: "Overlay" },
  { id: "settings-ratings", label: "Draft ratings" },
  { id: "settings-ai", label: "AI" },
  { id: "settings-appearance", label: "Appearance" },
  { id: "settings-app", label: "App and data" },
] as const;

/**
 * Tracks which section is at the top of the scrolling page. Sections near the
 * end cannot reach the top, so a section chosen from the list stays current
 * until the reader scrolls by hand.
 */
function useActiveSection(ready: boolean): [string, (id: string) => void] {
  const [active, setActive] = useState<string>(sections[0].id);
  const chosen = useRef<string | null>(null);
  useEffect(() => {
    if (!ready) return;
    const root = document.querySelector<HTMLElement>(".app-scroll");
    if (!root) return;
    let frame = 0;
    const update = () => {
      frame = 0;
      if (chosen.current) return;
      const line = root.getBoundingClientRect().top + 96;
      let current: string = sections[0].id;
      for (const section of sections) {
        const element = document.getElementById(section.id);
        if (element && element.getBoundingClientRect().top <= line) current = section.id;
      }
      setActive(current);
    };
    const onScroll = () => {
      if (!frame) frame = window.requestAnimationFrame(update);
    };
    const release = () => {
      chosen.current = null;
      onScroll();
    };
    update();
    root.addEventListener("scroll", onScroll, { passive: true });
    root.addEventListener("wheel", release, { passive: true });
    root.addEventListener("touchmove", release, { passive: true });
    root.addEventListener("pointerdown", release, { passive: true });
    window.addEventListener("keydown", release);
    return () => {
      root.removeEventListener("scroll", onScroll);
      root.removeEventListener("wheel", release);
      root.removeEventListener("touchmove", release);
      root.removeEventListener("pointerdown", release);
      window.removeEventListener("keydown", release);
      if (frame) window.cancelAnimationFrame(frame);
    };
  }, [ready]);
  const choose = (id: string) => {
    chosen.current = id;
    setActive(id);
    const element = document.getElementById(id);
    if (!element) return;
    // Jump straight there: smooth scrolling sometimes did not move the page at all.
    const root = document.querySelector<HTMLElement>(".app-scroll");
    if (root) {
      const offset = element.getBoundingClientRect().top - root.getBoundingClientRect().top;
      root.scrollTo({ top: id === sections[0].id ? 0 : root.scrollTop + offset - 16, behavior: "auto" });
    }
    // Mark the section, since one near the end may not move to the top.
    for (const marked of document.querySelectorAll(".settings-section.is-target")) marked.classList.remove("is-target");
    void element.offsetWidth;
    element.classList.add("is-target");
  };
  return [active, choose];
}

const runtimeStatusKey = ["runtime-status"] as const;
const autostartKey = ["autostart-status"] as const;
const aiStatusKey = ["ai-status"] as const;

const aiUsageNumberFormatter = new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 });

const aiUsageCompactFormatter = new Intl.NumberFormat(undefined, { notation: "compact", maximumSignificantDigits: 3 });

function formatTokenCount(value: number): string {
  return `${aiUsageNumberFormatter.format(value)} ${value === 1 ? "token" : "tokens"}`;
}

function providerState(provider: AiProviderStatus): { label: string; tone: "positive" | "negative" | "neutral" } {
  if (provider.available) return { label: "Ready", tone: "positive" };
  if (provider.installed && provider.version) return { label: "Sign-in needed", tone: "negative" };
  if (provider.installed) return { label: "CLI error", tone: "negative" };
  return { label: "Not installed", tone: "neutral" };
}

const fallbackAIProviders: AiProviderStatus[] = [
  {
    id: "claude",
    name: "Claude Code",
    installed: false,
    authenticated: false,
    available: false,
    models: [
      { id: "opus", name: "Opus", description: "Highest-quality Claude model alias." },
      { id: "sonnet", name: "Sonnet", description: "Balanced Claude model alias." },
      { id: "haiku", name: "Haiku", description: "Fastest Claude model alias." },
    ],
  },
  {
    id: "openai",
    name: "OpenAI (Codex)",
    installed: false,
    authenticated: false,
    available: false,
    models: [
      {
        id: "default",
        name: "Codex default",
        description: "Tracks the default model selected by your installed Codex CLI.",
      },
    ],
  },
];

function modelsForProvider(
  providers: AiProviderStatus[],
  providerID: AiProvider,
  selectedModel: string,
): AiModelOption[] {
  const models = [...(providers.find((provider) => provider.id === providerID)?.models ?? [])];
  if (selectedModel && !models.some((model) => model.id === selectedModel)) {
    models.push({ id: selectedModel, name: selectedModel, description: "Model saved in your current configuration." });
  }
  return models;
}


function summarizeUpdateCheck(result: UpdateCheck): string {
  if (result.note) {
    return result.note;
  }
  if (result.updateAvailable && result.latestVersion) {
    return `Update available: ${result.latestVersion} (current ${result.currentVersion})`;
  }
  return `Up to date (${result.currentVersion})`;
}

function formatRuntimeDuration(durationMs: number): string {
  if (!Number.isFinite(durationMs) || durationMs <= 0) {
    return "-";
  }
  const seconds = Math.max(1, Math.round(durationMs / 1000));
  const minutes = Math.floor(seconds / 60);
  const remainder = seconds % 60;
  return minutes > 0 ? `${minutes}m ${remainder}s` : `${remainder}s`;
}

function summarizeOperation(operation?: RuntimeOperation): string {
  if (!operation) {
    return "No runs yet.";
  }
  return [
    `${operation.matchesUpserted} matches`,
    `${operation.decksUpserted} decks`,
    `${operation.draftPicksAdded} picks`,
    formatRuntimeDuration(operation.durationMs),
  ].join(" · ");
}

async function refreshDataQueries(queryClient: ReturnType<typeof useQueryClient>) {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: runtimeStatusKey }),
    queryClient.invalidateQueries({ queryKey: ["overview"] }),
    queryClient.invalidateQueries({ queryKey: ["rank-history"] }),
    queryClient.invalidateQueries({ queryKey: ["matches"] }),
    queryClient.invalidateQueries({ queryKey: ["match-detail"] }),
    queryClient.invalidateQueries({ queryKey: ["match-timeline"] }),
    queryClient.invalidateQueries({ queryKey: ["match-replay"] }),
    queryClient.invalidateQueries({ queryKey: ["match-replay-status"] }),
    queryClient.invalidateQueries({ queryKey: ["decks"] }),
    queryClient.invalidateQueries({ queryKey: ["drafts"] }),
    queryClient.invalidateQueries({ queryKey: ["draft-analytics"] }),
    queryClient.invalidateQueries({ queryKey: ["analytics-games"] }),
  ]);
}

function normalizePollInterval(value: string): number {
  const parsed = Number.parseInt(value, 10);
  if (!Number.isFinite(parsed) || parsed <= 0) {
    return 2;
  }
  return parsed;
}

function syncForm(status: RuntimeStatus): RuntimeConfig {
  return {
    appIcon: status.config.appIcon ?? "white",
    logPath: status.config.logPath ?? "",
    pollIntervalSeconds: status.config.pollIntervalSeconds,
    includePrev: status.config.includePrev,
    autoStartLive: status.config.autoStartLive ?? true,
    autoCheckUpdates: status.config.autoCheckUpdates ?? false,
    aiProvider: status.config.aiProvider ?? "claude",
    aiModel: status.config.aiModel ?? "opus",
    seventeenLandsUserGroup: status.config.seventeenLandsUserGroup ?? "",
    seventeenLandsFormat: status.config.seventeenLandsFormat ?? "",
  };
}

export function SettingsPage() {
  const queryClient = useQueryClient();
  const { mode, modePreference, setModePreference, scheme, setScheme } = useThemeControls();
  const { data, isLoading, error } = useQuery({
    queryKey: runtimeStatusKey,
    queryFn: api.runtimeStatus,
    refetchInterval: 3000,
  });
  const aiStatusQuery = useQuery({
    queryKey: aiStatusKey,
    queryFn: api.aiStatus,
    staleTime: 30_000,
  });

  const [form, setForm] = useState<RuntimeConfig>({
    appIcon: "white",
    logPath: "",
    pollIntervalSeconds: 2,
    includePrev: true,
    autoStartLive: true,
    autoCheckUpdates: false,
    aiProvider: "claude",
    aiModel: "opus",
    seventeenLandsUserGroup: "",
    seventeenLandsFormat: "",
  });
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState("");
  const [pathError, setPathError] = useState("");
  const [savedFlash, setSavedFlash] = useState(false);
  const [dismissedError, setDismissedError] = useState("");
  const saveQueue = useRef(new RuntimeConfigSaveQueue(form)).current;
  const logDraft = useRef<string | null>(null);
  const [pathDirty, setPathDirty] = useState(false);
  const [pathEditing, setPathEditing] = useState(false);
  const [overlayStatus, setOverlayStatus] = useState<"idle" | "saving" | "error">("idle");
  const [activeSection, chooseSection] = useActiveSection(Boolean(data));

  useEffect(() => {
    if (!data || saveQueue.running || Object.keys(saveQueue.pending).length) return;
    saveQueue.baseline = syncForm(data);
    setForm({ ...saveQueue.baseline, ...(logDraft.current !== null ? { logPath: logDraft.current } : {}) });
  }, [data]);

  useEffect(() => {
    if (!savedFlash) return;
    const timer = window.setTimeout(() => setSavedFlash(false), 2000);
    return () => window.clearTimeout(timer);
  }, [savedFlash]);

  // Serialize full-config requests and merge queued patches into the last
  // acknowledged config. Polling and earlier saves must never erase later edits.
  const drainSaves = async () => {
    if (saveQueue.running) return;
    setSaving(true);
    setSavedFlash(false);
    setSaveError("");
    try {
      await saveQueue.drain(async (config, patch) => {
        await queryClient.cancelQueries({ queryKey: runtimeStatusKey });
        const status = await api.saveRuntimeConfig(config);
        await queryClient.cancelQueries({ queryKey: runtimeStatusKey });
        const acknowledged = syncForm(status);
        queryClient.setQueryData(runtimeStatusKey, status);
        if (patch.autoCheckUpdates && !status.updateCheck) updateCheckMutation.mutate();
        if ("seventeenLandsUserGroup" in patch || "seventeenLandsFormat" in patch) {
          void queryClient.invalidateQueries({ queryKey: ["card-stats"] });
        }
        setForm({ ...acknowledged, ...saveQueue.pending,
          ...(logDraft.current !== null ? { logPath: logDraft.current } : {}) });
        void queryClient.invalidateQueries({ queryKey: aiStatusKey });
        return acknowledged;
      });
      setSavedFlash(true);
    } catch (error) {
      setSaveError(error instanceof Error ? error.message : "Unable to save settings.");
    } finally {
      setSaving(false);
    }
  };

  const savePatch = (patch: Partial<RuntimeConfig>) => {
    saveQueue.enqueue(patch);
    setForm((current) => ({ ...current, ...patch }));
    void drainSaves();
  };

  const commitLogPath = (value: string) => {
    const logPath = value.trim();
    if (/[\r\n\0]/.test(logPath)) {
      setPathError("Enter a log file path on one line.");
      return;
    }
    setPathError("");
    logDraft.current = null;
    setPathDirty(false);
    if (saveQueue.running || logPath !== saveQueue.baseline.logPath || "logPath" in saveQueue.pending) savePatch({ logPath });
    else setForm((current) => ({ ...current, logPath }));
  };

  const importMutation = useMutation({
    mutationFn: () => api.runImport(true),
    onSuccess: async () => {
      await refreshDataQueries(queryClient);
    },
  });

  const startLiveMutation = useMutation({
    mutationFn: api.startLive,
    onSuccess: (status) => {
      queryClient.setQueryData(runtimeStatusKey, status);
    },
  });

  const stopLiveMutation = useMutation({
    mutationFn: api.stopLive,
    onSuccess: (status) => {
      queryClient.setQueryData(runtimeStatusKey, status);
    },
  });

  const autostartQuery = useQuery({
    queryKey: autostartKey,
    queryFn: api.autostartStatus,
    staleTime: 60_000,
  });

  const [autostartDraft, setAutostartDraft] = useState<boolean | null>(null);
  const autostartMutation = useMutation({
    mutationFn: (enabled: boolean) => api.setAutostart(enabled),
    onSuccess: (status) => {
      queryClient.setQueryData(autostartKey, status);
      setAutostartDraft(null);
      setSavedFlash(true);
    },
  });

  const updateCheckMutation = useMutation({
    mutationFn: api.checkForUpdate,
    onSuccess: () => {
      // The backend stores the result on the status payload; refresh it so the
      // displayed result and its timestamp come from one source.
      void queryClient.invalidateQueries({ queryKey: runtimeStatusKey });
    },
  });

  const pickLogMutation = useMutation({
    mutationFn: api.pickLogFile,
    onSuccess: (result) => {
      if (result.path) {
        commitLogPath(result.path);
      }
    },
  });

  const pollOptions = useMemo(() => {
    const base = [1, 2, 5, 10];
    if (!base.includes(form.pollIntervalSeconds)) {
      base.push(form.pollIntervalSeconds);
    }
    return base.sort((a, b) => a - b);
  }, [form.pollIntervalSeconds]);

  if (isLoading) return <LoadingMessage page>Loading runtime settings…</LoadingMessage>;
  if (error) return <StatusMessage tone="error">{(error as Error).message}</StatusMessage>;
  if (!data) return <StatusMessage>No runtime status available.</StatusMessage>;

  const effectiveActivePath = form.logPath.trim() || data.defaultLogPath;
  const canPickFile = Boolean(data.capabilities?.pickFile);
  const canReveal = Boolean(data.capabilities?.reveal);
  const updateResult = data.updateCheck ?? updateCheckMutation.data;
  const settingsPending = saving || Boolean(saveError) || pathDirty || Boolean(pathError);
  const liveMutationPending = startLiveMutation.isPending || stopLiveMutation.isPending;
  const importDisabled = importMutation.isPending || data.liveRunning || settingsPending;
  const liveError = (startLiveMutation.error || stopLiveMutation.error) as Error | null;
  const aiProviders = aiStatusQuery.data?.providers?.length ? aiStatusQuery.data.providers : fallbackAIProviders;
  const selectedAIProvider =
    aiProviders.find((provider) => provider.id === form.aiProvider) ??
    fallbackAIProviders.find((provider) => provider.id === form.aiProvider)!;
  const aiModels = modelsForProvider(aiProviders, form.aiProvider, form.aiModel);
  const selectedAIModel = aiModels.find((model) => model.id === form.aiModel);
  const aiUsage = aiStatusQuery.data?.usage;

  const importCompletedAt = data.lastImport?.completedAt ? Date.parse(data.lastImport.completedAt) : 0;
  const liveCompletedAt = data.lastLiveActivity?.completedAt ? Date.parse(data.lastLiveActivity.completedAt) : 0;
  const lastActivity =
    importCompletedAt || liveCompletedAt
      ? liveCompletedAt >= importCompletedAt
        ? data.lastLiveActivity
        : data.lastImport
      : undefined;

  const handleLiveToggle = () => {
    if (data.liveRunning) stopLiveMutation.mutate();
    else if (!settingsPending) startLiveMutation.mutate();
  };


  const customLogPath = form.logPath.trim().length > 0;
  const showPathEditor = pathEditing || pathDirty || Boolean(pathError);
  const previousLogPath = data.previousLogPath || data.defaultPrevLogPath;
  const autostart = autostartQuery.data;
  const autostartEnabled = autostartDraft ?? autostart?.enabled ?? false;
  // A note on a supported platform is an error from the OS lookup, so the
  // switch can only be used to turn an existing login item off.
  const autostartUnavailable = !autostart?.supported || (Boolean(autostart.note) && !autostartEnabled);
  const savingAny = saving || autostartMutation.isPending || overlayStatus === "saving";
  const saveStatus = savingAny
    ? "Saving…"
    : pathDirty
      ? "Finish editing the log path to save."
      : saveError || overlayStatus === "error" || autostartMutation.error
        ? "Some changes were not saved"
        : "All changes saved";
  const activityText = lastActivity?.completedAt
    ? `last activity ${formatRelativeTime(lastActivity.completedAt)}`
    : data.liveRunning
      ? "watching for Arena activity"
      : "no activity yet";
  const usageSummary = aiUsage
    ? [
        `${aiUsageNumberFormatter.format(aiUsage.runs)} ${aiUsage.runs === 1 ? "run" : "runs"}`,
        `${aiUsageCompactFormatter.format(aiUsage.inputTokens)} in`,
        `${aiUsageCompactFormatter.format(aiUsage.outputTokens)} out`,
        aiUsage.lastRun ? `last ${formatShortDateTime(aiUsage.lastRun.createdAt)}` : "",
      ]
        .filter(Boolean)
        .join(" · ")
    : aiStatusQuery.isLoading ? <LoadingLabel>Loading…</LoadingLabel> : "Usage unavailable";

  return (
    <div className="stack-lg">
      <PageHeader
        title="Settings"
        actions={
          <div className="settings-save-status" role="status" aria-live="polite">
            {saveStatus}
          </div>
        }
      />
      {saveError ? (
        <StatusMessage tone="error">
          Settings could not be saved: {saveError}{" "}
          <button type="button" className="settings-text-button" onClick={() => void drainSaves()} disabled={saving}>Retry</button>
        </StatusMessage>
      ) : null}

      <div className="settings-layout">
        <nav className="settings-nav" aria-label="Settings sections">
          {sections.map((section) => (
            <button
              key={section.id}
              type="button"
              className={`settings-nav-item${activeSection === section.id ? " is-active" : ""}`}
              aria-current={activeSection === section.id ? "true" : undefined}
              onClick={() => chooseSection(section.id)}
            >
              {section.label}
            </button>
          ))}
        </nav>

        <div className="settings-sections">
          <section id="settings-tracking" className="settings-section" aria-labelledby="settings-tracking-title">
            <div className="settings-section-head">
              <h3 id="settings-tracking-title">Tracking</h3>
            </div>
            {data.lastError && data.lastError !== dismissedError ? (
              <div className="settings-last-error" role="alert">
                <span>Last runtime error</span>
                <p>{data.lastError}</p>
                <button type="button" onClick={() => setDismissedError(data.lastError ?? "")}>
                  Dismiss
                </button>
              </div>
            ) : null}
            <div className="panel settings-card">
              <div className="settings-row settings-row--status">
                <div className="settings-status">
                  <span className={`settings-status-dot${data.liveRunning ? " is-on" : ""}`} aria-hidden="true" />
                  <div className="settings-row-text">
                    <div className="settings-status-title">
                      {data.liveRunning ? "Recording Arena activity" : "Tracking is stopped"}
                    </div>
                    <div className="settings-row-help">
                      {data.activeLogPathExists ? "Arena log found" : <strong className="is-negative">Arena log not found</strong>}
                      {" · "}
                      {activityText}
                      {lastActivity?.completedAt ? ` · ${summarizeOperation(lastActivity)}` : ""}
                    </div>
                  </div>
                </div>
                <div className="settings-row-control">
                  <button
                    type="button"
                    className={`settings-button${data.liveRunning ? "" : " settings-button--primary"}`}
                    onClick={() => void handleLiveToggle()}
                    disabled={liveMutationPending || (!data.liveRunning && settingsPending)}
                  >
                    {liveMutationPending
                      ? data.liveRunning
                        ? "Stopping…"
                        : "Starting…"
                      : data.liveRunning
                        ? "Stop tracking"
                        : "Start tracking"}
                  </button>
                </div>
              </div>
              <ToggleRow
                label={`Start tracking when ${APP_NAME} opens`}
                help={`Also picks up anything Arena logged while ${APP_NAME} was closed.`}
                checked={form.autoStartLive ?? true}
                onChange={(autoStartLive) => savePatch({ autoStartLive })}
              />

              <details className="settings-expander" open={!data.activeLogPathExists}>
                <summary>
                  <Chevron />
                  <span>Advanced</span>
                  <small>Log file, scan interval, manual check</small>
                </summary>
                <div className="settings-expander-body">
                  <SettingRow
                    label="Log file"
                    help={
                      <code className="settings-path" title={effectiveActivePath}>
                        {shortenHomePath(effectiveActivePath)}
                      </code>
                    }
                  >
                    <span className="settings-tag">{customLogPath ? "Custom" : "Default"}</span>
                    {customLogPath ? (
                      <button type="button" className="settings-text-button" onClick={() => commitLogPath("")}>
                        Use default
                      </button>
                    ) : null}
                    <button
                      type="button"
                      className="settings-button"
                      aria-expanded={showPathEditor}
                      onClick={() => setPathEditing((open) => !open)}
                    >
                      {showPathEditor ? "Done" : "Change…"}
                    </button>
                  </SettingRow>
                  {showPathEditor ? (
                    <div className="settings-row settings-row--editor">
                      <input
                        className="settings-input"
                        type="text"
                        aria-label="Custom log file path"
                        value={form.logPath}
                        onChange={(event) => {
                          logDraft.current = event.target.value;
                          setPathDirty(true);
                          setForm((current) => ({ ...current, logPath: event.target.value }));
                        }}
                        onBlur={(event) => commitLogPath(event.currentTarget.value)}
                        onKeyDown={(event) => {
                          if (event.key === "Enter") {
                            event.preventDefault();
                            commitLogPath(event.currentTarget.value);
                          }
                        }}
                        aria-invalid={Boolean(pathError)}
                        placeholder={data.defaultLogPath}
                        spellCheck={false}
                      />
                      {canPickFile ? (
                        <button
                          type="button"
                          className="settings-button"
                          onClick={() => pickLogMutation.mutate()}
                          disabled={pickLogMutation.isPending}
                        >
                          {pickLogMutation.isPending ? "Choosing…" : "Browse…"}
                        </button>
                      ) : null}
                    </div>
                  ) : null}
                  {pathError ? <StatusMessage tone="error">{pathError}</StatusMessage> : null}
                  {pickLogMutation.error ? (
                    <StatusMessage tone="error">File picker: {(pickLogMutation.error as Error).message}</StatusMessage>
                  ) : null}

                  <ToggleRow
                    label="Include the previous session's log"
                    help={
                      customLogPath ? (
                        "Off while a custom log file is set."
                      ) : (
                        <span title={previousLogPath}>
                          Player-prev.log · {data.previousLogPathExists ? "found" : "not found"}
                        </span>
                      )
                    }
                    checked={form.includePrev}
                    onChange={(includePrev) => savePatch({ includePrev })}
                    disabled={customLogPath}
                  />

                  <SettingRow
                    label="Check for missed updates every"
                    help="A safety net. Log changes are normally picked up straight away."
                    htmlFor="settings-poll"
                  >
                    <select
                      id="settings-poll"
                      className="settings-select"
                      value={String(form.pollIntervalSeconds)}
                      onChange={(event) => {
                        savePatch({ pollIntervalSeconds: normalizePollInterval(event.target.value) });
                      }}
                    >
                      {pollOptions.map((seconds) => (
                        <option key={seconds} value={String(seconds)}>
                          {seconds === 1 ? "1 second" : `${seconds} seconds`}
                        </option>
                      ))}
                    </select>
                  </SettingRow>

                  <SettingRow
                    label="Check logs once without tracking"
                    help={
                      <>
                        {data.lastImport?.completedAt
                          ? `Last checked ${formatShortDateTime(data.lastImport.completedAt)} · ${summarizeOperation(data.lastImport)}.`
                          : "No manual checks yet."}
                        {data.liveRunning ? " Stop tracking to use this." : ""}
                      </>
                    }
                  >
                    <button
                      type="button"
                      className="settings-button"
                      onClick={() => importMutation.mutate()}
                      disabled={importDisabled}
                      title={
                        settingsPending
                          ? "Wait for settings to save before checking the logs."
                          : data.liveRunning
                            ? "Stop tracking before running a manual check."
                            : undefined
                      }
                    >
                      {importMutation.isPending ? "Checking…" : "Check now"}
                    </button>
                  </SettingRow>
                  {importMutation.error ? (
                    <StatusMessage tone="error">Log check failed: {(importMutation.error as Error).message}</StatusMessage>
                  ) : null}
                </div>
              </details>
            </div>
            {liveError ? <StatusMessage tone="error">Tracking: {liveError.message}</StatusMessage> : null}
          </section>

          <OverlaySettingsPanel id="settings-overlay" onStatusChange={setOverlayStatus} />

          <section id="settings-ratings" className="settings-section" aria-labelledby="settings-ratings-title">
            <div className="settings-section-head">
              <h3 id="settings-ratings-title">Draft ratings</h3>
              <p>Which 17Lands ratings appear in pick logs, pack replays and deck lists.</p>
            </div>
            <div className="panel settings-card">
              <SettingRow
                label="Player group"
                help="Narrower groups have fewer games, so more cards show no rating. Quick Draft always covers all players."
                htmlFor="settings-17l-group"
              >
                <select
                  id="settings-17l-group"
                  className="settings-select"
                  value={form.seventeenLandsUserGroup}
                  onChange={(event) => {
                    savePatch({ seventeenLandsUserGroup: event.target.value as SeventeenLandsUserGroup });
                  }}
                >
                  {Object.entries(SEVENTEEN_LANDS_USER_GROUP_LABELS).map(([group, label]) => (
                    <option key={group} value={group}>
                      {label}
                    </option>
                  ))}
                </select>
              </SettingRow>
              <SettingRow
                label="Event type"
                help="Applies to Premier, Traditional, Quick and Pick Two drafts. Other events have no ratings."
                htmlFor="settings-17l-format"
              >
                <select
                  id="settings-17l-format"
                  className="settings-select"
                  value={form.seventeenLandsFormat}
                  onChange={(event) => {
                    savePatch({ seventeenLandsFormat: event.target.value as SeventeenLandsFormat });
                  }}
                >
                  <option value="">Match each draft</option>
                  {Object.entries(SEVENTEEN_LANDS_FORMAT_LABELS).map(([format, label]) => (
                    <option key={format} value={format}>
                      {label}
                    </option>
                  ))}
                </select>
              </SettingRow>
            </div>
          </section>

          <section id="settings-ai" className="settings-section" aria-labelledby="settings-ai-title">
            <div className="settings-section-head">
              <h3 id="settings-ai-title">AI</h3>
              <p>
                Deck primers and game reviews, run through a CLI you are already signed in to. {APP_NAME} stores no
                credentials.
              </p>
            </div>
            <div className="panel settings-card">
              <div className="settings-block">
                <div className="settings-block-head">
                  <span className="settings-row-label" id="settings-ai-provider-label">
                    Provider
                  </span>
                  <button
                    type="button"
                    className="settings-text-button"
                    onClick={() => void aiStatusQuery.refetch()}
                    disabled={aiStatusQuery.isFetching}
                  >
                    {aiStatusQuery.isFetching ? "Checking…" : "Recheck sign-in"}
                  </button>
                </div>
                <div className="settings-choice-grid" role="radiogroup" aria-labelledby="settings-ai-provider-label">
                  {aiProviders.map((provider) => {
                    const selected = provider.id === form.aiProvider;
                    const state = aiStatusQuery.data ? providerState(provider) : { label: "Checking…", tone: "neutral" as const };
                    const facts = [provider.authMethod, provider.version].filter(Boolean).join(" · ");
                    return (
                      <button
                        key={provider.id}
                        type="button"
                        role="radio"
                        aria-checked={selected}
                        className={`settings-choice${selected ? " is-active" : ""}`}
                        title={provider.cliPath ? `CLI: ${provider.cliPath}` : undefined}
                        onClick={() => {
                          if (selected) return;
                          savePatch({
                            aiProvider: provider.id,
                            aiModel: provider.models[0]?.id ?? (provider.id === "openai" ? "default" : "opus"),
                          });
                        }}
                      >
                        <span className="settings-choice-mark" aria-hidden="true" />
                        <span className="settings-choice-text">
                          <span className="settings-choice-title">
                            {provider.name}
                            <span className={`settings-tag is-${state.tone}`}>{state.label}</span>
                          </span>
                          <span className="settings-row-help">
                            {provider.available ? facts || provider.detail : provider.detail || facts || "Checking installation and sign-in…"}
                          </span>
                        </span>
                      </button>
                    );
                  })}
                </div>
                {aiStatusQuery.data && !selectedAIProvider.available ? (
                  <p className="settings-row-help">
                    {form.aiProvider === "openai"
                      ? "Install the Codex CLI, run `codex login` and choose Sign in with ChatGPT."
                      : "Install the Claude Code CLI and sign in with your Claude subscription."}{" "}
                    AI generation stays off until the selected provider is ready.
                  </p>
                ) : null}
                {aiStatusQuery.error ? (
                  <StatusMessage tone="error">AI status: {(aiStatusQuery.error as Error).message}</StatusMessage>
                ) : null}
              </div>

              <SettingRow
                label="Model"
                help={selectedAIModel?.description ?? "Model passed to the selected CLI."}
                htmlFor="settings-ai-model"
              >
                <select
                  id="settings-ai-model"
                  className="settings-select"
                  value={form.aiModel}
                  onChange={(event) => {
                    savePatch({ aiModel: event.target.value });
                  }}
                >
                  {aiModels.map((model) => (
                    <option key={model.id} value={model.id}>
                      {model.name}
                    </option>
                  ))}
                </select>
              </SettingRow>

              <details className="settings-expander settings-expander--row">
                <summary>
                  <Chevron />
                  <span>Usage</span>
                  <small className="settings-expander-value">{usageSummary}</small>
                </summary>
                <div className="settings-expander-body">
                  {aiStatusQuery.data?.usageError ? (
                    <StatusMessage tone="error">Token usage: {aiStatusQuery.data.usageError}</StatusMessage>
                  ) : null}
                  {aiUsage ? (
                    <>
                      <div className="settings-table-scroll">
                        <table className="settings-usage-table">
                          <thead>
                            <tr>
                              <th scope="col">Provider</th>
                              <th scope="col">Runs</th>
                              <th scope="col">Input</th>
                              <th scope="col">Cached</th>
                              <th scope="col">Cache written</th>
                              <th scope="col">Output</th>
                              <th scope="col">Reasoning</th>
                            </tr>
                          </thead>
                          <tbody>
                            {[
                              ...aiUsage.providers.map((usage) => ({
                                key: usage.provider as string,
                                name: aiProviders.find((provider) => provider.id === usage.provider)?.name ?? usage.provider,
                                usage,
                              })),
                              ...(aiUsage.providers.length === 1 ? [] : [{ key: "total", name: "Total", usage: aiUsage }]),
                            ].map(({ key, name, usage }) => (
                              <tr key={key} className={key === "total" ? "is-total" : undefined}>
                                <th scope="row">{name}</th>
                                <td>
                                  {aiUsageNumberFormatter.format(usage.runs)}
                                  {usage.runs > usage.successfulRuns ? (
                                    <span className="is-negative"> ({usage.runs - usage.successfulRuns} failed)</span>
                                  ) : null}
                                </td>
                                <td>{aiUsageNumberFormatter.format(usage.inputTokens)}</td>
                                <td className="is-subset">{aiUsageNumberFormatter.format(usage.cachedInputTokens)}</td>
                                <td className="is-subset">{aiUsageNumberFormatter.format(usage.cacheWriteInputTokens)}</td>
                                <td>{aiUsageNumberFormatter.format(usage.outputTokens)}</td>
                                <td className="is-subset">{aiUsageNumberFormatter.format(usage.reasoningOutputTokens)}</td>
                              </tr>
                            ))}
                          </tbody>
                        </table>
                      </div>
                      <p className="settings-row-help">
                        {aiUsage.lastRun
                          ? `Last run ${formatShortDateTime(aiUsage.lastRun.createdAt)}: ${aiUsage.lastRun.provider}/${aiUsage.lastRun.model}, ${formatTokenCount(aiUsage.lastRun.outputTokens)} out, ${aiUsage.lastRun.succeeded ? "completed" : "failed"}. `
                          : "No provider has reported token usage yet. "}
                        Cached and reasoning tokens are provider-reported subsets and are not added to the input and
                        output totals.
                      </p>
                    </>
                  ) : aiStatusQuery.isLoading ? (
                    <LoadingMessage className="settings-row-help">Loading token usage…</LoadingMessage>
                  ) : (
                    <p className="settings-row-help">Token usage is unavailable.</p>
                  )}
                </div>
              </details>
            </div>
          </section>

          <section id="settings-appearance" className="settings-section" aria-labelledby="settings-appearance-title">
            <div className="settings-section-head">
              <h3 id="settings-appearance-title">Appearance</h3>
            </div>
            <div className="panel settings-card">
              <SettingRow label={<span id="settings-mode-label">Mode</span>}>
                <div className="settings-segmented" role="radiogroup" aria-labelledby="settings-mode-label">
                  {modeOptions.map((option) => {
                    const Icon = option.icon;
                    return (
                      <button
                        key={option.value}
                        type="button"
                        role="radio"
                        aria-checked={modePreference === option.value}
                        className={`settings-segment${modePreference === option.value ? " is-active" : ""}`}
                        onClick={() => setModePreference(option.value)}
                      >
                        <Icon />
                        {option.label}
                      </button>
                    );
                  })}
                </div>
              </SettingRow>
              <SettingRow label={<span id="settings-scheme-label">Colour scheme</span>}>
                <div className="settings-swatches" role="radiogroup" aria-labelledby="settings-scheme-label">
                  {COLOR_SCHEMES.map((option) => (
                    <button
                      key={option}
                      type="button"
                      role="radio"
                      aria-checked={scheme === option}
                      className={`settings-swatch${scheme === option ? " is-active" : ""}`}
                      onClick={() => setScheme(option)}
                    >
                      <span className="settings-swatch-chip" aria-hidden="true">
                        <span style={{ background: THEME_COLORS[option][mode] }} />
                        <span style={{ background: CHART_THEME_TOKENS[`${option}-${mode}`].accent }} />
                      </span>
                      {SCHEME_LABELS[option]}
                    </button>
                  ))}
                </div>
              </SettingRow>
              {data.capabilities?.appIcon ? (
                <AppIconPicker value={form.appIcon} onChange={(appIcon) => savePatch({ appIcon })} />
              ) : null}
            </div>
          </section>

          <section id="settings-app" className="settings-section" aria-labelledby="settings-app-title">
            <div className="settings-section-head">
              <h3 id="settings-app-title">App and data</h3>
            </div>
            {autostartMutation.error ? (
              <StatusMessage tone="error">
                Open at login could not be saved: {(autostartMutation.error as Error).message}{" "}
                <button type="button" className="settings-text-button" disabled={autostartMutation.isPending}
                  onClick={() => autostartDraft !== null && autostartMutation.mutate(autostartDraft)}>Retry</button>
              </StatusMessage>
            ) : null}
            <div className="panel settings-card">
              <ToggleRow
                label={`Open ${APP_NAME} at login`}
                help={
                  autostartUnavailable ? (
                    <span title={autostart?.note || undefined}>
                      {autostartQuery.isPending ? "Checking…" : "Not available in this build. It works in the installed app."}
                    </span>
                  ) : (
                    autostart?.note || undefined
                  )
                }
                checked={autostartEnabled}
                onChange={(enabled) => {
                  setAutostartDraft(enabled);
                  autostartMutation.mutate(enabled);
                }}
                disabled={autostartUnavailable || autostartMutation.isPending}
              />
              <ToggleRow
                label="Check for updates daily"
                help={
                  <>
                    Version {data.version || "unknown"}
                    {updateResult ? ` · ${summarizeUpdateCheck(updateResult)}` : ""}
                    {updateResult?.checkedAt ? ` · checked ${formatRelativeTime(updateResult.checkedAt)}` : ""}
                    {updateResult?.updateAvailable && updateResult.releaseUrl ? (
                      <>
                        {" · "}
                        <a href={updateResult.releaseUrl} target="_blank" rel="noreferrer">
                          View release
                        </a>
                      </>
                    ) : null}
                  </>
                }
                checked={form.autoCheckUpdates ?? false}
                onChange={(autoCheckUpdates) => savePatch({ autoCheckUpdates })}
                before={
                  <button
                    type="button"
                    className="settings-button"
                    onClick={() => updateCheckMutation.mutate()}
                    disabled={updateCheckMutation.isPending}
                  >
                    {updateCheckMutation.isPending ? "Checking…" : "Check now"}
                  </button>
                }
              />
              {updateCheckMutation.error ? (
                <StatusMessage tone="error">{(updateCheckMutation.error as Error).message}</StatusMessage>
              ) : null}
              <PathRow
                label="Database"
                detail={data.dbSizeBytes > 0 ? formatBytes(data.dbSizeBytes) : "Not created yet"}
                path={data.dbPath}
                canReveal={canReveal}
              />
              <PathRow label="Config file" path={data.configPath} canReveal={canReveal} />
            </div>
            <p className="settings-footnote">
              Closing the window keeps {APP_NAME} tracking in the background. Quit fully with Cmd+Q.
            </p>
          </section>
        </div>
      </div>
    </div>
  );
}
