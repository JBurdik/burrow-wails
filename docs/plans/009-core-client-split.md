# 009 — Split the backend into an always-on core and thin clients

Status: **decided; A + B done, C in progress** (2026-11-03, branch `v3/core-client-split`).
Decisions below came out of a grilling session; change one only on purpose.

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
needs the same treatment.

## What is already in place

- The desktop window reads the backend **only** over `ws://127.0.0.1:<hookPort>/v2/ws`,
  like the phone (`src/runtime/transport.ts`). Its only in-process dependency
  is `DesktopService.LocalEndpoint()` for a ticket.
- `remoteAllowed` (`remoteapi.go`) is the full command surface + scope table,
  guarded both ways by tests.
- `main_headless.go` already runs `App.startup` with no Wails at all.
- Every client-visible event goes through `busEmit`, with seq + ring + resume
  (`shellstream.go`), so a client can attach and detach freely.

## Decisions

| Topic | Decision |
|---|---|
| Goal of v1 | ⌘Q closes only the UI; the phone works with the app closed. Linux = phase 2, first-class |
| Core start | on demand (the UI starts it); Settings toggle "start at login" (login item) |
| PTY daemon | stays separate from the core (core crash / update must not take the shells) |
| Core binary (Mac) | **one binary, two roles**: `Burrow --core` runs the core, plain `Burrow` runs the UI. Packaged as helper `Contents/Library/LoginItems/Burrow Core.app` (`LSUIElement`, copy of the same binary), registered via `SMAppService`. *Refines the grilling's "two entrypoints": `App` lives in `package main` (~200 files), a second `main` package would mean moving the whole backend.* |
| Tray + notifications | in the core: tray menu, tray panel, global hotkey, **all** native notifications (gated by `chat_mark_active`, like `pushGate`). Dock badge stays in the UI |
| ⌘Q | quits the UI; the core quits too **if it has no work** (below). ⌥⌘Q / tray "Quit Burrow Completely" always stops both |
| Core has work | running turn or open request · non-empty queue, undelivered notices, armed limit resume · an active (unpaused) schedule · remote access on (then PTY agent tabs count too) · login-item mode. Otherwise the core exits after 5 min with no UI connected |
| Last window closed | quits the UI process; "close = hide" goes away |
| UI auth | the UI reads `<app-data>/control.token` (0600) and redeems it on loopback for an all-scope ticket incl. `ui:ack` (`POST /v2/local-ticket`). No other path mints `ui:ack` |
| Version skew | `welcome` carries the core version. After an update the core restarts once everything settled (max ~30 min) with a banner meanwhile; an incompatible protocol restarts at once |
| Deep links | URL scheme `burrow://…` handled by the UI (Wails `common:ApplicationLaunchedWithUrl`); notifications / tray open the UI through it |
| CLI | verbs auto-start the core and wait ≤ 5 s; `burrow hook` / `burrow approve` never do (fire-and-forget; TUI decides) |
| Modes | **dev + beta: external core only. Stable: in-process until it switches, then the in-process path is deleted.** Selected by `buildChannel`, no user flag |
| Linux | `systemd --user`: `burrow-core.service` + separate `burrow-daemon.service`, linger, XDG paths, `burrow core install` |

## Target shape

```
burrow-daemon             PTYs                         separate on purpose
Burrow --core             App + tray + notifications   helper .app / systemd unit
Burrow (UI)               Wails windows + Dock badge   client of the core's /v2/ws
phone / iPad              PWA                          client (unchanged)
```

## Phases

Each phase ships to beta on its own.

### A — core as its own process (beta/dev)

1. `main.go` role switch: `--core` → `App.startup` + hook server, no main
   window; UI role → no `App` at all, only Wails windows + `DesktopService`.
   `externalCore()` = `buildChannel != "stable"`; stable keeps today's path.
2. `POST /v2/local-ticket` (Bearer `control.token`) → all-scope ticket incl.
   `ui:ack`. UI-role `LocalEndpoint()` = read `hook.port` + token, redeem.
3. UI start: core reachable? else spawn `Burrow --core` detached (the
   `daemonspawn_darwin.go` launchd launcher, generalised) and wait for
   `hook.port`.
4. `DesktopService` split by where the work happens:
   UI-local (dialogs, windows, float relay, `GetAppVersion`) stay;
   `InstallAgentDocs`, update checks → core commands over `/v2/ws`;
   tray methods → core (phase B; until then the UI keeps a tray-less shell).
5. `desktopEmit` from core code (`lsp-msg-*`, `extension-task:*`,
   `update:progress`) → `busEmit` (`notRingable` for `lsp-msg-*`), and
   `wailsCompat/event.ts` stops routing those prefixes to Wails.
6. ⌘Q quits the UI only; the core keeps running (no idle rule yet).

**Done (2026-11-03):** `corerole.go` (`externalCore`, `runCore` with a
`core.lock` flock, `ensureCore` / `uiLocalEndpoint`), `/v2/local-ticket` in
`controlapi.go`, `spawnDetached(name, argv, …)` shared by daemon and core,
`lsp-msg-*` / `extension-task:*` on `busEmit`, `file-drop` desktop-only.
Verified on a signed bundle copy: core is `ppid 1` and outside the UI's
coalition, ⌘Q removes the app from the Dock while the core + a running chat
keep going, a relaunched UI reuses the core, a `kill -9`'d core is
respawned by the UI within 2 s. Known gaps until phase B: no tray, tray
panel, hotkey or Dock badge on dev/beta. Until phase D an update leaves the
old core running (restart it by quitting it: `pkill -f "Burrow --core"`).

