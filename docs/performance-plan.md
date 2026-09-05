# Performance Plan

Findings from a performance investigation (2026-07-12), focused on database size,
with query/runtime improvements included. Baseline: `data/ponder.db` is 8.6MB for
75 matches (~115KB/match), dominated by `match_replay_archives` (5.0MB, ~60%) and
`events_raw` + indexes (2.5MB, ~30%).

## 1. Shrink and cap `events_raw` (~30% of DB, ~80% dead weight)

**Finding.** The only reader of `events_raw` is `RepairDraftDataFromRawEvents`
(`internal/db/store_drafts.go`), which needs `kind='outgoing'` rows for a handful
of draft/deck methods. That subset is 835 of 7,464 rows (444KB of 2.1MB).
Everything else is written and never read:

- 3,780 `method_complete` / `room_state` rows with zero payload
  (`parser.go:572`, `match.go:248`)
- `method_result` rank response lines (`rank.go:82`) — the parsed data already
  lands in `match_rank_snapshots`
- `outgoing` payloads for methods repair never touches (`DeckUpsertDeckV2` alone
  is 126KB)
- `outgoing_unparsed` marker rows

The table also has no retention: it spans Feb–Jul and grows unboundedly.

**Plan.**
- [x] Stop inserting kinds/methods nothing reads. Keep an allowlist:
      `LogBusinessEvents` (EventType 24 only), `EventPlayerDraftMakePick`,
      `DraftCompleteDraft`, `EventSetDeckV2`, `EventSetDeckV3`.
- [x] Add `PruneRawEvents` to delete already-stored rows outside the allowlist,
      run as part of startup maintenance.
- [x] Drop `idx_events_raw_kind` (148KB; low cardinality, repair filters pair
      kind with method and the method index suffices).

## 2. Replay archive compression level (~30% off the biggest table)

**Finding.** Archives compress ~100x with zstd `SpeedBetterCompression`
(`replay_archive.go:89`). Re-encoding the three largest blobs at
`SpeedBestCompression` measured ~31% smaller (486→336KB, 342→237KB, 265→183KB).
Archiving happens once per completed match, so encode speed is irrelevant.

**Plan.**
- [x] Switch the encoder to `zstd.SpeedBestCompression`.
- [x] One-time recompress of existing archives during startup maintenance,
      guarded by an `app_metadata` flag so it runs once.

## 3. Drop redundant indexes (space + write amplification during live ingest)

**Finding.** Several indexes duplicate the leftmost prefix of a UNIQUE
constraint's autoindex (or another index) on write-heavy tables:

- `idx_match_replay_frames_match_game_state` — exact duplicate of
  `UNIQUE(match_id, game_number, game_state_id)`
- `idx_match_card_plays_match_id` — prefix of `UNIQUE(match_id, game_number,
  instance_id)` and of `idx_match_card_plays_turn_order`
- `idx_match_opponent_cards_match_id` — prefix of its UNIQUE
- `idx_match_replay_frame_objects_frame_id` — prefix of
  `UNIQUE(frame_id, instance_id)` and of the zone index
- `idx_events_raw_kind` — covered in item 1

**Plan.**
- [x] Remove from `schema.sql`, add `DROP INDEX IF EXISTS` migration in
      `db.Init`, and stop recreating them in the legacy table-rebuild paths in
      `db.go`.

## 4. Move draft repair out of the API request path

**Finding.** `ListDraftSessions` and `ListDraftPicks` call
`RepairDraftDataFromRawEvents` on every `/api/drafts*` request — ~12 correlated
subqueries running `json_extract` scans over `events_raw`. Cost grows with the
table; it belongs after ingest, not on read.

**Plan.**
- [x] Remove the repair calls from the two list methods.
- [x] Run repair at the end of `Parser.ParseFile` when draft-relevant activity
      was ingested, and once during startup maintenance (covers pre-existing
      gaps without any request-path work).

## 5. Connection/pragma hygiene (correctness + UI responsiveness)

**Finding.**
- `db.Open` uses a bare path DSN and `SetMaxOpenConns(1)`: every API read
  queues behind ingest write transactions (batches of 500 lines) even though
  WAL exists to let readers run alongside a writer.
- `PRAGMA foreign_keys = ON` is applied once in `schema.sql` at Init, but the
  pragma is connection-scoped. If `database/sql` recreates the connection,
  `ON DELETE CASCADE` silently stops firing and orphan rows accumulate.
