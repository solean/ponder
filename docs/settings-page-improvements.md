# Settings Page Improvements — Plan

Status: Phase 1 implemented (2026-07-03); Phases 2–4 proposed
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

## Phase 2 — Form correctness & feedback (frontend only)

1. **Kill the unsaved-edits trap.** When `hasLocalEdits` is true and the user clicks
   Start Live Tracking, save first, then start (chain the mutations), surfacing any save
   error at the button. Alternative considered and rejected: a confirm dialog — extra
   friction for the common "I meant both" case.
2. **Unsaved-changes indicator + reset.** Show a small "Unsaved changes" chip in the
   Tracking panel head while dirty, and a "Discard" text button that re-syncs the form
   from `runtimeStatus` (`syncForm`). Add "Use default" next to Custom Log Path to clear
   it explicitly.
3. **Save confirmation.** Transient "Saved ✓" state on the Save button (~2s) after a
   successful mutation.
4. **Localize errors to their action.** Replace the merged `currentError` banner with:
   - mutation errors rendered directly under the action row that triggered them;
   - `data.lastError` shown as its own labeled status item ("Last runtime error") with
     the tick/operation timestamp, visually distinct from a just-failed action, and
     dismissable (client-side hide until the message changes).
5. **Poll interval as presets.** Replace the free number input with a select
   (1s / 2s / 5s / 10s). Removes the `min={1}` vs `normalizePollInterval` → 2 mismatch;
   keep the normalizer for config loaded from disk.

Acceptance: edit poll interval → click Start Live → new interval is active (verify via
runtime status); failed save shows error at the button; dirty chip appears/disappears
correctly.

## Phase 3 — Information architecture

Restructure the page into a status strip plus three purpose-named sections:

1. **Status strip (top, replaces "Runtime Control").** One compact ribbon: live state
   pill, active log Found/Missing, last activity summary. Long paths move out of the
   hero position into the Data section. The panel-head subtitle slot goes back to being
   a *description* everywhere; status never lives there.
2. **Tracking** — log path, poll interval, `includePrev` (with the Default Previous Log
   path shown inline here, moved out of "Recent Activity"), Start/Stop Live button
   adjacent to the state it controls, Save/Discard for the form.
3. **Data** — database path (+ size, Phase 4), config file path, Import Logs Now, and the
   Last Import / Last Live Activity cards. Consider mirroring Recent Activity on the
   Overview page later; not in scope here.
4. **Application** — version, launch-at-login, update check, background-window note, and
   a theme toggle mirror (nav toggle stays; Settings is where people look for it).

Acceptance: no orphan cards in grids; every panel's subtitle is descriptive; tab order
follows visual order.

## Phase 4 — New capabilities (backend + frontend)

Each item is independent; ordered by value/effort.

1. **Copyable paths.** Frontend-only: copy-to-clipboard button on DB, config, and log
   path rows (clipboard API, "Copied" tooltip feedback).
2. **Capabilities in RuntimeStatus.** Add `capabilities: { pickFile: bool, reveal: bool }`
   to the status payload (`internal/appstate/service.go` Status struct + `web/src/lib/types.ts`).
   Desktop sets both true; serve mode false. Gates the two features below.
3. **Browse… for the log path.** Desktop: `POST /api/runtime/pick-log` → handler invokes
   Wails `runtime.OpenFileDialog` (needs the Wails context plumbed from `app.go` into the
   API server, e.g. a `DialogProvider` interface the desktop app implements) → returns the
   chosen path → frontend fills the input (still requires Save). Hidden in serve mode.
4. **Reveal in Finder.** `POST /api/runtime/reveal {path}` — desktop uses Wails/`open -R`;
   restrict to the known paths from status (db, config, logs), never arbitrary client
   input. Hidden when capability is false.
5. **Database size.** Add `dbSizeBytes` to Status (stat the file in `Status()`, include
   -wal/-shm) and display it next to the DB path. Backend + display only; cheap.
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
