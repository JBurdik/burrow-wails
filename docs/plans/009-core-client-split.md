# 009 — Split the backend into an always-on core and thin clients

Status: **proposal, not started** (2026-11-03).

## Why

Today there are three processes:

| Process | Owns | After ⌘Q |
|---|---|---|
| `burrow-daemon` | PTYs only (shells, 1 MiB ring per PTY) | keeps running |
| `Burrow` (Go `App` + Wails UI, one process) | chats + their agent processes, phases, SQLite, schedules, queue / sub-agent delivery, usage-limit resume, worktree cleanup, hook server, `/v2/ws`, phone pairing + push, tray, Dock badge | **exits** |
| desktop windows, phone, tray panel | clients over `/v2/ws` | – |

The middle row is the problem. Everything that should "just keep working" —
a running chat turn, a schedule due at 3 am, a queued follow-up, the phone —
dies with the window process. "Close = hide" and the tray exist to paper over
that, and they make ⌘Q a footgun: it reads as "close the UI" but stops the
whole backend.

Related: on 2026-11-03 the daemon stopped being a launchd-subordinate of the
app (`daemonspawn_darwin.go`, a throwaway launchd job). Before that the Dock
showed a quit Burrow as "running in background" because of it. The core
would need the same treatment.

## What is already in place

The remote-access rewrite did most of the hard part:

- The desktop window reads the backend **only** over `ws://127.0.0.1:<hookPort>/v2/ws`,
  like the phone (`src/runtime/transport.ts`). Its only in-process dependency
  is `DesktopService.LocalEndpoint()` for a ticket.
- `remoteAllowed` (`remoteapi.go`) is the full command surface + scope table,
  guarded both ways by tests.
- `main_headless.go` already runs `App.startup` with no Wails at all.
- Every client-visible event goes through `busEmit`, with seq + ring + resume
  (`shellstream.go`), so a client can attach and detach freely.

## Target shape

```
burrow-daemon        PTYs                    (unchanged, separate on purpose)
burrow-core          App: everything above   LaunchAgent, always on
Burrow.app (UI)      Wails windows + tray     client of burrow-core's /v2/ws
phone / iPad         PWA                      client (unchanged)
```

Keep the daemon separate from the core: a core crash or update must not take
the shells with it, just as a UI restart doesn't today.

## Steps

1. **Core binary.** Build `main_headless.go` (darwin, cgo allowed) as
   `Contents/MacOS/burrow-core` in the bundle. Bring it up by default instead
   of in-process `App` behind a dev flag first (`BURROW_EXTERNAL_CORE=1`).
2. **UI ↔ core bootstrap.** Replace `LocalEndpoint()` (in-process ticket) with
   a local credential: the UI reads `<app-data>/control.token` (0600, same
   user) and redeems it for a full-scope ticket incl. `ui:ack`. Being the
   same Unix user is the authorization, like the CLI. Keep `ui:ack` off every
   other path.
3. **Lifecycle.** Register the core as a LaunchAgent via `SMAppService`
   (macOS 13+; login item, visible in System Settings → Login Items). UI
   starts it if not running, waits for `hook.port`, connects.
   Needs a real "Quit Burrow completely" (tray + menu) that stops the core,
   separate from ⌘Q = quit UI.
4. **Version handshake.** `welcome` carries the core's version; UI and core
   from different releases → UI asks the core to restart (after its turns
   settle) or shows a banner. Same idea as `staleAndReplaced` for the daemon.
   The updater (`updater.go`) must restart the core after swapping the bundle.
5. **Native bits.** These live in `desktop.go` today and need a home:
   - tray icon + panel, global hotkey, window management → stay in the UI
     process (no UI = no tray; acceptable for v1)
   - Dock badge, native notifications, `notifyPermission` → UI process, fed
     by events it already receives; with the UI closed the phone push
     (`pushtrigger.go`) is the notification path
   - `desktopEmit` / Wails-only events (`menu-*`, `float-*`, `lsp-msg-*`,
     `update:*`, `window-closed`) stay UI-local; `lsp` and extensions need a
     check whether they belong in the core
   - `pickPaths` (dialogs) must stay in the UI: the core can't show a dialog
6. **Drop close = hide.** Closing the last window can quit the UI for real; the
   tray becomes optional (a `uiPrefs` toggle) rather than load-bearing.

## Linux

The core is the same `main_headless.go` build (`GOOS=linux CGO_ENABLED=0
-tags headless`, already compiles); a phone / PWA in a browser is a complete
client for it today. What Linux needs on top:

- **Lifecycle = `systemd --user`** instead of a LaunchAgent: ship
  `burrow-core.service`; `loginctl enable-linger` keeps it up with nobody
  logged in (VPS / home server). `burrow core install|uninstall` writes the
  unit, so neither platform needs hand-edited service files.
- **The daemon must not live in the core's cgroup.** systemd's default
  `KillMode=control-group` kills every process of a unit on stop/restart —
  i.e. the daemon and every shell with the core. That is the Linux twin of
  the macOS coalition problem (`daemonspawn_darwin.go`). Options: a separate
  `burrow-daemon.service` the core starts with `systemctl --user start`
  (preferred: own lifecycle, own logs in journald), or `systemd-run --user
  --scope`. Outside systemd (containers, plain `nohup`) a plain child stays the
  fallback, as `daemonspawn_other.go` does now.
- **Logs:** under systemd stderr goes to journald; `burrow.log` / `daemon.log`
  in app-data can stay for parity.
- **App-data path:** `appDataDir()` is `~/Library/Application Support/…` on
  every OS; on Linux use `$XDG_DATA_HOME` (`~/.local/share/burrow-wails`) —
  with a migration if anyone already runs it there.
- **Desktop UI on Linux** (Wails v3 GTK/WebKitGTK) is optional and separate:
  `desktop.go` is macOS-first (tray panel NSPanel, Dock badge, Carbon hotkey).
  Until then the PWA is the Linux desktop client.
- **Remote guard:** the funnel check shells out to `tailscale`; verify it
  fails closed the same way on Linux.

## Open questions / risks

- **TCC attribution.** Shells and agents spawned by a LaunchAgent core are no
  longer "responsible" to Burrow.app: Full Disk Access / folder prompts may
  attach to `burrow-core` / `burrow-daemon` instead. Verify (and document
  what to grant) before shipping. Applies to the daemon already since
  2026-11-03.
- **Always-on server.** The loopback hook server + `/v2/ws` now run with no
  window open. The pairing + funnel guard + loopback-only bind stay; the
  difference is visibility: Settings needs a clear "core running / remote
  access on" state.
- **One core build for every OS.** Linux / VPS already use `main_headless.go`; this plan should
  not fork that path — macOS adds a LaunchAgent wrapper, Linux a systemd unit (see Linux).
- **Per-device state** (`burrow.seenAt`, `chatActiveByWs`, layout) is already
  client-side; nothing in it assumes one process.
- **Tests:** `TestRemoteSurfaceIsExhaustive` and `commandSurface.test.ts`
  keep guarding the surface; add one that the UI build links no `App`
  method directly (only `DesktopService` + transport).

## Not in scope

- Merging the PTY daemon into the core.
- Running the core on another machine for the desktop UI (the phone already
  covers "remote backend"; the desktop's `Connect to another Burrow` switch
  exists for that).
