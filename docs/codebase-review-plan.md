# Codebase review: issues and implementation plan

Reviewed September 29, 2026. This document records four concrete findings and a
follow-up maintainability improvement. Parser restart recovery, the CLI loopback
default, and complete-range yearly activity are implemented; remaining work is
tracked below.

## Priorities and order

| Priority | Work item | Status |
| --- | --- | --- |
| P1 | Preserve ingestion context across parser restarts | Complete |
| P1 | Bind the CLI API to loopback by default | Partially complete |
| P1 | Reject untrusted-origin API mutations | Not started |
| P2 | Calculate yearly activity from the complete date range | Complete |
| Follow-up | Split the match-detail page into focused components | Complete |

Address the three P1 issues first. The two API changes can ship together, but
each needs its own regression coverage: loopback binding does not replace
request-origin validation. Fix activity correctness before restructuring UI code.

## 1. P1 — Parser restarts silently drop active-match events

### Problem and evidence

Before the fix, `internal/ingest/parser.go` loaded the durable byte offset,
created a fresh in-memory `parseState`, and sought directly to that offset. A new
parser did not restore the active match ID from the earlier log records.

In `internal/ingest/gre.go`, game-state messages without `gameInfo.matchID` rely
on `state.activeMatchID`. When both are empty, the message is skipped. The cursor
can then advance past the skipped event, so later incremental imports do not
recover it automatically.

This affects restarting the app during a match and stopping/restarting live
tracking, because `Service.StartLive` creates a new parser. It can leave card
plays, opponent observations, and replay data incomplete.

The review reproduced this by adapting
`TestTailParsePersistsStateAcrossResumeCalls`: replace the parser with a new
instance before the second parse. The existing fixture then reports
`expected 2 card plays, got 1`. The temporary reproduction test was removed.

### Implementation tasks

- [x] Add a permanent regression test using the existing incremental-match fixture.
- [x] Choose and document a restart strategy: persist enough parsing context
  alongside the cursor, or replay from a checkpoint that reconstructs that context.
- [x] Inventory all state required after restart, including active match/game,
  player identity and seat, pending responses, and replay zone context. Restoring
  only the match ID may leave other restart gaps.
- [x] Ensure cursor and persisted context remain consistent across batch commits,
  cancellation, and failed transactions.
- [x] Preserve rotation detection and partial-record recovery behavior.
- [x] Add coverage through the live-service stop/start path.

### Acceptance checks

- Restarting between a full state and a diff that omits the match ID produces
  the same stored observations as uninterrupted parsing.
- Repeated restarts do not duplicate observations or attach them to another game.
- Existing rotation, replay-diff, partial-line, and multiline deck-submission
  tests continue to pass.
- Any replay-based recovery has a documented cost and avoids reparsing the whole
  log on every ordinary live poll.

### Implementation result

Completed September 30, 2026. Each ingest transaction now commits a versioned,
compressed per-log context alongside the byte/line cursor and parsed records.
The context includes match/game identity, seats, turn/phase, replay objects and
zones (including hidden identities needed by later diffs), pending response
headers, multiline collectors, and pending game deck snapshots. Player identity
and the completed-match rank queue are persisted across files in the same
transaction. Reused parsers reload durable state before parsing new lines, so a
failed or cancelled transaction cannot leave their next pass ahead of the cursor.

Existing databases retain their cursors during migration. When new lines arrive
without a supported per-log checkpoint, a rollback-only pass reconstructs the
saved prefix before ingesting the suffix. It does not commit duplicate raw events
or hydrate historical diffs from newer replay frames. Recovery costs one prefix
scan per successful upgrade; unchanged polls read cursor metadata and small
global context without loading or decoding the per-log blob. Rotation detection
and unterminated-tail handling remain in place. Full reparses and schema backfills
clear stale rank queues while preserving responses split between consecutive logs.

Checkpoint size and encoding work grow with the retained replay state. A local
synthetic fixture with 100 games and 100 objects per game produced about 5 MB of
JSON and 218 KB of stored compressed/base64 context; encoding and decoding took
about 25 ms and 42 ms respectively. These are fixture measurements, not guarantees
for real logs. A malformed or unsupported global checkpoint returns an explicit
error rather than silently losing cross-file context.

Regression coverage compares restarted and uninterrupted replay/card observations,
retries failed commits before and after a 500-line batch, reconstructs legacy
checkpoints without duplicate raw events, preserves split responses and deck
submissions, and exercises repeated live-service stop/start. Historical rank
responses cannot consume a newer pending match during a reparse. All Go tests
and race checks for ingest, appstate, and DB passed using
`GOCACHE=/tmp/ponder-review-go-cache`. Desktop GUI smoke checks were not performed.