- No `busy_timeout`; `synchronous` is left at FULL, which is slower than the
  recommended WAL pairing (NORMAL) with no durability benefit for this app.

**Plan.**
- [x] Build a `file:` DSN with per-connection pragmas:
      `busy_timeout(5000)`, `foreign_keys(1)`, `journal_mode(WAL)`,
      `synchronous(NORMAL)`, plus `_txlock=immediate` so concurrent write
      transactions queue on the busy handler instead of failing.
- [x] Raise the pool to a small number of connections so reads run
      concurrently with ingest writes.

## 6. Consolidated startup maintenance

**Plan.**
- [x] Single `Store.RunMaintenance` that: compacts replays, runs the one-time
      archive recompress, prunes `events_raw`, repairs draft data, VACUUMs when
      anything was reclaimed, and truncates the WAL. Call it from the desktop
      app startup (`app.go`), `serve`, `parse`, `tail`, and `compact` commands
      in place of the current `CompactAndVacuumMatchReplays` calls.

## Results (measured 2026-07-12 after implementation)

All items above are implemented and verified. The dev backend (air hot reload)
picked up the new code and ran the first maintenance pass against
`data/ponder.db`:

- On-disk footprint: 8.6MB db + 8.7MB WAL → **5.4MB db + 0B WAL** (~69% smaller)
- `events_raw`: 7,464 rows / 2.1MB → 360 rows / 332KB, and now capped by the
  insert filter
- `match_replay_archives`: 5.0MB → 3.9MB after the one-time recompress
- Verified: `PRAGMA integrity_check` ok, zero `foreign_key_check` violations,
  all 63 archives decode and match their recorded frame counts, and
  `/api/overview`, `/api/matches`, `/api/matches/{id}` (archived replay),
  `/api/drafts`, `/api/drafts/{id}/picks`, `/api/live` all return correct data
  on the running dev server.

## Future work (not in this pass)

- **Delta-encode replay frames (schema v2).** Each frame stores the full object
  list; consecutive frames are nearly identical. Raw JSON for the largest match
  is 53MB decompressed — disk cost is hidden by zstd, but every replay read and
  archive merge unmarshals all of it. Per-frame diffs would cut disk further and
  eliminate the ~50MB memory spikes. Biggest structural win, real project.
- **N+1 in `enrichDraftSessionsWithDeckResults`** — per-session queries; N is
  small today.
- **Manual cleanup (not automated, user data):** `data/` holds ~1.7MB of old dev
  DBs (`ponder-mvp*.db`, `ponder-fix-opponent*.db`, `-shm` files) and a 34MB
  test log. Directory is gitignored; delete locally if unwanted.

## Non-findings (checked, already good)

- Ingest batches 500 lines/transaction.
- Match list endpoints batch card-quantity lookups (no N+1 on hot paths).
- Frontend polling is modest (2s live / 5s idle / 30s overview) and pauses in
  background.
- Replay compaction + VACUUM at startup keeps the freelist at zero.

## Optimization plan — 2026-09-05

This plan follows a read-only review of the current Go/SQLite backend, React
frontend, and Wails v3 integration. It extends the completed July work above;
the unchecked items below are recommendations, not implemented changes.

### Goals and scope

- Eliminate unnecessary work while live tracking is idle.
- Reduce replay loading allocations, payload size, and repeated playback work.
- Reduce initial frontend JavaScript loading and evaluation.
- Bound long-running parser memory and history-dependent repair work.
- Preserve log recovery, replay correctness, and existing SQLite safeguards.

No evidence from this review justifies replacing Wails or the current database
architecture. Native WKWebView performance still needs direct measurement.

### Evidence and measurement limits

The review exercised the actual parser, replay store, and HTTP handler with
synthetic data in a disposable SQLite database. The existing production frontend
build was opened in Chromium. No gameplay database was available at the expected
local paths. Temporary probes, the server, and synthetic data were removed.

**Idle polling:** 30 unchanged-log polls read zero new bytes, allocated
**4.002 MiB per call**, and took **0.207 ms median**. At the default two-second
interval, this corresponds to approximately **7 GiB/hour of cumulative allocation
churn**, not retained memory. CPU time per call is small; battery impact was not
measured.

