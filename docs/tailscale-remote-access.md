# Remote access over Tailscale

Burrow's dispatch router (`src-tauri/src/dispatch.rs`) can be reached over
HTTP/WebSocket, not just Tauri IPC. The transport (`src-tauri/src/http_server/`)
is **off by default** and binds `127.0.0.1` only — it never listens on
`0.0.0.0`. To reach it from another device, publish the loopback port through
Tailscale.

## 1. Enable the HTTP transport

The transport starts at app launch, only if the `http_enabled` pref file
says "1". There's no Settings UI toggle yet (tracked in
`docs/plans/checklist.md`) — for now, write the file by hand and restart
Burrow:

```bash
echo -n "1" > "$HOME/Library/Application Support/com.agenticide.app/http_enabled"
```

(A future Settings toggle will call the existing `set_http_enabled` Tauri
command, which just writes this same file — starting/stopping the server
live, without a restart, is also a follow-up.)

Optional: pick a non-default port (default `8420`):

```bash
echo -n "8765" > "$HOME/Library/Application Support/com.agenticide.app/http_port"
```

Restart Burrow. On startup it binds `127.0.0.1:<port>` and writes a bearer
token to `http.token` in the same app-data directory (chmod `0600`).

## 2. Install and log in to Tailscale

```bash
brew install --cask tailscale
tailscale up
```

## 3. Publish the loopback port with Tailscale Serve

Burrow only ever binds loopback — it deliberately does not bind
`0.0.0.0` or a tailnet interface itself. Use `tailscale serve` to proxy the
local port onto your tailnet instead of relaxing Burrow's own bind:

```bash
tailscale serve --bg 8420
```

This makes the server reachable at `https://<your-machine>.<tailnet>.ts.net`
from any device on your tailnet, with Tailscale handling TLS. (Use
`tailscale funnel` instead of `serve` only if you intentionally want the
port reachable from the public internet — not recommended for this
transport.)

## 4. Auth

Every `/ws` connection (and any future authenticated REST route) requires
the bearer token, either as `Authorization: Bearer <token>` or `?token=...`.
`/healthz` is the only unauthenticated route.

```bash
curl -H "Authorization: Bearer $(cat ~/Library/Application\ Support/com.agenticide.app/http.token)" http://127.0.0.1:8420/healthz
```

## 5. WebSocket protocol

Connect to `/ws` with the bearer token, then send JSON frames:

```json
{"id": 1, "command": "list_workspaces", "args": {}}
```

You'll get back `{"id": 1, "result": ...}` or `{"id": 1, "error": "..."}`.

To receive streaming events (PTY output, agent status, etc.), subscribe by
event name:

```json
{"subscribe": "pty-data-42"}
```

Matching events arrive as `{"event": "pty-data-42", "payload": ...}`. There
is currently no replay buffer — you only receive events emitted after you
subscribe.

## 6. Mobile UI

A touch-first web UI (`src/mobile/`) talks to the same `/ws` transport above
— Connect / Sessions / Terminal screens, xterm.js rendering, no separate
pairing flow (just the host + bearer token from step 4).

```bash
pnpm build:mobile
```

This produces `dist-mobile/mobile.html` + assets. Enable the HTTP toggle in
Settings (desktop app → Settings → the HTTP/WS transport section), restart
Burrow, then from your phone (on the same tailnet) visit:

```
http://<tailscale-host>:<port>/
```

`/` is served straight from `dist-mobile/`, unauthenticated (a plain browser
GET can't send an `Authorization` header — same reasoning as `/healthz`).
Paste that same base URL and the bearer token (Settings shows the token
*file path*; `cat` it, e.g. `cat ~/Library/Application\ Support/com.agenticide.app/http.token`)
into the Connect screen. Sessions lists every open workspace/tab with a live
status dot; tapping one opens a real terminal.
