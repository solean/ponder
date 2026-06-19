# Game Replay — Redesign Plan

*A plan to 100× the UI/UX of the match replay feature. Grounded in the current
implementation (`web/src/pages/MatchDetailPage.tsx`, ~4,090 lines) and the
amber-on-black "night operations" design system in `web/src/styles.css`.*

Status markers: ✅ done · 🔲 open · 🟡 in progress

> **Progress — 2026-06-16:** ✅ **Phase 0 complete.** The enabling refactor
> shipped: pure replay logic extracted to `web/src/lib/replay/index.ts` (78
> declarations; the page dropped ~4,090 → ~3,030 lines), the duplicated transport
> state machine unified into `web/src/lib/replay/useReplayPlayer.ts`, and a test
> runner wired up (`bun test`) with `web/test/replay.test.ts`. No behavior change
> — verified by `tsc -b`, `bun run build`, and **21 passing tests**.
>
> ✅ **Phase 1 complete.** Persistent HUD (`MatchReplayHud` — big life totals
> with a Δ flash, turn/phase, step counter, beat headline, change pills) replaces
> the buried status sentence; keyboard transport (`useReplayKeyboard`: ←/→ step,
> ⇧+←/→ turn, space play, Home/End); and a 0.5×/1×/2×/4× speed control on the
> hook (`useReplayPlayer`). Wired into both the frame board and the observed
> timeline board. **22 tests pass**; verified live against match 675 (895 frames)
> — HUD renders, Shift+← jumps turns, a −7 combat swing flashes red, 2× speed
> toggles, no console errors.
>
> ✅ **Phase 2 complete.** `MatchReplayScrubber` retires the turn-pill row: a
> full-width track with a dual life sparkline (green you / red opponent across the
> whole game), turn-boundary labels, event ticks colored by kind (combat/spell/
> life), a draggable playhead, and click/drag-to-seek. Backed by new pure helpers
> `buildReplayLifeSeries` / `replayLifeSeriesDomain` / `replayFrameTickKind` /
> `buildReplayTickKinds`. **26 tests pass**; verified live on match 675 — sparkline
> traces both life totals, T?–T13 labels render, clicking 75% jumps to step 67
> (Turn 11) with the HUD in sync, no console errors.
>
> ✅ **Phase 3 complete.** The board became a mirrored arena (opponent on top,
> you at the foot, stack centered) on a full-width single column;
> `MatchReplayFrameSideSummary` gained a `variant="rail"` mode. *(Superseded by
> the Phase 4 layout realignment below — see note.)*
>
> ✅ **Phase 4 + layout realignment complete.** Course-corrected to match the
> original concept mockup, which the single-column Phase 3 had drifted from:
> - **Two-column layout** — the mirrored arena on the left, a **play-by-play
>   rail** on the right (`MatchReplayMoveList`: turn-grouped beats, current beat
>   highlighted + auto-scrolled, click-to-jump). The stack moved from the center
>   line to the **top-right** of the board; the rail is sticky while the board
>   scrolls.
> - **Humanized, coalesced narration** (`buildReplayBeat`) feeds both the rail and
>   the HUD headline: attacks/blocks with P/T ("You attack with Floodpits Drowner
>   (2/1) and 1 more"), casts, plays (with "tapped" notes), tap/untap counts, and
>   life swings ("Life change · opponent 16 → 11"). GRE no-op "noise moves"
>   (Hand→Hand, Limbo→Limbo) are now dropped from the timeline
>   (`replayChangeIsNoiseMove`), and board-resync battlefield entries no longer
>   masquerade as plays.
> - **HUD avatars** (OP / YOU) flanking the life pods.
>
> **32 tests pass**; verified live on match 675 — two-column board + rail render,
> avatars present, narration is clean (no "Limbo", no resync "and N more"), the
> rail tracks the playhead, no console errors. Remaining narration polish (a few
> raw GRE phrasings like "lost public visibility of X") is a follow-up.
>
> ✅ **Board density pass.** The arena was still too tall to take in without
> scrolling (~1900px). Shrank board cards (108→56px) and hand cards (116→74px),
> tightened lane/section spacing, dropped the redundant "Tapped"/"Attacking" text
> pills (already shown by the 90° rotation / red border), and — the big one —
> laid each battlefield's type-sections **side-by-side** (lands | creatures |
> artifacts) instead of stacking them. Result: arena **~1900px → ~800px**, so the
> whole board (both battlefields + hand) now fits one screen. Verified live on
> match 675.
>
> ✅ **Phase 5 (story + polish).** Auto-detected **key-moment pins** on the
> scrubber (`findReplayKeyMoments`: the decisive 0-life step + the biggest life
> swings) — clickable diamonds that jump straight there. **Narration polish**:
> friendly phrasings for permanents leaving play ("X leaves the battlefield / is
> put into the graveyard / is exiled / returns to hand"), spells ("X resolves"),
> reveals ("You reveal X"), and hidden info ("X is no longer revealed"), plus
> **"T?" → "Pre" / "Pre-game"**. **36 tests pass**; verified live on match 675 —
> 3 swing pins render and a pin click jumps to step 28, narration is clean, no
> console errors. *Deferred from Phase 5: combat "moments" and FLIP card-motion
> animations — the former is data-limited (no block-link IDs in sample logs) and
> the latter is hard to verify reliably here; both are good future work.*

---

## TL;DR

The replay is **functionally rich but perceptually a database viewer**. The data
model is genuinely excellent — every frame carries per-object power/toughness,
tapped/summoning-sick, attack & block state, counters, controller-vs-owner
(stolen), plus frame-level life totals, win reason, and an annotations stream
already mined for spell targets and exile-under-card. That is a *complete
game-state diff stream*.

The UI spends that richness on clinical narration and vertical stacking. The work
is not "add features" — it is **re-found the screen around watching a game**, then
layer analysis affordances on top. Five moves do most of it: a mirrored arena, a
persistent HUD, a scrubber with a life sparkline, a turn-grouped play-by-play
rail, and keyboard + speed controls.

Phases 1–3 alone deliver ~80% of the perceived "100×."

---

## Core diagnosis

What's strong:
- The amber-on-black terminal aesthetic is distinctive and consistent.
- Real card art, tapped/summoning-sick badges, combat/spell-target arrows,
  zone viewer dialogs, linked exile stacks — all already implemented.
- The underlying frame/object/change/annotation model is complete and per-step.

What's holding it back:
1. **Vertical scroll instead of a board.** The board is three lanes stacked
   top-to-bottom (`opponent → you → hand`) that you scroll through
   (`MatchDetailPage.tsx` ~3010). You never see the whole game state at once. A
   real MTG table is two mirrored halves seen together.
2. **Life totals are buried.** They live as stat tiles in a left sidebar and are
   re-stated inside a muted sentence —
   `"30 tracked cards visible • stack empty • You 20 • Opponent 18"`
   (`frameVisibilitySummary`, ~2801). The most important number in the game is
   the hardest to find.
3. **Narration is literal GRE-speak.** `describeReplayChange` (~1255) emits
   *"Opponent declared Otter as a blocker."* — correct, but a log line, not a
   play-by-play.
4. **Navigation is thin.** Five buttons + a turn-pill row (~2866–2942). No
   scrubber, no keyboard shortcuts, no speed control, no life graph. Autoplay is
   hardcoded to `1200ms` (~2572). Keyboard nav exists only on the game *tabs*
   (~3727), not the board.
5. **Inverted hierarchy.** Section chrome (`OPPONENT BATTLEFIELD`, `LANDS`,
   `ARTIFACTS + ENCHANTMENTS`…) is as visually heavy as the cards. The labels
   shout, the cards whisper.

---

## The five big moves

### 1. A mirrored "arena" that fits one screen ✅
Collapse the sidebar + vertical lanes into one board taken in at a glance:
opponent on top, you at the foot, hand below, stack top-right, life in the HUD.
*Done across Phase 3/4 + the density pass: the arena is the left column of a
two-column canvas (board + play-by-play rail); off-board zones are compact rails;
and each battlefield's type-sections sit side-by-side. With small board cards the
whole board now fits ~one screen (arena ~800px, down from ~1900px) — no more long
vertical scroll. The connection overlay stays wired to the arena surface.*

### 2. A persistent HUD strip ✅
Both players' life **big**, a Δ flash on change (reuse `replayFrameHasLifeDelta`,
~992), turn + phase, and the current beat as a **headline**. Replaces the buried
`frameVisibilitySummary` sentence. The "what's happening now" anchor that never
moves. *Done in Phase 1 (`MatchReplayHud`); active-player indicator deferred
since the frame data doesn't expose it cleanly.*