**Replay loading:**

| Synthetic replay | Response JSON before gzip | Gzip API median | Store-load allocations |
| --- | ---: | ---: | ---: |
| 100 frames × 40 objects/frame | 1.83 MB | 10.5 ms | 9.1 MiB |
| 500 frames × 80 objects/frame | 18.36 MB | 105 ms | 89.0 MiB |
| 1,000 frames × 120 objects/frame | 55.23 MB | 317 ms | 221.4 MiB |

- API timings are medians of five warm measurements through the real handler
  using an in-process response recorder. They exclude browser/network time.
- Store allocations are cumulative allocated bytes per load, not peak heap,
  retained memory, or total HTTP-handler allocations.
- Synthetic archives were seeded using zstd's fastest level; these measurements
  do not compare production compression levels or measure completion-time encoding.
- Chromium took approximately **42 ms median** to `JSON.parse` the largest
  response, excluding React preprocessing and rendering.
- These fixtures demonstrate scaling costs, not typical Arena match sizes.

**Replay traversal:** sequentially visiting 1,000 synthetic frames through
`buildReplayAttachmentState` performed **500,500 annotation parses** and took
approximately **168 ms total** in Bun. This is cumulative helper execution,
not a single-frame stall or a full React playback benchmark.

**Frontend bundle:** the existing production build loaded one **2.01 MB**
JavaScript bundle on Overview, approximately **640 KB** when gzip-compressed.
No before/after startup improvement was measured.

### Implementation order

| Order | Work | Expected benefit | Scope |
| --- | --- | --- | --- |
| 1 | Idle-poll fast path | Less idle allocation and checkpoint writing | Small |
| 2 | Replay freshness and projection allocation | Fewer repeated loads and intermediate allocations | Small–medium |
| 3 | Route-level code splitting | Less initial JavaScript work | Small–medium |
| 4 | Replay attachment indexing | Remove quadratic playback prefix scans | Medium |
| 5 | Bounded parser state | Bound memory across a long live session | Medium |
| 6 | Incremental historical repairs | Avoid repeated all-history queries/writes | Medium |
| 7 | Chunked/keyframe replay format | Reduce full-match expansion and transfer | Large |

Idle polling, route splitting, and replay indexing can be investigated
independently. Parser eviction and replay-format changes need explicit recovery
and late-message contracts before implementation.

### 1. Make unchanged log polls nearly free

**Finding before implementation.** `Parser.ParseFile` in
`internal/ingest/parser.go` allocated a 4 MiB reader and began a write transaction
before discovering unchanged EOF. The final path still saved and committed the
ingest checkpoint on each live-tracking poll.

**Plan.**
- [x] Add a no-change return after validating a non-zero cursor, file size, and
      signature, before reader allocation and transaction creation.
- [ ] Avoid checkpoint writes when neither the durable cursor nor its recovery
      state changed, including polls containing only an incomplete trailing line.
- [ ] Allocate the large reader only for actual parsing; measure whether a
      smaller buffer is sufficient before changing its size.

**Invariants.** Preserve same-size replacement detection, truncation handling,
unterminated-tail recovery, pending logical-record checkpoints, and 500-line
transaction batching. A size-only shortcut is not safe.

**Verification.** Re-run the unchanged-log allocation probe and observe no
checkpoint UPDATE/commit or 4 MiB reader allocation on ordinary unchanged EOF.
Exercise appended complete lines, incomplete lines completed on a later poll,
truncation, same-size replacement, and recovery from a pinned checkpoint.
Keep regression coverage for any newly introduced recovery boundary.

**Implemented — 2026-09-05.** The fast path applies only when a validated,
non-zero durable cursor equals the file size. It reports a completed idle poll
without creating a reader or acquiring the SQLite writer lock. Zero/pinned
checkpoints, empty files, incomplete trailing lines, and explicit full reparses
retain their existing processing paths. Buffer resizing and incomplete-tail
write suppression remain unchecked follow-ups above.

Measured with the same disposable Go probe before and after the change, using
30 unchanged polls per run and GC before each sample:

| Metric | Before | After |
| --- | ---: | ---: |
| Allocated bytes per poll | 4,197,116 | 1,422 |
| Median time per poll | 0.126 ms | 0.026 ms |
| Polls changing the database | 30 / 30 | 0 / 30 |

