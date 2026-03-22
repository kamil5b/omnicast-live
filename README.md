# OmniCast Live Engine

A real-time live-game engine built with **Go** and plain WebSockets.
No Node.js, no Socket.IO — a single self-contained binary that embeds all
static assets via `go:embed`.

## Features

- **Game Master** dashboard — manage players, assign roles (manual, checklist, or randomised), control buzzer, open/close voting, reveal votes, toggle modules, load/save templates, send direct messages to players, reset scores
- **Player** view — join via QR code, buzz in, vote, see your secret role (when revealed)
- **Operator** view — override player images via URL or file upload, toggle show-all-roles on the overlay, reset scores
- **OBS Overlay** — animated portrait grid with live points, player statuses and buzzer banner
- **Templates** — JSON files that pre-configure active modules (Trivia, Werewolf, Undercover…); can be saved from the GM dashboard
- **Image uploads** — players and operators can upload avatar photos (≤ 5 MB, images only)
- **Rate limiting** — per-IP limits on general endpoints (200 req/min) and uploads (30 req/min)
- **Role system** — define named roles with optional max counts and colours; assign manually, via checklist, or randomise across the player pool; reveal roles individually or all at once to the overlay

## Architecture

| File | Responsibility |
|---|---|
| `main.go` | HTTP server, route registration, `go:embed`, rate-limit wiring |
| `hub.go` | WebSocket hub — client registration/unregistration, deliver/broadcast helpers, ping/pong |
| `game.go` | Core types (`GameState`, `player`, `Modules`, `RoleDefinition`), inbound message helpers |
| `game_state.go` | State snapshot builders — `publicState`, `gmState`, `privateState` |
| `game_broadcast.go` | Fan-out helpers — `broadcastPublicState`, `setShowAllRoles`, `resetAllScores` |
| `game_dispatch.go` | WebSocket message router — maps `type` strings to handlers |
| `game_handlers_gm.go` | All GM-originated event handlers |
| `game_handlers_player.go` | Player buzz and vote handlers |
| `game_handlers_operator.go` | Operator image-override handlers |
| `game_handlers_join.go` | Join handlers for every client role |
| `game_helpers.go` | `sanitize`, template file types, `mergeModules` |
| `handlers.go` | HTTP REST handlers (`/qr`, `/upload`, `/api/templates`), utility helpers |
| `ratelimit.go` | Per-IP rate limiter, `writeJSON`/`writeError` helpers |
| `public/js/ws-client.js` | Browser WebSocket wrapper with Socket.IO-compatible API |

## Getting started

### Prerequisites

- Go 1.22+

### Run in development

```bash
go run .
```

### Build a production binary

```bash
go build -o omnicast-live .
./omnicast-live
```

The binary is fully self-contained — `public/` and the default `templates/`
are embedded at compile time. On first run the embedded templates are copied
to a `templates/` directory next to the binary so they can be edited freely.

### Environment

| Variable | Default | Description |
|---|---|---|
| *(none)* | port `3000` | Port is hard-coded; change `appPort` in `main.go` |

## URLs

| URL | Audience |
|---|---|
| `/` | Players — join screen |
| `/player.html` | Player game view (redirected to after joining) |
| `/gm` | Game Master dashboard |
| `/operator` | Stream operator |
| `/overlay` | OBS browser source |
| `/qr` | JSON `{ url, qr }` — local network URL + base64 PNG QR code |
| `/upload` | `POST multipart/form-data` image upload → `{ filename, url }` |
| `/api/templates` | `GET` list / `POST` save templates |
| `/uploads/<file>` | Uploaded avatar images |

## WebSocket events

### Inbound (client → server)

