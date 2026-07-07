# Settings Page Improvements — Plan

Status: Phases 1–3 implemented (2026-07-04); Phase 4 in progress — items 1 (copyable
paths), 2 (capabilities), 3 (file picker), 4 (reveal), and 5 (database size) done
2026-07-04, plus a three-way dark/light/system theme control replacing both the settings
checkbox and the navbar toggle. Remaining: 6 (auto-start live), 7 (auto-check updates),
8 (data management, needs spec)
Scope: `web/src/pages/SettingsPage.tsx`, `web/src/styles.css`, `web/src/lib/{api,types}.ts`,
`internal/appstate/service.go`, `internal/api/server.go`, `app.go` (desktop-only features)

## Background

The settings page currently mixes three kinds of content — read-only status (Runtime
Control, Recent Activity), one-shot actions (Import, Start/Stop Live, Check for Updates),
and persisted preferences (log path, poll interval, `includePrev`, autostart). Visually it
is consistent with the rest of the app, but paths dominate the layout, status carries no
color, notes render as uppercase shouting, and there is one genuine UX trap around unsaved
edits.

Two backend facts that shape the plan:

- `UpdateConfig` (internal/appstate/service.go:200) already restarts the live poller when
  config is saved while live is running. The unsaved-edits trap exists only on the
  *edit → Start Live without saving* path, where the stale saved config is used silently.
- Live tracking does **not** auto-resume on app launch; nothing outside the API handlers
  and the `UpdateConfig` restart calls `StartLive`.

The app runs in two modes: Wails desktop and headless `serve` (plain browser). Anything
that needs a native shell (file picker, Reveal in Finder) must degrade gracefully in
serve mode — plan is to advertise capabilities in `RuntimeStatus` and hide the buttons
when unsupported.

---

## Phase 1 — Quick wins (CSS + copy only, no behavior change) ✅ DONE (2026-07-03)

Small, independently shippable, all frontend.

1. ✅ **Sentence-case the notes.** `.settings-note` currently inherits the uppercase mono
   label styling, so full sentences ("MANUAL IMPORT USES RESUME MODE…") read like alarms.
   Split `.settings-note` out of the `.settings-field span, .settings-status-card span`
   rule (styles.css:1326) and set it in `--font-sans`, normal case, `--muted-strong`.
2. ✅ **Color the status words.** "Found"/"Missing" and "Running"/"Stopped" are plain text.
   Add a small status pill/dot component reusing the existing `--pill-win-*` /
   `--pill-loss-*` tokens: green for Found/Running (pulse animation while live), red/amber
   for Missing, neutral for Stopped.
3. ✅ **Theme the checkboxes.** Native checkboxes render browser-blue in both themes. Add
   `accent-color: var(--accent)` to `.settings-checkbox input`.
4. ✅ **De-emphasize paths.** Paths are `<strong>` today and dominate every card. Render them
   in `--font-mono` at `small` size with `word-break: break-all`, display with `~/`
   shorthand (keep full path in `title`), and reserve bold for state words.
5. ✅ **Copy fixes.**
   - "No ticks yet" → "Waiting for first update" (also "Last tick" → "Last update").
   - Literal backticks in "Include \`Player-prev.log\`" → a styled `<code>` element.
   - Helper text on the disabled `includePrev` checkbox: "Disabled while a custom log
     path is set."
6. ✅ **Button hierarchy.** Add a `.control-button--primary` accent-filled variant. Apply to
   Save Settings when dirty, otherwise to Start Live Tracking. Give Stop Live Tracking a
   distinct (outline/neutral) treatment (`.control-button--quiet`).

Acceptance: screenshot diff in both themes; no layout regressions on narrow widths.

Implementation notes (as built):
- New local components in `SettingsPage.tsx`: `StatusPill` (tone: positive/negative/neutral,
  optional `pulsing`) and `PathValue` (mono `<code>`, `~/`-shortened, full path in `title`).
- New helper `shortenHomePath()` in `web/src/lib/format.ts`.
- Label selectors tightened to direct-child (`.settings-field > span`,
  `.settings-status-card > span`) so pills/paths inside cards don't inherit the uppercase
  label styling; the pill base rule is `span.settings-status-pill` to match that
  specificity. New CSS: `.settings-path`, `.settings-status-pill` (+ `is-positive`,
  `is-negative`, `is-pulsing`), `.settings-checkbox-hint`, `.settings-note code` /
  `.settings-checkbox code`, `.control-button--primary`, `.control-button--quiet`.