### 3. A scrubber with a dual life sparkline ✅
REVIEW items 5 & 6 in one component. A full-width track: event ticks colored by
kind, turn boundaries marked, a draggable playhead, and your/opponent life drawn
as two lines across the whole game (the comeback, the burn turn made visible) and
clickable. *Done in Phase 2 (`MatchReplayScrubber`); replaced
`MatchReplayTurnSelector` entirely. The frame board passes a carried-forward life
series + tick kinds; the observed timeline board gets the track + turn markers
without the sparkline (no life data on plays).*

### 4. A turn-grouped play-by-play rail ✅
The "List" view reborn as a chess-style move list that co-pilots the board.
Current beat highlighted + auto-scrolled, click any line to jump, turns as group
headers. *Done in Phase 4 (`MatchReplayMoveList`), in a two-column layout beside
the arena.*

### 5. Keyboard + speed ✅
`←/→` step, `Shift+←/→` turn, `Space` play/pause, `Home/End`, and a
0.5×/1×/2×/4× speed control. Cheapest win, highest "feels pro" payoff. *Done in
Phase 1 (`useReplayKeyboard` + speed state in `useReplayPlayer`); the global
listener ignores focused inputs/buttons/tabs so it never double-fires.*

---

## Polish wins that punch above their weight

- **Coalesce GRE noise and humanize narration** (REVIEW item 4) ✅ — *Done in
  Phase 4–5 (`buildReplayBeat`): no-op moves (Hand→Hand, Limbo→Limbo) are dropped
  from the timeline; attackers/blockers fold into one beat with P/T; plays note
  "tapped"; life swings show before→after; permanents leaving play, spells
  resolving, reveals, and hidden info all read in plain English. Remaining: a rare
  hand→Limbo bookkeeping move still shows raw, and cast→resolve spans separate
  beats rather than merging.*
