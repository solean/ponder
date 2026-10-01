# Match detail component boundaries

The match detail refactor preserves the existing queries, rendered controls, and
mount/reset behavior. The boundaries below were mapped before moving the components:

| Owner | Responsibility and state |
| --- | --- |
| `MatchDetailPage` | Route ID, detail/timeline queries, active section, shared game selection and accessible tab/panel IDs. Game selection stays shared between Replay and AI Game Review. |
| Replay view data hook | Replay groups, timeline fallback groups, game summaries, unique preview cards and eager preview queries. Receives frames from the existing `useMatchReplay` query hook, which remains in the page. |
| Opponent cards | Card categories and mana/rarity metadata presentation. Its query hook remains called unconditionally by the page, preserving eager loading. |
| Replay frame board | Playback, seeking, selected relationships, inspected zone, collapsed move list, and DOM card registry. Uses the existing `useReplayPlayer`, `useReplayKeyboard`, and persisted preference hooks. |
| Timeline fallback board | Playback and zone inspection for first public sightings when replay frames are unavailable. Uses the existing playback and keyboard hooks. |
| Replay battlefield | Card stacks, hands, stack zones, battlefield rows, and their board layout. Receives frame objects and callbacks from its board owner. |
| Replay connections | DOM measurements and connection overlay lifecycle. Receives the board's element registry and selected relationships. |
| Replay zones | Zone counts, inspection dialog, focus trap, and focus restoration. The board owns which zone is open. |
| Replay controls | Scrubber pointer/hover state, HUD, speed selector, and move-list scroll tracking. The board owns playback position and speed. |
| Card previews | Hover/focus state, floating portal placement, card rendering, and lazy name previews. Query keys remain shared with eager board/opponent previews. |
| Analytics | Opening-hand previews, turn-shape charts, and analytics rendering. |
| Sideboard changes | Submitted deck differences and card preview links. |
| Review section | Shared game tabs and review loading/error presentation. The existing `GameReviewPanel` continues to own generation, cancellation, streaming text, and review queries. |

Boards and the existing review panel retain their game-number React keys, so
changing games resets local playback/dialog/generation state as before. Switching
sections retains the page's selected game while unmounting the active board or
review panel. CSS classes, accessible labels, storage keys, and query keys remain
the same.
