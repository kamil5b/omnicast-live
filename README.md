# OmniCast Live Engine

A real-time live-game engine built with **Go** and plain WebSockets.
No Node.js, no Socket.IO — a single self-contained binary that embeds all
static assets via `go:embed`.

## Features

- **Game Master** dashboard — manage players, assign roles, control buzzer,
  open/close voting, reveal votes, toggle modules, load templates
- **Player** view — join via QR code, buzz in, vote, see your secret role
- **Operator** view — override player images, show roles on the stream overlay
- **OBS Overlay** — animated portrait grid with live points and buzzer banner
- **Templates** — JSON files that pre-configure active modules (Trivia, Werewolf, Undercover…)
- **Image uploads** — players and operators can upload avatar photos (≤ 5 MB)
- **Rate limiting** — per-IP limits on general endpoints (200 req/min) and uploads (30 req/min)

## Architecture

| File | Responsibility |
|---|---|
| `main.go` | HTTP server, REST handlers, `go:embed`, rate limiting |
| `hub.go` | WebSocket hub — client registration, broadcast helpers, ping/pong |
| `game.go` | Game state (mutex-protected), all event dispatch and handler logic |
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
are embedded at compile time.

### Environment

| Variable | Default | Description |
|---|---|---|
| *(none)* | port 3000 | Port is hard-coded; change `appPort` in `main.go` |

## URLs

| URL | Audience |
|---|---|
| `/` | Players — join screen |
| `/gm` | Game Master |
| `/operator` | Stream operator |
| `/overlay` | OBS browser source |
| `/qr` | JSON with local network URL + QR data-URI |

## Templates

Templates live in `templates/*.json`.  The bundled defaults are copied to the
working directory on first run and can then be edited freely.

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