### B — tray, hotkey and notifications in the core

Core role on Mac = Wails app with no main window: tray + `TrayPanel` NSPanel
+ Carbon hotkey move there; `TrayOpen` opens the UI via `burrow://`. Native
notifications from Go (Wails notifications service) on `PhaseStore.onChange`,
same decisions as `pushtrigger.go` + `chat_mark_active` gate; the frontend's
`notifyPermission` / `chatHost.notify*` stop notifying natively (toasts stay).
Dock badge: UI counts from phases it already receives.

**Done (2026-11-03):** the core role is a Wails app with
`ActivationPolicyAccessory` (no Dock icon, `UIElement`): tray, tray panel
(served the in-process ticket), hotkey and `notifications` service live
there. `openUI(route)`: a connected UI (`desktopUIDevice` connections from
`/v2/local-ticket`, `remoteWS.uiConnected`) gets `ui-open` on the bus, else the
core starts one with `open -n -a <bundle> --args --route` (start URL
`/#<route>`). Tray Quit emits `core-quit` (UI quits itself via `QuitApp`)
before stopping. Dock badge: core emits `waiting-count`, `src/lib/coreLink.ts`
→ `SetDockBadge` (+ `waiting_count` on load / resync). Notifications: the
phone-push decisions in `pushtrigger.go` also feed `nativeNotify` (own rate
slot; suppressed while any chat was `chat_mark_active` in the last 90 s; PTY
"done" native-only); the webview's `sendNotification` shim stands down when
`CoreNotifies()`.
Deviations: **no `burrow://` scheme yet** — the core runs from the same bundle,
so LaunchServices would route the URL to the core; it comes with the helper
bundle in C. Not verified: notification authorization on a signed beta (the
ad-hoc test copy is refused outright), the Dock badge visually, tray clicks.
Gap: a terminal-only view sends no `chat_mark_active`, so it gets banners
while focused.

### C — lifecycle

`Burrow Core.app` helper + `SMAppService` (login item toggle in Settings →
General), idle-exit rule, ⌥⌘Q / tray "Quit Completely", last window closed →
UI quits, `burrow://` scheme in Info.plist, CLI verbs auto-start the core.

**Done (2026-11-03), without the helper bundle:**
- `coreidle.go`: pure `coreShouldExit` (table-tested) + a 30 s sweep — no UI
  for 5 min and no `coreWork` (open chat turn, queue / undelivered notice /
  armed limit resume, unpaused schedule, remote access on, `uiPrefs.coreAtLogin`;
  unreadable config = stay) ⇒ the core quits. `BURROW_CORE_IDLE_GRACE`
  shortens it for tests.
- "Quit Burrow Completely" (⌥⌘Q, UI role only) → `POST /v2/core-quit`
  (control.token) → the core's tray-Quit path (core-quit, then stop).
- UI role: closing the main window quits the UI process.
- `burrow` CLI: a verb that finds nobody listening starts `$(cat core.exe)
  --core` (written by every core) without the tab's `BURROW_*`, waits ≤ 5 s
  for hook.port, retries once. A core not started via the launcher re-launches
  itself through it (`BURROW_CORE_DETACHED`), so closing that tab can't kill it.
- `ensureUIOpen`: a UI-performed verb (`workspace_create`, `new_tab`, …) on a
  core with no UI opens one first (serialized; ≤ 10 s + 2 s for App.vue's
  control bridge to mount).
Verified on a bundle copy: idle exit after the grace, a schedule keeps the
core, `burrow workspace_create` with nothing running starts core + UI and
succeeds, `/v2/core-quit` stops both (daemon stays). Not verified: the window
close button (no click automation).

### D — versions + updater

Version in `welcome`; updater swaps the bundle → asks the core to
restart-when-settled; banner in the UI.

### E — stable switch

Stable on external core; delete the in-process path, `quitting` /
`ShouldQuit` / hide-on-close, `LocalEndpoint`'s in-process branch.

### F — Linux

See below.

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
  the macOS coalition problem (`daemonspawn_darwin.go`). Decided: a separate
  `burrow-daemon.service` the core starts with `systemctl --user start`
  (own lifecycle, own logs in journald). Outside systemd (containers, plain
  `nohup`) a plain child stays the fallback, as `daemonspawn_other.go` does now.
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

- **TCC attribution.** Shells and agents spawned by a detached core are no
  longer "responsible" to Burrow.app: Full Disk Access / folder prompts may
  attach to the core / `burrow-daemon` instead. Verify (and document what to
  grant) before stable. Applies to the daemon already since 2026-11-03.
- **Always-on server.** The loopback hook server + `/v2/ws` run with no window
  open. Pairing + funnel guard + loopback-only bind stay; Settings needs a
  clear "core running / remote access on" state.
- **One core build for every OS.** Linux / VPS already use `main_headless.go`;
  don't fork that path — macOS adds the helper app, Linux a systemd unit.
- **Per-device state** (`burrow.seenAt`, `chatActiveByWs`, layout) is already
  client-side; nothing in it assumes one process.
- **Tests:** `TestRemoteSurfaceIsExhaustive` and `commandSurface.test.ts`
  keep guarding the surface; add one that the UI role never starts `App`.

## Not in scope

- Merging the PTY daemon into the core.
- Running the core on another machine for the desktop UI (the phone already
  covers "remote backend"; `Connect to another Burrow` exists for that).
