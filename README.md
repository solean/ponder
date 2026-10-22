# Ponder

Private, local-first match tracking and analytics for **MTG Arena** on macOS.

Ponder reads the log file Arena already writes on your Mac, turns it into a
local database of your matches, decks, drafts, and economy, and gives you a
native app to explore it, plus an in-game overlay that tracks your library
while you play. Nothing is uploaded and you don't need an account.

## Features

- **Overview**: rank progress for Constructed and Limited, recent form,
  streaks, an activity calendar, and your most-played decks. **Season
  details** breaks down win rates by tier and season.
- **Matches**: full match history with game-by-game results, the opponent's
  revealed cards, a turn-by-turn timeline, and a **visual replay** of every
  game you play while Ponder is tracking.
- **Decks**: per-deck records, play/draw splits, mulligan and opening-hand
  stats, card performance, matchups by opponent colors and archetype, deck
  version history, and one-click export back to Arena.
- **Drafts**: every pick, the full pack contents, and a pack-by-pack replay,
  annotated with **17Lands** card ratings (GIH WR, ATA, and more). You also
  get draft performance and set matchups across all your Limited play.
- **Economy**: gold, gems, wildcards, vault progress, boosters, and where
  each change came from.
- **In-game overlay**: shows your remaining deck and the opponent's revealed
  cards at the top edges of Arena's window, with card previews on hover.
- **AI deck primers and game reviews** (optional): generated through the
  Claude Code or Codex CLI using your existing subscription.
- **Menu bar status**: start or stop tracking and see the current match at a
  glance, even with the window closed.

## Install

**Requirements:** macOS 12 Monterey or later, on Apple Silicon or Intel, with
MTG Arena installed.