A dedicated observer connection checked SQLite `data_version` after each poll.
These are synthetic parser measurements, not native-window or battery results.

Verification completed:
- The new writer-contention regression failed before the fix with `SQLITE_BUSY`
  and passes after it; unchanged polling no longer needs the writer lock.
- Regression coverage checks same-size replacement and in-memory match-state
  preservation across an idle poll between appends.
- The disposable smoke probe passed append-after-idle, incomplete/completed
  trailing lines, shorter replacement, full reparse, truncation to empty, and
  append-after-empty-reset scenarios.
- `go test -race ./...` passed all seven packages with tests, including
  multiline/pinned-checkpoint restart recovery. The linker emitted macOS
  deployment-target warnings; they did not prevent the test run.
- Temporary probe source and its disposable database were removed.

### 2. Reduce repeated replay loads and projection allocations

**Finding.** `loadReplayArchivePayload` in `internal/db/replay_archive.go`
decompresses and unmarshals the entire archive. `loadArchivedMatchReplayFrames`
constructs another API-shaped object graph, and `ListMatchReplayFrames` in
`internal/db/store_replay.go` derives changes across the frames.

The replay query in `web/src/pages/MatchDetailPage.tsx` has no explicit
`staleTime`, so default query behavior can refetch a completed replay on
remount/focus.

**Plan.**
- [ ] Give completed replay queries an explicit freshness policy.
- [ ] Define invalidation for reparsing, late frames, and other replay changes;
      completed does not mean permanently immutable.
- [ ] Keep query retention bounded so browsing multiple large replays does not
      retain all decoded object graphs indefinitely.
- [ ] Preallocate object slices using known archive object counts during
      archive-to-API conversion.

**Verification.** Observe request counts when leaving/re-entering a completed
replay and hiding/showing the app. Confirm a changed replay refreshes correctly.
Repeat store/API allocation measurements and observe heap retention after
browsing several large replays and leaving the route. Preserve archive/live-row
merge precedence and replay-change semantics.

### 3. Split the startup JavaScript bundle

**Finding.** `web/src/App.tsx` statically imports every route, including replay,
chart, and AI Markdown dependencies.

**Plan.**
- [ ] Lazy-load routes with `React.lazy` and a route-level `Suspense` boundary.
- [ ] Split optional replay/review panels when route splitting alone still
      loads heavy code before it is needed.
- [ ] Use modular ECharts imports for the chart types actually rendered.
- [ ] Add preloading only if measured first-navigation latency warrants it.

**Tradeoff.** Smaller initial work introduces chunk loading on first navigation.
Moving code into vendor chunks without deferring imports is not equivalent to
lazy loading.

**Verification.** Build production assets and compare initial-route requested
JavaScript bytes and parse/evaluation time. Exercise every route, direct deep
links, and navigation in the packaged Wails app to verify dynamic chunk URLs
through the asset server. Record initial-load and first-navigation tradeoffs.

### 4. Index replay attachment state instead of rescanning prefixes

**Finding.** `buildReplayAttachmentState` in `web/src/lib/replay/index.ts`
walks frames zero through the selected index and reparses annotations.
`MatchDetailPage` invokes it as the selected frame changes. Sequential traversal
therefore performs quadratic cumulative prefix work.

**Plan.**
- [ ] Parse annotations once per loaded replay.
- [ ] Build attachment state incrementally, using indexed events or checkpoints
      to support backward/random seeking.
- [ ] Reuse existing `ReplayRelationshipIndex` and scrubber caching patterns.
- [ ] Avoid full copied attachment-state snapshots for every frame unless
      memory measurements justify that representation.

**Verification.** Compare sequential traversal at 250, 500, and 1,000 frames;
annotation decoding should no longer scale with the sum of all frame prefixes.
Verify backward seeks, repeated seeks, game switches, attachment replacement,
and visibility filtering. Profile actual autoplay and scrubbing in the UI;
the helper benchmark alone does not establish frame-rate improvement.

### 5. Bound long-lived parser state

**Finding.** `rememberReplayState` in `internal/ingest/gre.go` retains object
maps keyed by match/game. Completion processing in `internal/ingest/match.go`
archives replay data but does not evict those maps and associated per-match
metadata. A live-tracking session retains one parser across polls.
Retention is confirmed from code; retained-byte growth was not measured.