- Verified in the running app in both themes (computed styles + dirty/clean button-state
  transitions); `bun test` (60 pass) and `tsc -b` clean.

## Phase 2 — Form correctness & feedback (frontend only) ✅ DONE (2026-07-03)

1. ✅ **Kill the unsaved-edits trap.** When `hasLocalEdits` is true and the user clicks
   Start Live Tracking, save first, then start (chain the mutations), surfacing any save
   error at the button. Alternative considered and rejected: a confirm dialog — extra
   friction for the common "I meant both" case.
2. ✅ **Unsaved-changes indicator + reset.** Show a small "Unsaved changes" chip in the
   Tracking panel head while dirty, and a "Discard" text button that re-syncs the form
   from `runtimeStatus` (`syncForm`). Add "Use default" next to Custom Log Path to clear
   it explicitly.
3. ✅ **Save confirmation.** Transient "Saved ✓" state on the Save button (~2s) after a
   successful mutation.
4. ✅ **Localize errors to their action.** Replace the merged `currentError` banner with:
   - mutation errors rendered directly under the action row that triggered them;
   - `data.lastError` shown as its own labeled status item ("Last runtime error"),
     visually distinct from a just-failed action, and dismissable (client-side hide
     until the message changes). Note: no timestamp — the status payload doesn't carry
     one for `lastError`; add server-side if it proves confusing.
5. ✅ **Poll interval as presets.** Replace the free number input with a select
   (1s / 2s / 5s / 10s). Removes the `min={1}` vs `normalizePollInterval` → 2 mismatch;
   keep the normalizer for config loaded from disk.

Acceptance: edit poll interval → click Start Live → new interval is active (verify via
runtime status); failed save shows error at the button; dirty chip appears/disappears
correctly.

Implementation notes (as built):
- `handleLiveToggle` in `SettingsPage.tsx` chains `saveMutation.mutateAsync()` before
  `startLive` when dirty and aborts on save failure; the live button is also disabled
  while a save is pending. Discard is a quiet button next to Save, only visible dirty.
- Save flash uses `savedFlash` state + `.control-button.is-flash:disabled` (full opacity,
  win-color border) so the disabled flash state doesn't look dimmed.
- Mutation errors render as `StatusMessage` lines under the action row, prefixed
  ("Save failed:", "Import failed:", "Live tracking:"); `.settings-action-row ~ .state`
  adds spacing. `data.lastError` renders as `.settings-last-error` (loss-tokens card)
  with a Dismiss button; dismissal is per-message client state.
- If a saved config has a non-preset interval (e.g. 3s), it's injected into the select
  options so the control never misrepresents saved state.
- New CSS: `.settings-unsaved-chip`, `.settings-text-button`, `.settings-last-error`,
  `.control-button.is-flash`. Verified live: chip/discard/use-default flows, select
  options, real save roundtrip (5s → verified via /api/runtime/status → restored 2s),
  flash appears and reverts after ~2s. Start-live chaining not exercised against the
  running app (would start the real poller); logic covered by review + typecheck.

## Phase 3 — Information architecture ✅ DONE (2026-07-04)

Restructure the page into a status strip plus three purpose-named sections:

1. ✅ **Status strip (top, replaces "Runtime Control").** One compact ribbon: live state
   pill, active log Found/Missing, last activity summary. Long paths move out of the
   hero position into the Data section. The panel-head subtitle slot goes back to being
   a *description* everywhere; status never lives there.
2. ✅ **Tracking** — log path, poll interval, `includePrev` (with the Default Previous Log
   path shown inline here, moved out of "Recent Activity"), Start/Stop Live button
   adjacent to the state it controls, Save/Discard for the form.
3. ✅ **Data** — database path (+ size, Phase 4), config file path, Import Logs Now, and
   the Last Import / Last Live Activity cards. Consider mirroring Recent Activity on the
   Overview page later; not in scope here.
4. ✅ **Application** — version, launch-at-login, update check, background-window note,
   and a theme toggle mirror (nav toggle stays; Settings is where people look for it).

Acceptance: no orphan cards in grids; every panel's subtitle is descriptive; tab order
follows visual order.

Implementation notes (as built):
- Status strip is an unheaded `section.panel` with `aria-label="Runtime status"` holding
  `.settings-strip` items (Live Tracking pill, Active Log pill, Last Activity relative
  time via `formatRelativeTime`). The dismissable last-error card lives at the top of it.