Review follow-up, October 1, 2026: rotated logs now reuse matching saved cursors
and context, so importing `Player-prev.log` cannot replay an old rank response
against a newer pending match. When live tracking reads a replacement `Player.log`
first, it preserves the matching old checkpoint under `Player-prev.log` in the
same transaction. An explicit account identity now replaces a cached identity
and clears the former account's pending match, deck, response, and rank state.
Regressions cover both rotation orders, unparsed diffs after rotation, account
changes with and without restarts, and repeated or placeholder identities.
Account histories still share one database; this does not add account filtering.

## 2. P1 — CLI API listens on all interfaces by default

### Problem and evidence

`cmd/ponder/main.go:runServe` defaulted `-addr` to `:8080`. This listens on all
available interfaces. `internal/api/server.go:withHostCheck` accepts any parsed
IP address, so it does not restrict access to loopback clients.

The API has no authentication. Where the host's firewall and network permit
access, another machine can read tracker data and invoke runtime controls.
This finding concerns CLI serve mode. The production desktop app mounts its API
on the Wails asset server; its development API already uses loopback.

A temporary handler test confirmed that a request addressed to
`http://192.168.1.2:8080/api/health` passes the host check. This was an in-process
test, not an external network penetration test.

### Implementation tasks

- [x] Change the CLI default to `127.0.0.1:8080`.
- [x] Update CLI help, README examples, and scripts that imply the old default.
- [x] Keep explicit `-addr` overrides supported and document that a non-loopback
  binding exposes the unauthenticated API.
- [ ] Define host validation consistently with the supported deployment modes,
  preserving intended localhost, IPv6 loopback, and desktop behavior.
- [x] Add CLI option tests for the default address and explicit override.
- [ ] Add tests for accepted/rejected host cases as part of origin/host policy work.

### Acceptance checks

- Starting `ponder serve` without `-addr` opens only a loopback listener.
- Browser development, Vite proxying, and desktop development still work.
- Any explicitly supported remote-listen mode has documented access semantics.

### Implementation result

The CLI now defaults to `127.0.0.1:8080`, and `scripts/start-backend.sh` uses
that default instead of forcing `:8080`. Explicit `-addr` overrides remain
supported. CLI help and the README describe the new behavior.

Luna implemented the change, and the parent agent reviewed it. The new default
regression test failed with the old `:8080` value and passed after restoration
of the fix. `go test ./cmd/ponder ./internal/api`,
`bash -n scripts/start-backend.sh`, and `git diff --check` passed. Go tests used
`GOCACHE=/tmp/ponder-review-go-cache` because of the default cache's sandbox
permissions. Desktop/browser smoke checks and a live listener inspection were
not performed. Host validation and origin protection remain separate pending work.

## 3. P1 — CORS does not prevent untrusted-origin mutations

### Problem and evidence

`internal/api/server.go:withCORS` conditionally sets response headers but forwards
requests from untrusted origins to the same handlers. `decodeJSONBody` accepts
JSON regardless of the request's content type. Several control endpoints also
accept a POST without a body.

Withholding CORS headers prevents browser code from reading a response; it does
not, by itself, reject the write. A simple POST with `Content-Type: text/plain`
can contain JSON without requiring a CORS preflight. Actual browser access can
also depend on browser-specific local-network restrictions, so the server must
enforce its own boundary.

A temporary in-process test sent an untrusted `Origin` and a text/plain JSON body
through the host check and full router to `/api/runtime/open-url`. The handler
returned 200 and invoked a mocked desktop action. No real browser was opened.

### Implementation tasks

- [ ] Define an explicit origin policy for production desktop, CLI same-origin
  pages, and supported local development servers.
- [ ] Reject disallowed origins before executing state-changing handlers.
- [ ] Define handling for absent and `null` origins, including non-browser CLI
  clients and actual Wails request behavior.
- [ ] Validate media types for endpoints that require JSON. Treat this as an
  additional check, not a replacement for origin validation.
- [ ] Apply protection to bodyless mutations as well as JSON mutations.
- [ ] Add handler tests asserting both rejection and absence of side effects.

### Acceptance checks

- An untrusted-origin text/plain POST is rejected before a mocked action runs.
- An untrusted-origin bodyless POST cannot start or stop tracking.
- Disallowed preflights do not grant access.
- Valid same-origin, supported development, and desktop requests still succeed.
- The missing-Origin policy is explicit and covered by tests.

## 4. P2 — Yearly activity is calculated from only 500 matches

### Problem and evidence

`web/src/pages/OverviewPage.tsx` originally fetched `api.matches(MATCH_WINDOW)` with
`MATCH_WINDOW = 500`, then called `dailyActivity(allMatches, ACTIVITY_DAYS)` with
`ACTIVITY_DAYS = 365`.

The chart, total, and accessible label described the last 365 days without
disclosing the match cap. A player with more than 500 matches in that period
therefore saw understated totals and incomplete historical days. This finding
was established by tracing the fetch, aggregation, and display code.