- **Make combat a moment** 🔲 — the attack/block arrows
  (`MatchReplayConnectionOverlay`, ~1674) are nice and underused. In an arena
  layout, highlight the combat step, advance attackers toward the defender, show
  damage math.
- **Card motion between zones (the real 100× kicker)** 🔲 — `instanceId` is
  stable across frames, so a FLIP transition can slide a card
  hand→stack→battlefield→graveyard as you step. Converts "stepping through
  snapshots" into "watching a replay." Big effort, biggest wow.
- **Auto-detected key moments** ✅ — *Done in Phase 5 (`findReplayKeyMoments` +
  scrubber pins): clickable diamonds mark the decisive 0-life step and the biggest
  life swings, turning a long replay into a skimmable story. (Mulligan detection
  not yet included.)*
- **Rename "T?" → "Pre-game"** ✅ — *Done in Phase 5 (`boardTurnLabel` → "Pre",
  `replayTurnLabel` → "Pre-game").* Still open: "G1: Play/Draw reads as a result"
  (REVIEW item 3).

---

## Enabling refactor (do this first — it is load-bearing)

You cannot comfortably build the above inside a 4,090-line file. REVIEW already
calls this *"the highest-value refactor in the repo."*

- ✅ **Extract `lib/replay/`** as pure, unit-testable functions — *done: 78
  declarations (types, zone/label helpers, object inspection, annotations, life,
  frame filtering, turn boundaries, narration, game summaries) moved verbatim out
  of the page into `web/src/lib/replay/index.ts`. The component dropped from
  ~4,090 → ~3,030 lines and now imports them. No behavior change; verified by
  typecheck + build + tests.*