| Type | Role | Description |
|---|---|---|
| `player:join` | — | Register as a player with a name and optional image |
| `gm:join` | — | Register as Game Master |
| `operator:join` | — | Register as stream operator |
| `overlay:join` | — | Register as OBS overlay |
| `player:buzz` | player | Trigger the buzzer |
| `player:vote` | player | Cast a vote for a target player |
| `gm:setPoints` | gm | Adjust a player's points by a delta |
| `gm:setStatus` | gm | Set a player status (`ALIVE` / `DEAD` / `MUTE`) |
| `gm:assignRole` | gm | Directly assign a role string to a player |
| `gm:assignRoleChecklist` | gm | Assign/unassign a defined role via checklist (enforces max count) |
| `gm:setRoleDefinitions` | gm | Replace the ordered role definition list |
| `gm:randomizeRoles` | gm | Randomly distribute defined roles to unassigned players |
| `gm:resetRoles` | gm | Clear all role assignments and per-player reveal flags |
| `gm:revealRoleForPlayer` | gm | Show/hide a single player's role to that player only |
| `gm:showAllRoles` | gm | Bulk-reveal (or hide) all roles on the overlay |
| `gm:messagePlayer` | gm | Send a direct text message to a specific player |
| `gm:enableBuzzers` | gm | Unlock buzzer and clear winner |
| `gm:resetBuzzer` | gm | Reset buzzer state (same as enable) |
| `gm:disableBuzzerForPlayer` | gm | Enable or disable the buzzer for one player |
| `gm:openVoting` | gm | Open a voting round (clears previous votes) |
| `gm:closeVoting` | gm | Close the current voting round |
| `gm:revealVotes` | gm | Tally and broadcast vote results |
| `gm:hideVotes` | gm | Hide revealed vote results |
| `gm:removePlayer` | gm | Remove a player from the game |
| `gm:resetScores` | gm | Zero all player point totals |
| `gm:loadTemplate` | gm | Apply a template's module configuration |
| `gm:setModules` | gm | Toggle individual modules on or off |
| `operator:overrideImage` | operator | Override a player's avatar with a URL |
| `operator:overrideImageUpload` | operator | Override a player's avatar with an uploaded file |
| `operator:showAllRoles` | operator | Same as `gm:showAllRoles` but from operator |
| `operator:resetScores` | operator | Same as `gm:resetScores` but from operator |

### Outbound (server → client)

| Type | Audience | Description |
|---|---|---|
| `gameState` | overlay, operator | Public game state snapshot |
| `gameState` | gm | Full game state (includes roles and votes maps) |
| `playerState` | player | Private player state (own role, voting options, buzzer) |
| `buzzer:enabled` | all | Buzzer was unlocked via `gm:enableBuzzers` |
| `buzzer:reset` | all | Buzzer was reset via `gm:resetBuzzer` |
| `gm:message` | player | Direct message text from the GM |

## Modules

| Module | Controls |
|---|---|
| `buzzer` | Buzz-in button and winner banner |
| `points` | Point scoring and leaderboard |
| `roles` | Role assignment, reveal and overlay display |
| `voting` | Voting round and result reveal |
| `status` | Player status badges (ALIVE / DEAD / MUTE) |

## Templates

Templates live in `templates/*.json`. The bundled defaults (`trivia`, `werewolf`, `undercover`) are seeded on first run and can then be edited freely. New templates can be saved from the GM dashboard via `POST /api/templates`.

```json
{
  "name": "Werewolf",
  "description": "Social deduction — roles and voting only.",
  "modules": {
    "buzzer": false,
    "points": false,
    "roles": true,
    "voting": true,
    "status": true
  }
}
```

## Release builds

Pre-built binaries for all major platforms are produced automatically by the
GitHub Actions release workflow (`.github/workflows/release.yml`) when a
GitHub Release is published.

| Platform | Architecture | Binary |
|---|---|---|
| Windows | amd64 | `omnicast-live-windows-amd64.exe` |
| Windows | arm64 | `omnicast-live-windows-arm64.exe` |
| Linux | amd64 | `omnicast-live-linux-amd64` |
| Linux | arm64 | `omnicast-live-linux-arm64` |
| macOS | amd64 | `omnicast-live-darwin-amd64` |
| macOS | arm64 (Apple Silicon) | `omnicast-live-darwin-arm64` |

All binaries are built with `CGO_ENABLED=0` and `-trimpath -ldflags "-s -w"` for a minimal, fully static output.