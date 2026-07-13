# Performance Plan

Findings from a performance investigation (2026-07-12), focused on database size,
with query/runtime improvements included. Baseline: `data/mtgdata.db` is 8.6MB for
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
`data/mtgdata.db`:

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
  DBs (`mtgdata-mvp*.db`, `mtgdata-fix-opponent*.db`, `-shm` files) and a 34MB
  test log. Directory is gitignored; delete locally if unwanted.

## Non-findings (checked, already good)

- Ingest batches 500 lines/transaction.
- Match list endpoints batch card-quantity lookups (no N+1 on hot paths).
- Frontend polling is modest (2s live / 5s idle / 30s overview) and pauses in
  background.
- Replay compaction + VACUUM at startup keeps the freelist at zero.