### Implementation tasks

- [x] Prefer a backend daily-activity aggregate over the complete requested date
  range, independent of the match-list limit.
- [x] Define local-day boundaries explicitly so the new aggregation preserves
  the existing local-calendar behavior, including daylight-saving transitions.
- [x] Return the per-day data needed by the graph and hover details: counts,
  results, tracked duration, and format mix.
- [x] Keep recent-match lists bounded and separate from yearly activity data.
- [x] Remove the activity cap entirely; no interim cap disclosure is needed.
- [x] Add a fixture with more than 500 matches inside the year and additional
  matches outside it.

### Acceptance checks

- All in-range matches contribute to yearly counts, even when there are more
  than 500; out-of-range matches do not.
- Chart cells, hover details, displayed totals, and accessible labels agree.
- Local midnight and daylight-saving boundary cases behave consistently.
- Loading the yearly chart does not require downloading every match record.

### Implementation result — September 30, 2026

`POST /api/activity` accepts 1–366 ordered, consecutive local dates with
contiguous explicit midnight intervals. The browser generates the boundaries,
including 23/25-hour DST days, and refreshes its calendar key while mounted and
when returning to the app. The store scans only scalar activity fields in the
requested range, aggregates without a match limit, and returns one row per day.
Precise timestamp comparisons assign days after a padded SQLite range prefilter,
so submillisecond timestamps immediately before midnight remain on the correct
day. Positive durations retain the match-list end/start fallback; nonpositive
explicit durations remain untracked.

The graph, hover details, visible total, and accessible labels now all consume
the same server totals. Activity has independent loading/error states. The
existing 500-match request continues to serve recent-match statistics.

Regression coverage includes 601 matches inside a complete 365-day range,
matches just outside both endpoints, empty days, result/duration/format totals,
offset timestamps, and both DST transitions with nanosecond midnight cases.
The full-year API regression failed with 404 before the endpoint was added.
Focused Go tests, the full frontend test suite (193 tests), TypeScript
typechecking, and the production build passed. The full Go suite also passed
during independent review. Build warnings concerned existing bundle size and
Browserslist data freshness.
An interactive browser/desktop smoke test was not performed.

## 5. Follow-up — Reduce match-detail page complexity

Implemented October 1, 2026. `web/src/pages/MatchDetailPage.tsx` now handles route
queries, section selection, and shared game selection in 233 lines, down from
4,919. Focused modules under `web/src/components/match-detail/` own replay boards,
controls, battlefield rendering, connections, zone inspection, previews,
analytics, sideboarding, overview, opponent cards, and review presentation.

[Component boundaries and state ownership](match-detail-components.md) describe
where each responsibility lives. `useMatchReplayView` groups replay/timeline data
and eager card previews; a shared card-preview key helper preserves the existing
cache keys. Playback, keyboard handling, persisted preferences, and review
streaming still use their existing hooks/components. Shared game selection and
keyed board/review resets retain their previous behavior.

- [x] Map state ownership and component boundaries before moving code.
- [x] Reuse the existing `web/src/lib/replay/` hooks where appropriate.
- [x] Extract cohesive sections incrementally, preserving behavior and query keys.
- [x] Verify replay navigation, keyboard controls, card previews, game switching,
  and AI review behavior across the extraction.
- [x] Avoid combining this restructuring with the correctness fixes above.

Validation: `bun run typecheck`, `bun run build`, all 193 frontend tests,
`go test ./...`, and `git diff --check` passed. Browser smoke checks used the
production frontend build with temporary local API/card fixtures: replay and
fallback navigation, playback/speed, keyboard seeking, card preview focus, zone
dialogs, shared game tabs, sideboard changes, analytics, opponent cards, review
streaming/cancellation/completion, and empty states. No real AI provider was
invoked, and the native desktop shell was not exercised. Existing build warnings
about chunk size and stale Browserslist data remain.

## Validation and completion

Baseline review results:

- `go test ./...` passed.
- `bun test` in `web/` passed: 190 tests.
- `bun run typecheck` in `web/` passed.
- `go vet ./...` passed using a writable temporary Go cache after the default
  cache encountered a sandbox permission error.
- Temporary targeted tests reproduced the restart and API-boundary findings;
  they were removed after the review. The codebase was left unchanged.

For each implementation:

- [ ] Add the relevant regression coverage and confirm it fails before the fix.
- [ ] Run the affected Go packages and/or frontend tests and type checks, using
  Bun for frontend commands.
- [ ] Run the complete suites before considering the plan finished.
- [ ] Perform browser/desktop smoke checks for changes to origin handling or UI.
- [ ] Record the chosen behavior, validation results, and any remaining limitations
  in this document or the implementing PR.