1. Download the latest `Ponder.dmg` from
   [GitHub Releases](https://github.com/solean/ponder/releases).
2. Open it and drag **Ponder** into your **Applications** folder.
3. Launch Ponder from Applications or Spotlight.

> Signed and notarized releases are still being set up. Until they're
> published, [build the app from source](#building-the-app). If macOS blocks a
> build you got from someone else, open **System Settings → Privacy &
> Security** and click **Open Anyway**.

Ponder checks GitHub for new versions. Use **Settings → Application → Check
for Updates**, or turn on automatic checks there. To install an update,
replace the app in Applications with the new download. Your data stays where
it is.

## Getting started

### 1. Turn on Detailed Logs in Arena

Ponder needs Arena's detailed logs to see match, deck, and draft data. In
Arena, open **Options → Account**, check **Detailed Logs (Plugin Support)**,
then restart Arena. Wizards'
[log file instructions](https://mtgarena-support.wizards.com/hc/en-us/articles/360000726823-Creating-Log-Files-on-PC-Mac-Steam)
have more detail.

### 2. Open Ponder and play

Tracking starts automatically. On launch, Ponder imports everything still in
Arena's current and previous logs. After that it records new activity as it
happens. Arena only keeps its last two sessions of logs, so leave Ponder
running while you play. Matches played with Ponder closed can't be recovered
once Arena overwrites those logs.

Ponder reads logs from `~/Library/Logs/Wizards Of The Coast/MTGA/`. You can
point it at a different file under **Settings → Tracking → Advanced
tracking**.

### 3. Leave it running

- Closing the window hides Ponder but keeps tracking. Reopen it from the Dock
  or the menu bar, and quit with **⌘Q**.
- The **menu bar icon** has a Live Tracking switch and shows the opponent,
  game, turn, and deck during a match. Filled orbs mean tracking is on,
  hollow orbs mean it's off, and an exclamation mark means something needs
  attention.
- Turn on **Settings → Application → Launch Ponder at login** to have it
  ready whenever you start your Mac.

## The in-game overlay

During a live match, Ponder shows two panels flush with the top edges of
Arena's window. Your side lists the cards left in your library, and the
opponent's side lists the cards they've revealed. Both are sorted by mana
value, with lands last. Hover any row to see the card.

The overlay only appears while Arena is the frontmost app and a match is in
progress. It never captures clicks or scrolling, so everything passes through
to Arena.

Press **⌘⇧O** to hide or show it, even while Arena has focus, or use
**Overlay → Hide Game Overlay** in Ponder's app menu. More options are
under **Settings → Overlay**.

## AI primers and game reviews

Ponder can write a strategy primer for any deck and review individual games
from their replays. It does this by running an AI command-line tool you've
already installed and signed into, so it uses your existing subscription.
Ponder never sees or stores your credentials.

- **Claude**: install [Claude Code](https://claude.com/claude-code) and sign
  in with your Claude account.
- **OpenAI**: install the [Codex CLI](https://github.com/openai/codex), run
  `codex login`, and choose **Sign in with ChatGPT**.

Then pick the provider and model under **Settings → AI**. When you generate a
primer or review, the relevant deck list and game data are sent to that
provider.

## 17Lands draft ratings

Draft reports show 17Lands card ratings for Premier, Traditional, Quick, and
Pick Two drafts. Under **Settings → Draft Ratings** you can limit ratings to
17Lands' top, middle, or bottom player group, or pin a single event type for
every draft. Ratings are fetched at most once a day per set and format and
then cached, so they still show when you're offline.

17Lands provides this data for use on 17lands.com. Ponder's integration is
intended for personal use.

## Privacy

Your match history, decks, drafts, and settings live only on your Mac. Ponder
has no accounts, sync, or telemetry. It makes these outbound requests:

| Service | What for |
| --- | --- |
| [Scryfall](https://scryfall.com) | Card names, images, and set symbols |
| [17Lands](https://www.17lands.com) | Draft card ratings |
| GitHub | Checking for new Ponder releases |
| Claude Code / Codex CLI | AI primers and reviews, only when you ask for one |

## Your data

Everything is stored in `~/Library/Application Support/ponder/`:

- `ponder.db` is the SQLite database with all your tracked history.
- `config.json` holds your settings.

**Settings → Data** shows both paths and can reveal them in Finder. To back
up, quit Ponder and copy that folder.

### Uninstalling

1. Quit Ponder and drag it from Applications to the Trash.
2. To remove your data too, delete `~/Library/Application Support/ponder/`.
3. If you turned on launch at login, delete
   `~/Library/LaunchAgents/dev.ixianlabs.ponder.plist`.

---

## Development

Ponder is a [Wails v3](https://v3.wails.io) desktop app. The Go backend
parses Arena logs into SQLite and serves a JSON API, and the React frontend
(Vite, TypeScript, Tailwind) is embedded in the app.

### Prerequisites

- Go 1.25+
- [Bun](https://bun.sh) 1.3+
- Xcode Command Line Tools (`xcode-select --install`)
- The Wails CLI:

  ```bash
  go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.5
  ```

Install frontend dependencies once:

```bash
cd web && bun install
```

### Run the desktop app in development

```bash
./scripts/start-desktop.sh
```

This runs `wails3 dev` against `data/ponder.db` in the repo instead of your
real Application Support database. Edits in `web/src` hot-reload in place, and
Go changes rebuild and relaunch the app. Running `wails3 dev` without the
script uses the normal Application Support database.

Only run one process that ingests into a database at a time. Stop
`start-backend*.sh` before starting the desktop app, and vice versa.

### Run in a browser instead

```bash
./scripts/start-backend-dev.sh   # API on 127.0.0.1:8080, hot reload via air
./scripts/start-web.sh           # Vite on 127.0.0.1:5173, proxies /api to 8080
```

`start-backend-dev.sh` needs [air](https://github.com/air-verse/air)
(`go install github.com/air-verse/air@latest`).
`start-backend.sh` runs the same server without hot reload.

### Building the app

```bash
wails3 package                          # bin/Ponder.app for this Mac's architecture
wails3 task darwin:package:universal    # bin/Ponder.app for Apple Silicon + Intel
```

Builds are ad-hoc signed. Product metadata (name, bundle ID, version) lives in
`build/config.yml`, and the in-app version comes from `internal/version`.
Override it at build time with
`-ldflags "-X github.com/solean/ponder/internal/version.Version=x.y.z"`.

### Command-line tool

The `ponder` CLI runs the same ingestion and API without the desktop shell.
It's useful for debugging the parser against real logs:

```bash
go run ./cmd/ponder parse -db data/ponder.db               # import Player-prev.log + Player.log
go run ./cmd/ponder parse -db data/ponder.db -resume=false # re-import from scratch
go run ./cmd/ponder tail  -db data/ponder.db -interval=2s  # follow Player.log live
go run ./cmd/ponder serve -db data/ponder.db               # API (and web/dist if built) on :8080
go run ./cmd/ponder compact -db data/ponder.db             # compress stored replays
```

Pass `-log /path/to/Player.log` to read a specific file. The `scripts/start-*.sh`
wrappers forward extra arguments to these commands.

The API binds to `127.0.0.1` and rejects cross-origin browser requests. It has
no authentication, so think twice before passing a non-loopback `-addr`. In
the packaged app the API runs inside the Wails asset server and opens no port
at all.

### Tests

```bash
go test ./...
cd web && bun test && bun run typecheck
```

### Project layout

| Path | Contents |
| --- | --- |
| `main.go`, `app.go`, `menu_bar*`, `overlay_native*` | Desktop shell: windows, menu bar, native overlay |
| `cmd/ponder` | CLI entry point |
| `internal/ingest` | Arena log parser |
| `internal/db` | SQLite schema, migrations, and queries |
| `internal/api` | HTTP API, plus Scryfall, 17Lands, and update-check clients |
| `internal/ai` | Claude Code and Codex CLI integration |
| `internal/appstate` | Runtime config, live tracking, launch at login |
| `web` | React frontend |
| `build` | Wails build config, app icon, platform packaging |

Windows and Linux build targets come from the Wails template, but Ponder is
developed and tested on macOS only.

## Disclaimer

Ponder is unofficial Fan Content permitted under the Wizards of the Coast Fan
Content Policy. It is not approved or endorsed by Wizards. Portions of the
materials used are property of Wizards of the Coast. ©Wizards of the Coast
LLC. Card data and images are provided by Scryfall, and draft ratings by
17Lands.