- Tracking's action row is Save / Discard / Start-Stop Live; Import moved to Data with
  its resume-mode note (plus a hint when disabled because live tracking is running).
  Default Previous Log renders as `.settings-prevlog` under the `includePrev` checkbox,
  only when the default log location is in use; it swaps with the custom-path hint.
- Data grid is four cards (Database, Config File, Last Import, Last Live Activity) — no
  orphans. Application shows a version line, then theme / autostart checkboxes, update
  check, and the background-window note.
- Theme mirror required extending `ThemeContext` to `{ theme, setTheme }`
  (`web/src/lib/theme.ts`); `useTheme()` kept its signature so `RankProgressPanel` was
  untouched, and `useThemeControls()` exposes the setter. Layout memoizes the context
  value.
- Verified live: strip pills/labels, section structure via a11y snapshot, theme checkbox
  flips the app theme and the nav toggle label both ways (localStorage persisted),
  Overview page still renders with no console errors.

## Phase 4 — New capabilities (backend + frontend)

Each item is independent; ordered by value/effort.

1. ✅ **Copyable paths.** (2026-07-04) `CopyButton` + `PathValue` wrapper in
   `SettingsPage.tsx`: clipboard API with `execCommand` fallback, icon flips to a green
   check for 1.5s on success only (no false feedback if the write is denied). Buttons on
   database, config, effective log, and previous log paths.
2. ✅ **Capabilities in RuntimeStatus.** (2026-07-04) `appstate.Capabilities` on
   `Options`/`Status`; desktop passes both true in `app.go`, serve mode gets the zero
   value. Frontend type is `RuntimeCapabilities`, optional on `RuntimeStatus`.
3. ✅ **Browse… for the log path.** (2026-07-04) `api.Desktop` interface
   (`internal/api/desktop.go`) with `POST /api/runtime/pick-log`; `App` implements it
   via Wails `OpenFileDialog` (defaults to the MTGA log directory, *.log filter).
   Browse… button beside the log path input fills the form (still requires Save);
   hidden in serve mode, and the endpoint 400s without a desktop.
4. ✅ **Reveal in Finder.** (2026-07-04) `POST /api/runtime/reveal {path}` → `open -R`
   (files) / `open` (dirs; missing files fall back to parent dir). `revealablePath()`
   allows only paths advertised by the current status (unit-tested in
   `internal/api/desktop_test.go`, incl. traversal and empty-entry cases). Folder icon
   buttons on path rows, hidden without the capability; failures turn the icon red with
   the error in the tooltip.
5. ✅ **Database size.** (2026-07-04) `databaseSize()` in
   `internal/appstate/service.go` sums the db file + `-wal` + `-shm`; `dbSizeBytes` on
   Status, rendered via new `formatBytes()` (unit-tested in `web/test/format.test.ts`)
   in the Database card ("8.8 MB on disk"; "Not created yet" when absent). Verified
   against `ls -la` byte counts.
6. **Auto-start live tracking on launch.** New `Config.autoStartLive bool` (default
   false). Desktop `startup()` and serve-mode startup call `StartLive()` when set and the
   log path exists. Checkbox in the Tracking section. Saved config keeps working for old
   configs (zero-value = off).
7. **Auto-check for updates.** New `Config.autoCheckUpdates bool` (default true?). On
   launch (and at most once per 24h), run the existing update check; surface result as a
   dismissable note in the Application panel and persist the last result in Status so it
   survives navigation (today the result vanishes when leaving the page).
8. **Data management (stretch).** "Back up database" (copy to user-chosen location via
   save dialog, desktop-only) and "Rebuild from logs" (dangerous: delete + full re-import,
   requires typed confirmation). Spec separately before building; listed here so the Data
   section layout reserves room.

Acceptance per item; notably: pick-log and reveal must be invisible in `serve` mode, and
reveal must reject paths not present in the current status payload.

## Sequencing & risk

- Phases 1–2 are pure frontend, low risk, and can ship together in one PR.
- Phase 3 is a layout refactor of one file plus CSS; do it after 1–2 so the new pieces
  (pills, localized errors) land in their final homes without rework.
- Phase 4 items 1, 5 are trivial; 2–4 need the Wails-context plumbing decision; 6–7 touch
  config schema (additive, backward compatible); 8 needs its own spec.
- No migrations: config is JSON with zero-value defaults; Status fields are additive.

## Out of scope

- Moving Recent Activity to the Overview page (candidate follow-up).
- CSV/data export.
- Windows/Linux autostart and reveal equivalents (current autostart is macOS-only;
  keep the `supported` gating pattern).
