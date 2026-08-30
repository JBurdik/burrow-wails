# Burrow Remote (mobile MVP)

> **Stale — describes the old Rust `burrow-web` binary and its pairing-code
> flow.** On the Go/Wails backend the mobile client is served by the in-app
> HTTP server instead (bearer token, no pairing code) and the bundle is
> embedded in the app binary. See `docs/tailscale-remote-access.md`.

Burrow Remote is a small mobile-first web client served by a separate `burrow-web`
process. The desktop app and `burrow-daemon` remain the owners of PTYs. The web
gateway binds to loopback by default and should be exposed only through a private
Tailscale tailnet.

## Run locally

Keep the Burrow desktop app running so its PTY daemon and workspace database are
available, then run:

```sh
pnpm build:mobile
cargo run --manifest-path src-tauri/Cargo.toml --bin burrow-web -- \
  --assets-dir dist-mobile
```

The process prints its local URL and a one-time six-digit pairing code. Open the
URL, enter the code, and the browser stores the resulting device token locally.
Five invalid pairing attempts lock pairing until `burrow-web` is restarted.

For development the same steps are available as:

```sh
pnpm remote:dev
```

## Tailscale

Do not bind `burrow-web` to a LAN or public address. Keep its default
`127.0.0.1:9867` listener and publish that listener with Tailscale Serve. A typical
recent Tailscale CLI setup is:

```sh
tailscale serve --bg http://127.0.0.1:9867
tailscale serve status
```

Use Tailscale ACLs to restrict the device/user tags that can reach the Mac. Confirm
the exact `tailscale serve` syntax with `tailscale serve --help` for the installed
version.

## MVP scope

- list workspaces and live PTY sessions;
- poll terminal output snapshots;
- send text input and Ctrl-C;
- one-time pairing plus a persistent 256-bit bearer token;
- same-origin mobile assets with restrictive browser security headers.

The API intentionally does not expose arbitrary filesystem, git, shell-spawn, PTY
kill, or resize operations. Agent-aware status from desktop hooks, push
notifications, session creation, token revocation UI, and multi-client controller
leases remain follow-up work.

## Security notes

- Treat access as remote shell access even though the first API is narrow.
- Never publish port 9867 directly to the internet.
- Pair only over the Tailscale HTTPS URL, not plain HTTP across a network.
- Remove `web.token` from the Burrow application data directory to revoke paired
  browsers, then restart `burrow-web`.
