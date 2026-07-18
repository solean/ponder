-- journal_mode, foreign_keys, and the other connection pragmas are set on the
-- DSN in db.Open so they apply to every pooled connection.

CREATE TABLE IF NOT EXISTS ingest_state (
  log_path TEXT PRIMARY KEY,
  byte_offset INTEGER NOT NULL DEFAULT 0,
  line_no INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS app_metadata (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS events_raw (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  log_path TEXT NOT NULL,
  line_no INTEGER NOT NULL,
  byte_offset INTEGER NOT NULL,
  kind TEXT NOT NULL,
  method_name TEXT,
  request_id TEXT,
  payload_json TEXT,
  raw_text TEXT,
  created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_events_raw_method ON events_raw(method_name);

CREATE TABLE IF NOT EXISTS event_runs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  event_name TEXT NOT NULL UNIQUE,
  event_type TEXT,
  entry_currency_type TEXT,
  entry_currency_paid INTEGER,
  status TEXT NOT NULL DEFAULT 'active',
  started_at TEXT,
  ended_at TEXT,
  wins INTEGER NOT NULL DEFAULT 0,
  losses INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS decks (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  arena_deck_id TEXT NOT NULL UNIQUE,
  event_name TEXT,
  name TEXT,
  format TEXT,
  source TEXT,
  last_updated TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_decks_event_name ON decks(event_name);

CREATE TABLE IF NOT EXISTS deck_cards (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  deck_id INTEGER NOT NULL,
  section TEXT NOT NULL,
  card_id INTEGER NOT NULL,
  quantity INTEGER NOT NULL,
  FOREIGN KEY(deck_id) REFERENCES decks(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_deck_cards_deck_id ON deck_cards(deck_id);

-- Immutable snapshots of a deck's contents. The decks/deck_cards tables keep
-- the latest Arena state for browsing, while matches link to the version that
-- was current when they were played.
CREATE TABLE IF NOT EXISTS deck_versions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  deck_id INTEGER NOT NULL,
  version_number INTEGER NOT NULL,
  cards_hash TEXT NOT NULL,
  source TEXT,
  effective_at TEXT,
  created_at TEXT NOT NULL,
  UNIQUE(deck_id, version_number),
  UNIQUE(deck_id, cards_hash),
  FOREIGN KEY(deck_id) REFERENCES decks(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_deck_versions_effective
  ON deck_versions(deck_id, effective_at, id);

CREATE TABLE IF NOT EXISTS deck_version_cards (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  deck_version_id INTEGER NOT NULL,
  section TEXT NOT NULL,
  card_id INTEGER NOT NULL,
  quantity INTEGER NOT NULL,
  UNIQUE(deck_version_id, section, card_id),
  FOREIGN KEY(deck_version_id) REFERENCES deck_versions(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS card_catalog (
  arena_id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_card_catalog_name ON card_catalog(name);

-- Card type lines (Scryfall `type_line`), resolved on demand and cached so the
-- live banner can compute land odds without re-fetching every poll.
CREATE TABLE IF NOT EXISTS card_types (
  arena_id INTEGER PRIMARY KEY,
  type_line TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

-- Friendly metadata for MTG sets, keyed by the lowercase set code embedded in
-- Arena event names (e.g. "tmt" in "QuickDraft_TMT_20260313"). Resolved on
-- demand from Scryfall and cached here so set names/symbols work offline.
-- Card color identity and mana value, resolved on demand from the local MTGA
-- raw card database (preferred) or Scryfall, and cached for offline matchup
-- classification. color_identity is a WUBRG-ordered subset string ("UB").
CREATE TABLE IF NOT EXISTS card_metadata (
  arena_id INTEGER PRIMARY KEY,
  color_identity TEXT NOT NULL DEFAULT '',
  mana_value REAL,
  updated_at TEXT NOT NULL
);

-- Manual archetype corrections for one match's opponent. Overrides win over
-- the derived classification wherever matchups are aggregated.
CREATE TABLE IF NOT EXISTS match_opponent_archetype_overrides (
  match_id INTEGER PRIMARY KEY,
  archetype TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY(match_id) REFERENCES matches(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS set_catalog (
  code TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  icon_svg_uri TEXT,
  released_at TEXT,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS matches (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  arena_match_id TEXT NOT NULL UNIQUE,
  event_name TEXT,
  format TEXT,
  player_seat_id INTEGER,
  opponent_name TEXT,
  opponent_user_id TEXT,
  started_at TEXT,
  ended_at TEXT,
  result TEXT,
  win_reason TEXT,
  turn_count INTEGER,
  seconds_count INTEGER,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_matches_event_name ON matches(event_name);
CREATE INDEX IF NOT EXISTS idx_matches_started_at ON matches(started_at);

CREATE TABLE IF NOT EXISTS match_decks (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  match_id INTEGER NOT NULL,
  deck_id INTEGER NOT NULL,
  deck_version_id INTEGER,
  snapshot_reason TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(match_id, deck_id),
  FOREIGN KEY(match_id) REFERENCES matches(id) ON DELETE CASCADE,
  FOREIGN KEY(deck_id) REFERENCES decks(id) ON DELETE CASCADE,
  FOREIGN KEY(deck_version_id) REFERENCES deck_versions(id) ON DELETE SET NULL
);

-- Replay-derived game analytics. Source/confidence fields make it explicit
-- which values came directly from GRE state and which are heuristics.
CREATE TABLE IF NOT EXISTS games (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  match_id INTEGER NOT NULL,
  game_number INTEGER NOT NULL,
  result TEXT NOT NULL DEFAULT 'unknown',
  win_reason TEXT,
  play_draw TEXT,
  started_at TEXT,
  ended_at TEXT,
  turn_count INTEGER,
  opening_life_total INTEGER,
  ending_life_total INTEGER,
  mulligan_count INTEGER,
  kept_hand_size INTEGER,
  result_source TEXT,
  result_confidence TEXT NOT NULL DEFAULT 'unknown',
  play_draw_source TEXT,
  play_draw_confidence TEXT NOT NULL DEFAULT 'unknown',
  opening_hand_source TEXT,
  opening_hand_confidence TEXT NOT NULL DEFAULT 'unknown',
  derived_at TEXT NOT NULL,
  UNIQUE(match_id, game_number),
  FOREIGN KEY(match_id) REFERENCES matches(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS game_opening_hands (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  game_id INTEGER NOT NULL,
  attempt_number INTEGER NOT NULL,
  decision TEXT NOT NULL DEFAULT 'unknown',
  offered_hand_size INTEGER NOT NULL,
  kept_hand_size INTEGER,
  observed_at TEXT,
  source TEXT NOT NULL,
  confidence TEXT NOT NULL DEFAULT 'derived',
  UNIQUE(game_id, attempt_number),
  FOREIGN KEY(game_id) REFERENCES games(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS game_opening_hand_cards (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  opening_hand_id INTEGER NOT NULL,
  card_id INTEGER NOT NULL,
  quantity INTEGER NOT NULL,
  kept INTEGER NOT NULL DEFAULT 0,
  UNIQUE(opening_hand_id, card_id),
  FOREIGN KEY(opening_hand_id) REFERENCES game_opening_hands(id) ON DELETE CASCADE
);

-- Per-game, per-card facts derived from replay hand snapshots and card plays.
-- One row per (game, card) the player kept, mulliganed, drew, or played.
-- Copies are counts within that game; deck analytics aggregate these rows
-- through games (results) and match_decks (deck/version scope).
CREATE TABLE IF NOT EXISTS game_card_stats (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  game_id INTEGER NOT NULL,
  match_id INTEGER NOT NULL,
  card_id INTEGER NOT NULL,
  opening_kept_copies INTEGER NOT NULL DEFAULT 0,
  mulligan_copies INTEGER NOT NULL DEFAULT 0,
  drawn_copies INTEGER NOT NULL DEFAULT 0,
  played_copies INTEGER NOT NULL DEFAULT 0,
  end_in_hand_copies INTEGER NOT NULL DEFAULT 0,
  first_seen_turn INTEGER,
  first_played_turn INTEGER,
  source TEXT,
  confidence TEXT NOT NULL DEFAULT 'derived',
  UNIQUE(game_id, card_id),
  FOREIGN KEY(game_id) REFERENCES games(id) ON DELETE CASCADE,
  FOREIGN KEY(match_id) REFERENCES matches(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_game_card_stats_match ON game_card_stats(match_id, card_id);

CREATE TABLE IF NOT EXISTS match_analytics_coverage (
  match_id INTEGER PRIMARY KEY,
  replay_available INTEGER NOT NULL DEFAULT 0,
  replay_frame_count INTEGER NOT NULL DEFAULT 0,
  game_count INTEGER NOT NULL DEFAULT 0,
  games_with_result INTEGER NOT NULL DEFAULT 0,
  games_with_opening_hand INTEGER NOT NULL DEFAULT 0,
  games_with_play_draw INTEGER NOT NULL DEFAULT 0,
  deck_snapshot_available INTEGER NOT NULL DEFAULT 0,
  deck_version_available INTEGER NOT NULL DEFAULT 0,
  overall_confidence TEXT NOT NULL DEFAULT 'unknown',
  derived_at TEXT NOT NULL,
  FOREIGN KEY(match_id) REFERENCES matches(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS match_opponent_card_instances (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  match_id INTEGER NOT NULL,
  game_number INTEGER NOT NULL DEFAULT 1,
  instance_id INTEGER NOT NULL,
  card_id INTEGER NOT NULL,
  source TEXT,
  first_seen_at TEXT,
  created_at TEXT NOT NULL,
  UNIQUE(match_id, game_number, instance_id),
  FOREIGN KEY(match_id) REFERENCES matches(id) ON DELETE CASCADE
);

-- match_id lookups are served by the UNIQUE(match_id, game_number, instance_id)
-- autoindex; no separate match_id index needed.
CREATE INDEX IF NOT EXISTS idx_match_opponent_cards_card_id ON match_opponent_card_instances(card_id);

CREATE TABLE IF NOT EXISTS match_card_plays (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  match_id INTEGER NOT NULL,
  game_number INTEGER NOT NULL DEFAULT 1,
  instance_id INTEGER NOT NULL,
  card_id INTEGER NOT NULL,
  owner_seat_id INTEGER,
  first_public_zone TEXT,
  turn_number INTEGER,
  phase TEXT,
  source TEXT,
  played_at TEXT,
  created_at TEXT NOT NULL,
  UNIQUE(match_id, game_number, instance_id),
  FOREIGN KEY(match_id) REFERENCES matches(id) ON DELETE CASCADE
);

-- match_id lookups are served by the UNIQUE(match_id, game_number, instance_id)
-- autoindex and the turn_order index prefix; no separate match_id index needed.
CREATE INDEX IF NOT EXISTS idx_match_card_plays_card_id ON match_card_plays(card_id);
CREATE INDEX IF NOT EXISTS idx_match_card_plays_turn_order ON match_card_plays(match_id, turn_number, played_at, id);

CREATE TABLE IF NOT EXISTS match_replay_frames (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  match_id INTEGER NOT NULL,
  game_number INTEGER NOT NULL DEFAULT 1,
  game_state_id INTEGER,
  prev_game_state_id INTEGER,
  game_state_type TEXT,
  game_stage TEXT,
  turn_number INTEGER,
  phase TEXT,
  player_life_totals_json TEXT,
  winning_player_side TEXT,
  win_reason TEXT,
  source TEXT,
  recorded_at TEXT,
  actions_json TEXT,
  annotations_json TEXT,
  created_at TEXT NOT NULL,
  UNIQUE(match_id, game_number, game_state_id),
  FOREIGN KEY(match_id) REFERENCES matches(id) ON DELETE CASCADE
);

-- (match_id, game_number, game_state_id) lookups are served by the UNIQUE
-- autoindex on those columns.
CREATE INDEX IF NOT EXISTS idx_match_replay_frames_turn_order
  ON match_replay_frames(match_id, game_number, turn_number, game_state_id, id);

CREATE TABLE IF NOT EXISTS match_replay_frame_objects (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  frame_id INTEGER NOT NULL,
  instance_id INTEGER NOT NULL,
  card_id INTEGER NOT NULL,
  owner_seat_id INTEGER,
  controller_seat_id INTEGER,
  zone_id INTEGER,
  zone_type TEXT NOT NULL,
  zone_position INTEGER,
  visibility TEXT,
  power INTEGER,
  toughness INTEGER,
  is_tapped INTEGER NOT NULL DEFAULT 0,
  has_summoning_sickness INTEGER NOT NULL DEFAULT 0,
  attack_state TEXT,
  attack_target_id INTEGER,
  block_state TEXT,
  block_attacker_ids_json TEXT,
  counter_summary_json TEXT,
  details_json TEXT,
  is_token INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  UNIQUE(frame_id, instance_id),
  FOREIGN KEY(frame_id) REFERENCES match_replay_frames(id) ON DELETE CASCADE
);

-- frame_id lookups are served by the UNIQUE(frame_id, instance_id) autoindex.
CREATE INDEX IF NOT EXISTS idx_match_replay_frame_objects_card_id
  ON match_replay_frame_objects(card_id);
CREATE INDEX IF NOT EXISTS idx_match_replay_frame_objects_zone
  ON match_replay_frame_objects(frame_id, zone_type, zone_position, instance_id);

CREATE TABLE IF NOT EXISTS match_replay_archives (
  match_id INTEGER PRIMARY KEY,
  schema_version INTEGER NOT NULL DEFAULT 1,
  frame_count INTEGER NOT NULL DEFAULT 0,
  object_count INTEGER NOT NULL DEFAULT 0,
  payload_zstd BLOB NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY(match_id) REFERENCES matches(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS match_rank_snapshots (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  match_id INTEGER NOT NULL UNIQUE,
  prev_snapshot_id INTEGER,
  observed_at TEXT,
  payload_json TEXT NOT NULL,
  constructed_season_ordinal INTEGER,
  constructed_rank_class TEXT,
  constructed_level INTEGER,
  constructed_step INTEGER,
  constructed_matches_won INTEGER,
  constructed_matches_lost INTEGER,
  limited_season_ordinal INTEGER,
  limited_rank_class TEXT,
  limited_level INTEGER,
  limited_step INTEGER,
  limited_matches_won INTEGER,
  limited_matches_lost INTEGER,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY(match_id) REFERENCES matches(id) ON DELETE CASCADE,
  FOREIGN KEY(prev_snapshot_id) REFERENCES match_rank_snapshots(id)
);

CREATE INDEX IF NOT EXISTS idx_match_rank_snapshots_observed_at ON match_rank_snapshots(observed_at);

CREATE TABLE IF NOT EXISTS economy_snapshots (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  log_path TEXT NOT NULL,
  line_no INTEGER NOT NULL,
  observed_at TEXT,
  sequence_id INTEGER NOT NULL DEFAULT 0,
  gold INTEGER NOT NULL DEFAULT 0,
  gems INTEGER NOT NULL DEFAULT 0,
  vault_progress INTEGER NOT NULL DEFAULT 0,
  wildcard_track_position INTEGER NOT NULL DEFAULT 0,
  wildcard_commons INTEGER NOT NULL DEFAULT 0,
  wildcard_uncommons INTEGER NOT NULL DEFAULT 0,
  wildcard_rares INTEGER NOT NULL DEFAULT 0,
  wildcard_mythics INTEGER NOT NULL DEFAULT 0,
  custom_tokens_json TEXT NOT NULL DEFAULT '{}',
  boosters_json TEXT NOT NULL DEFAULT '[]',
  vouchers_json TEXT NOT NULL DEFAULT '{}',
  changes_json TEXT NOT NULL DEFAULT '[]',
  created_at TEXT NOT NULL,
  UNIQUE(log_path, line_no)
);

CREATE INDEX IF NOT EXISTS idx_economy_snapshots_observed_at ON economy_snapshots(observed_at);

CREATE TABLE IF NOT EXISTS draft_sessions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  event_name TEXT,
  draft_id TEXT,
  is_bot_draft INTEGER NOT NULL,
  started_at TEXT,
  completed_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(draft_id, is_bot_draft)
);

CREATE INDEX IF NOT EXISTS idx_draft_sessions_event_name ON draft_sessions(event_name);

CREATE TABLE IF NOT EXISTS draft_picks (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  draft_session_id INTEGER NOT NULL,
  pack_number INTEGER NOT NULL,
  pick_number INTEGER NOT NULL,
  picked_card_ids TEXT NOT NULL,
  pack_card_ids TEXT,
  pick_ts TEXT,
  created_at TEXT NOT NULL,
  UNIQUE(draft_session_id, pack_number, pick_number),
  FOREIGN KEY(draft_session_id) REFERENCES draft_sessions(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_draft_picks_session ON draft_picks(draft_session_id);

CREATE TABLE IF NOT EXISTS deck_ai_primers (
  deck_id INTEGER PRIMARY KEY,
  cards_hash TEXT NOT NULL,
  model TEXT NOT NULL DEFAULT '',
  content TEXT NOT NULL,
  created_at TEXT NOT NULL,
  FOREIGN KEY(deck_id) REFERENCES decks(id) ON DELETE CASCADE
);