**Plan.**
- [ ] Define which state is required after a terminal match event.
- [ ] Bound the recent completed-match window and evict older replay/metadata
      entries together.
- [ ] Preserve separately required rank-association and pending-record state.
- [ ] Define rehydration or bounded retention behavior for late GRE messages
      and reconnects before choosing an eviction boundary.

**Verification.** Feed increasing numbers of completed matches through one
persistent parser and measure retained heap after GC. State should remain
bounded by the active match and chosen recent window. Verify late-message,
reconnect, and log-rotation behavior without losing stored replay information.

### 6. Make historical repairs incremental

**Finding.** `RepairEventRunInstances` in `internal/db/store_events.go` scans
historical drafts and performs per-draft queries/updates. It runs in `db.Init`,
again in `Store.RunMaintenance`, and after draft-relevant ingestion.
`RepairDraftDataFromRawEvents` also performs global JSON repair after relevant
ingest activity. Current repair latency was not measured.

**Plan.**
- [ ] Give legacy repair one versioned owner rather than running the same
      all-history pass twice at startup.
- [ ] During normal ingestion, repair only affected drafts/event runs and
      newly linkable transactions.
- [ ] Avoid rewriting unchanged derived values and timestamps.
- [ ] Preserve recovery for out-of-order and incomplete records.
- [ ] Keep global repair available for explicit recovery or versioned backfills.

**Verification.** Measure startup and one new draft event against increasing
synthetic history sizes. Observe statement counts and updated rows, not just
elapsed time. Verify normal ingest work is scoped to affected records and that
versioned recovery still fixes historical gaps. Do not delete raw repair
evidence merely to make scans smaller.

### 7. Reduce full-match replay expansion structurally

**Finding.** Compression controls disk size but not full-archive decoding,
API object construction, response JSON expansion, or browser heap size.
This carries forward July's unfinished delta-encoding work.

**Plan.**
- [ ] Define a versioned replay representation with keyframes and deltas.
- [ ] Make independently loadable chunks align with useful access boundaries,
      such as games or bounded frame ranges.
- [ ] Support sequential playback and random seeking without decoding the
      entire match.
- [ ] Migrate stored archives and all consumers, including analytics and AI
      review generation, with explicit archive/live-row merge semantics.
- [ ] Remove obsolete runtime paths after successful migration; avoid a
      permanent second replay implementation.

**Tradeoff.** This is a storage/API/client change with migration and seeking
complexity. Returning a smaller slice only after whole-archive decoding reduces
browser work but does not solve backend expansion.

**Verification.** Compare response bytes, load time, cumulative allocations,
peak/retained heap, and seek latency across small and large replays. Verify
frame equivalence, partial-log recovery, late live-row overrides, interrupted
migration recovery, analytics output, and AI review inputs. Choose quantitative
targets after capturing representative real-log baselines.

### Preserve existing optimizations

- Per-connection foreign keys, WAL, busy timeout, NORMAL synchronous mode, and
  the small database connection pool.
- 500-line transaction batching during active ingestion.
- Raw-event allowlisting and removal of repair from draft API read paths.
- Match-list virtualization and batched card-quantity lookups.
- Modest frontend polling and disabled background polling for live queries.
- The disabled WebGL background: Chromium inspection found no running canvas.

### Profile before expanding scope

- **Native hide/show behavior.** Wails hides the window rather than destroying
  the webview. Measure `document.visibilityState`, request counts, autoplay,
  and CPU while hidden. Add a native visibility bridge only if browser
  visibility handling does not pause the relevant work.
- **Large match history.** The frontend fetches up to 20,000 matches and filters
  locally. Virtualization already bounds DOM rows. Measure payload size,
  retained heap, and input latency before adding server-side filtering and
  pagination; preserve grouped-history behavior and totals if that changes.
- **Startup contention.** Maintenance overlaps auto-started ingestion and API
  use. Measure writer waits and first-use latency before changing scheduling.
  Eliminate duplicate repair first; background work is not automatically free.

### Completion criteria

Each implemented item should include before/after measurements of its affected
path, correctness checks for its stated invariants, and updated documentation.
Use disposable probes for straightforward performance comparisons; retain
tests where they defend plausible behavioral regressions. Do not treat the
synthetic baselines above as native WKWebView or typical-user latency claims.