- ✅ **Extract a `useReplayPlayer` hook** owning the transport state machine
  (selected step, play/pause, autoplay advance, re-clamp on shrink) — *done:
  `web/src/lib/replay/useReplayPlayer.ts`, now the single source for both
  `MatchReplayFrameBoard` and `MatchTimelineBoard`, which previously held
  duplicated copies. `speed` + `keyboard` are deliberately left for Phase 1 to
  keep Phase 0 a pure no-behavior-change refactor — the hook is structured so
  they drop in here.*
- ✅ **Test runner wired up** — *done: standardized on `bun test` (added `test`
  + `typecheck` scripts to `web/package.json`) rather than adding vitest, since
  the existing `rankProgress.test.ts` already uses `bun:test` and Bun is the
  package manager. New `web/test/replay.test.ts` covers zone classification,
  turn boundaries, meaningful-frame filtering, narration, win-reason formatting,
  and game-result inference. 21 tests pass.*
- 🔲 **Split `components/replay/`**: `Arena`, `Hud`, `Scrubber`, `MoveList`,
  `Board`, `ConnectionOverlay`. Mechanical, but it makes moves 1–5 each a
  contained PR. *(Not started — the React components still live in
  `MatchDetailPage.tsx`. Splitting `lib/replay/index.ts` into themed files
  — `labels`, `objects`, `frames`, `summary`, `narration` — is a trivial
  follow-up too.)*
- 🔲 **Smoothness prerequisite**: previews fetch per-card from Scryfall at render
  (`lib/scryfall.ts`), so the board pops in. The local-card-DB bulk import
  (REVIEW item) makes the arena render instantly and unblocks the card-motion
  idea.

---

## Sequencing

| Phase | Ships | Effort | Notes |
|---|---|---|---|
| 0 ✅ | Extract `lib/replay/` + `useReplayPlayer` + wire up `bun test` | M | Done — unblocks everything; no behavior change (typecheck + build + 21 tests green) |
| 1 ✅ | HUD strip + keyboard + speed | S | Done — verified live against match 675 (HUD, turn jumps, −7 delta flash, 2× speed) |
| 2 ✅ | Scrubber + life sparkline (retire turn pills) | M | Done — dual sparkline, colored ticks, click-seek; verified live (75% → step 67) |
| 3 ✅ | Mirrored arena layout | M | Done — arena (realigned to two-column in Phase 4) |
| 4 ✅ | Coalesced beats + humanized play-by-play rail | M | Done — two-column board+rail, avatars, clean narration; verified live |
| 5 🟡 | Key-moment pins + narration polish done; combat moments & card motion deferred | L | Pins + clean narration shipped; FLIP animation is future work |

Phases 1–3 get ~80% of the perceived "100×."

---

## Code reference index

| Concern | Location (`web/src/pages/MatchDetailPage.tsx` unless noted) |
|---|---|
| Main board component (owns index/playing) | `MatchReplayFrameBoard` ~2525 |
| Observed-plays fallback board (duplicate state machine) | `MatchTimelineBoard` ~3231 |
| Transport buttons (no scrubber/keyboard/speed) | ~2866–2942 |
| Hardcoded autoplay interval (`1200ms`) | ~2572 |
| Turn-pill row (replace with scrubber) | `MatchReplayTurnSelector` ~2470 |
| Clinical narration | `describeReplayChange` ~1255 |
| Frame filtering (no coalescing yet) | `filterMeaningfulReplayFrames` ~1016 |
| Life delta detection | `replayFrameHasLifeDelta` ~992 |
| Buried life/visibility sentence | `frameVisibilitySummary` ~2801 |
| Combat / spell-target arrows | `MatchReplayConnectionOverlay` ~1674 |
| Game-result inference | `summarizeReplayGame` / `terminalReplayFrameConfidence` ~1144 / ~1127 |
| Keyboard nav (tabs only, not board) | `handleTimelineGameTabKeyDown` ~3727 |
| Canvas layout (sidebar + board column) | `.match-replay-canvas` `styles.css` ~1948 |
| Data model | `web/src/lib/types.ts` ~60–120 |
| Per-card preview fetch (pops in at render) | `web/src/lib/scryfall.ts` |
