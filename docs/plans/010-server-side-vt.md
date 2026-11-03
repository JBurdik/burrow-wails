# 010 — A terminal emulator per PTY on the server (go-libghostty)

Status: **proposed** (2026-11-04, branch `v3/core-client-split`). Nothing built yet.
Builds on 009 (core / daemon split). Change a decision only on purpose.

## Why

The daemon keeps the last **1 MiB of raw bytes** per PTY (`internal/ptycore/ring.go`).
That ring is good at one thing — filling an exact hole after a dropped `/v2/ws`
(`pty_since`, `src/runtime/ptyStream.ts`) — and bad at everything else:

- **A fresh view can't be rebuilt.** Replaying raw bytes into a new xterm starts
  mid-escape, mid-OSC, mid-alt-screen, at whatever width the bytes were written
  for. Result: corrupted scrollback, garbage titles (`sanitizeTitle` exists for
  this), a TUI that isn't there until it redraws. That is why `App.vue` keeps
  **every** terminal host mounted (CLAUDE.md "View state = the URL"), why a
  popped-out leaf stays mounted `frozen` in the main window, and why the float
  bubble asks the *mounted* xterm's `SerializeAddon` for a snapshot.
- **Server-side readers get raw bytes or nothing.** `tab_output` is a
  `UIBridge` round trip into the mounted xterm (`controlBridge.ts` →
  `tabOutput`): no window, no output. Phase/foreground can't see "a TUI owns
  the screen" (alt-screen) at all.
- **Hidden terminals cost memory in the UI**: N mounted xterms × 5000 lines of
  scrollback + a WebGL atlas each.

Rex (Superlogical) solves this by keeping full emulator state per PTY in its Go
server with `go.mitchellh.com/libghostty`. Verified on this machine:
`go version -m "…/Rex Server.rex/Contents/MacOS/rex"` lists
`go.mitchellh.com/libghostty v0.0.0-20261021212106-df1957cbff56`, built
`CGO_ENABLED=1 -tags=static`, universal (`lipo`: x86_64 + arm64), 88 MB (most of
that is tailscale/gvisor, not libghostty).

## What go-libghostty is (read from source, commit `df1957c`, 2026-10-21)

Cloned from `https://tangled.org/mitchellh.com/go-libghostty`. Same commit Rex ships.

| Question | Answer (file) |
|---|---|
| What | cgo bindings for `libghostty-vt` — Ghostty's VT parser + screen model, no renderer, **libc only** (README) |
| License | **MIT** (LICENSE, © 2026 Mitchell Hashimoto) |
| Maturity | README: *"I'm not promising any API stability yet."* Underlying engine = Ghostty's own, stable. Commits are mostly "update libghostty" bumps. Pin the pseudo-version |
| Feed bytes | `Terminal.Write(p)` / `VTWrite(data)` (never errors on malformed input); `VTWriteUntil(data, boundaries)` stops at APC/DCS/OSC boundaries, no alloc (`terminal.go`) |
| Resize | `Resize(cols, rows, cellWpx, cellHpx)` — reflows the **primary** screen, never the alternate; ends sync-output; sends in-band resize if mode on. `SetResizePullScrollback` (`terminal.go`, `terminal_opt.go`) |
| Read text | `NewFormatter(term, WithFormatterFormat(Plain|VT|HTML), WithFormatterUnwrap, WithFormatterTrim, WithFormatterSelection, WithFormatterExtra{Palette,Modes,ScrollingRegion,Tabstops,Pwd,Keyboard,Cursor,Style,Hyperlink,Protection,KittyKeyboard,Charsets})` → `Format()` / `WriteTo(w)` (`formatter.go`). Formats **the active screen** |
| Serialize for a fresh client | `FormatterFormatVT` + all `Extra*` = VT bytes that reproduce screen, modes, cursor, style, charsets in another emulator. Plus `Continuation()` = the replay-safe suffix of an **unfinished** escape / UTF-8 sequence (needs `WithContinuationMaxBytes`) (`terminal_continuation.go`) |
| Native state save/restore | `Snapshot()` / `SnapshotWriteTo(w)` → opaque binary; `NewSnapshotDecoder(r)` → `Decode()` or `Ready()` + `Next()` (restore usable screen first, older history page by page) (`snapshot.go`, `examples/snapshot`). Ghostty format — **not** something xterm.js can read |
| Modes / screens | `Mode(ModeAltScreen / ModeAltScreenSave (1049) / ModeAltScreenLegacy (47) / …)`, `ActiveScreen()` → `ScreenPrimary|ScreenAlternate`, `MouseTracking()`, `CursorAtPrompt()` (OSC 133), `Title()`, `Pwd()`, `ScrollbackRows()`, `TotalRows()` (`mode.go`, `terminal_data.go`) |
| Effects (callbacks) | title, pwd (OSC 7), bell, desktop notification, progress report, semantic prompt, `WritePty` (replies to DA/DSR queries), clipboard, … (`terminal_opt.go`) |
| Memory | `WithMaxScrollbackBytes` / `WithMaxScrollbackLines`; `MemoryUsage()` (per screen: pages, virtual/resident/compressed bytes); `Compress(Incremental|Full)` + `CompressionActivity()` for idle compression (`terminal.go`, `terminal_data.go`) |
| Threading | **not** safe for concurrent use; serialize every call per terminal; effect callbacks must not call `VTWrite` |
| Build | `#cgo pkg-config: --static libghostty-vt-static`, `-DGHOSTTY_STATIC` (`cgo_static.go`); `-tags dynamic` for a dylib. You must provide `libghostty-vt` yourself: `zig build -Demit-lib-vt` in the Ghostty tree, or this repo's `CMakeLists.txt` (FetchContent of ghostty `f54340ad…`, `-Demit-xcframework=false`). **Zig ≥ 0.17** + CMake |
| Cross / universal | zig targets `aarch64-macos`, `x86_64-macos` (+ linux/windows). Universal = build both, `lipo -create` — what Rex does |
| Binary size | **not measured** (needs the build, see blocker). libc-only static lib; expect single-digit MB. Phase 0 measures |

Not yet verified (Phase 0 must answer):
1. Does `Formatter` on the primary screen include **scrollback history**, or only the viewport rows?
2. While the alt screen is active, can the primary screen (+ history) be formatted at all?
   The formatter takes "the active screen" and no screen argument.

### Blocker on this machine

`zig version` = **0.16.0**; go-libghostty requires **0.17.0+**. `cmake` and
`pkg-config` are present, `nix` is not. Nothing was built. Fix: install zig 0.17
(tarball from ziglang.org, or `brew upgrade zig` once it lands) — Phase 0 step 1.

## Decisions

| Topic | Decision |
|---|---|
| Where the emulator lives | **In `burrow-daemon`** (`internal/ptycore`), next to the ring. Not the core |
| Why the daemon | It owns the bytes: `pump` already writes the ring under a lock, so "emulator state + byte offset" is one atomic read — a snapshot from the core would race the live stream. The daemon outlives core restarts/updates (009), so state survives them for free. The core stays pure Go; only the daemon becomes cgo + zig |
| The ring | **Stays.** Small holes (reconnect) keep using `pty_since` — exact, cheap. The emulator replaces *cold* replay only |
| Query replies | Daemon emulator registers **no `WritePty` effect**. xterm.js already answers DA/DSR/etc.; two emulators answering = doubled replies in the shell. Same for clipboard (OSC 52) |
| Who sizes the emulator | The PTY's size **is** the emulator's size: `Manager.Resize` resizes both under the session lock. No per-client emulator |
| Scrollback cap | 10 000 lines **and** 8 MiB per PTY (`WithMaxScrollbackLines` + `WithMaxScrollbackBytes`); `Compress(Incremental)` on a 30 s idle tick. Tune after Phase 0's `MemoryUsage()` numbers. ponytail: fixed caps, a setting only if someone asks |
| Snapshot depth for attach | Screen + last **2 000** lines of history (xterm scrollback is 5 000; more makes attach slow on the phone). Full history only for `tab_output` |
| Build gating | Build tag `vt` while Phases 1–2 are in beta: without it the daemon is today's ring-only daemon and the attach op answers "unsupported" (client falls back to ring replay). Tag removed in Phase 3 |
| Arch | arm64 only, like today (`Taskfile.yml`: `GOARCH: arm64`). Universal = later, same `lipo` step for both zig outputs |
| Library build | `just libghostty` builds pinned `libghostty-vt` into `src-wails/third_party/libghostty-vt/<zig-target>/` (gitignored), `PKG_CONFIG_PATH` set in `Taskfile.yml` for the daemon build only |

## Attach protocol

New daemon op + core command `pty_attach(id, history)` (`remoteAllowed`, `scopeOrchRead`, like `pty_since`):

```
→ pty_attach {id, history: 2000}
← {off, cols, rows, alt, vt}      // vt: bytes; off: ring end at the moment vt was cut
```

Daemon, under the session lock (the same one `pump` holds for ring + `VTWrite`):
`vt = format(VT, all Extra*) + Continuation()`; `off = ring.end`. Everything the
pump writes after that is `≥ off`, so the client continues exactly where `vt`
stops — no overlap, no gap.

Client (`ptyStream.ts` gains one entry point, `attach()`; still no xterm import):

1. Register `pty-data-<id>` listeners first (as today — `XTerm.vue`'s comment on
   why), holding chunks.
2. `pty_attach` → `term.reset()`; if `cols/rows` differ from the view, resize the
   xterm to them first; `write(vt)`; set `next = off`; drain held chunks through
   the normal dedupe (`skip = next - c.off`).
3. Reconnect: unchanged — `catchUp()` via `pty_since`. If the gap fell out of the
   ring (`onLost`), **re-attach** instead of accepting the hole (today it just
   warns).
4. `pty-exit-<id>`: `reset()` as today; a new PTY under the id → attach again.

Alt screen (open question 2 above): if the formatter can't render the primary
screen while the alt screen is up, the snapshot carries the alt screen only
(what the user sees), and the daemon emits `pty-resync-<id>` when the alt screen
is **left** (checked after each `VTWrite`, cheap `ActiveScreen()`); the client
re-attaches and gets the primary screen + history then. One extra round trip per
TUI exit, only for views attached during the TUI.

### xterm.js impact

- **Resize races.** The emulator reflows on `Resize`; xterm reflows on its own
  `fit`. Attach always happens *after* the view's first `resize_pty` (XTerm
  already fits before streaming), and the snapshot reports `cols/rows` so a
  client that loses the race resizes to the snapshot rather than mis-rendering
  it. A later fit = `resize_pty` → both reflow independently (as today with a
  shell; the shell redraws its prompt).
- **Popout `frozen` leaf.** Unchanged rule: a frozen view never calls
  `resize_pty`. With attach the main window **no longer needs to keep it
  mounted** — the popout window attaches on open, the main window re-attaches on
  bring-back. Phase 3 deletes `frozen`.
- **Float bubble.** `send_float_snapshot` / `SerializeAddon` responder is
  replaced by `pty_attach` in the float window. Delete both in Phase 3.
- **Phone.** Same op. A phone viewer must not resize a desktop-driven PTY; it
  attaches at the PTY's size and lets xterm scroll horizontally / scale (what a
  frozen view does). Who drives the size is unchanged from today.
- **CPU.** Every byte is parsed twice (daemon + each xterm). libghostty-vt is
  Ghostty's hot path; expect noise next to xterm. Phase 0 measures `yes | head -c 1G`.

## What it unlocks

| Unlock | How | Phase |
|---|---|---|
| Clean `tab_output` with no window | verb reads the daemon: `Plain` + `Unwrap` + `Trim`, last N lines; `--format vt|html` for free. Drops the `UIBridge` round trip, works with the UI closed (009's whole point) | 1 |
| Unmount hidden terminals | `App.vue` renders only visible terminal hosts; a remount attaches. Deletes the "keep every host mounted" invariant, `frozen`, the float `SerializeAddon` relay | 3 |
| Alt-screen / prompt signal for phase | `ActiveScreen()` + `CursorAtPrompt()` (OSC 133) from the daemon's `foreground` reply → `phasepoll.go` gets "TUI owns the screen" / "shell at prompt" without guessing from process names | 4 |
| Titles without replay garbage | daemon `Title()` is authoritative; `sanitizeTitle`'s mid-OSC case goes away | 3 |
| Scrollback across daemon restart / update | `Snapshot()` per PTY to disk on clean shutdown (+ periodic), `NewSnapshotDecoder().Ready()` on start; the shell itself is gone, the screen isn't — shown read-only with "session ended" | 5 (later) |

## Phases

Each phase ships to beta on its own.

### 0 — spike (throwaway branch, no product code)

1. Install zig 0.17; `just libghostty` (pinned `df1957c` / ghostty `f54340ad`) for `aarch64-macos`.
2. 50-line Go program: feed a recorded Claude Code session (`script` capture) + `vim` + `htop` + 1 GB of `yes`.
3. Answer open questions 1 and 2 (history in formatter? primary while alt is up?).
4. Measure: daemon binary size delta, `MemoryUsage()` at 10k lines, throughput vs. today's daemon, `Format(VT)` time for 2 000 lines.
5. Write the `vt` output into a headless xterm (`@xterm/headless` in vitest) and diff against xterm fed the raw bytes at the same size.

**Exit:** both questions answered in this file; numbers recorded; xterm round-trip visually identical for the three captures. If size > 20 MB or throughput < 50 % of today → stop and revisit.

### 1 — shadow emulator in the daemon + server-side `tab_output`

1. `ptycore.session` gets `term *libghostty.Terminal` (build tag `vt`); `pump` does `ring.write` + `term.Write` under one mutex; `Resize`/`Kill` follow. Caps + idle `Compress` as decided.
2. Daemon ops `screen_text {id, lines, format}` and `attach {id, history}`.
3. `tab_output` verb reads `screen_text` through the core (no `UIBridge`); falls back to the UI path when the daemon says unsupported.

**Exit:** `burrow tab-output <id>` returns clean unwrapped text with the app window closed; `go test ./internal/ptycore` covers write/resize/attach under `-race`; no measurable latency change in a keystroke echo.

### 2 — attach replaces cold replay

1. `pty_attach` in `remoteAllowed` + `commandSurface.test.ts`.
2. `ptyStream.ts` `attach()` (tests with fake `fetchSince`/`attach`, like today); `XTerm.vue` attaches on mount instead of relying on the create-time ring replay; `onLost` → re-attach.
3. `pty-resync-<id>` on alt-screen exit (only if Phase 0 says it's needed).

**Exit:** open a tab, run Claude Code, `vim`, scroll 3 000 lines; kill + reopen the window / reconnect the phone mid-flood → identical screen, no doubled lines, no title garbage. Manual checklist in `docs/plans/003-manual-test.md`.

### 3 — unmount hidden terminals; drop the `vt` tag

1. `App.vue` / `Terminal.vue`: `v-if` instead of keep-mounted for terminal hosts not on screen.
2. Delete `frozen`, the float `SerializeAddon` responder + `send_float_snapshot` / `notify_float_grid` if unused, `sanitizeTitle`'s mid-OSC handling.
3. Remove the build tag — the daemon always has the emulator; release build gains the zig step (`justfile` `build`).

**Exit:** 20 tabs open, UI memory down measurably (record before/after); popout + bring-back without freeze logic; CLAUDE.md "View state = the URL" and "The shell stream" sections updated.

### 4 — phase / foreground signals

`foreground` reply carries `alt`, `atPrompt`, `title`; `phasepoll.go` uses them as tie-breakers (never above hooks / `claudestatus.go`). Table tests in `agentphase`.

**Exit:** a `vim` tab never shows `running`; a shell at prompt after an agent exit settles without the three-empty-reads delay.

### 5 — persisted scrollback (later, own decision)

`Snapshot()` per PTY on daemon shutdown + every 60 s if `CompressionActivity()` changed, to `<app-data>/pty-state/<id>.ghs` (0600). Restore with `Ready()` → `Next()`. **Encryption** (Rex has `sealfile`) is decided here: scrollback holds secrets (tokens pasted in shells). Default: off until encrypted with a key in the Keychain.

## Risks

- **API churn** — upstream says no stability. Pin the pseudo-version; all use behind one ~150-line file in `ptycore` so a bump touches one place.
- **Toolchain** — zig 0.17 on every build machine (dev, release). Mitigated by the `vt` tag until Phase 3; prebuilt `.a` per target cached in `third_party/` so a normal `go build` needs only `pkg-config`.
- **Two emulators disagree** (Ghostty vs xterm.js on edge sequences, reflow differences). Attach output is Ghostty's view; a sequence xterm doesn't know is lost on attach (it was garbage on replay anyway). Phase 0 step 5 is the check.
- **cgo in the daemon** — a C crash takes every shell down (the daemon is separate *because* shells must survive). Mitigation: libghostty-vt is fuzzed upstream; the daemon recovers nothing from a C fault, so Phase 1 runs a week in beta before Phase 2.
- **Memory moves to the daemon**: 10k lines × N PTYs. Caps + compression; `MemoryUsage()` logged in `burrow status`.
- **Lock hold time**: `Format(VT)` of 2 000 lines runs under the session lock and stalls output for that PTY. Phase 0 measures; if > 5 ms, snapshot via `Snapshot()` copy + format off-lock in a decoded copy.

## Deliberately skipped

- Server-side rendering / ghostty renderer — xterm.js stays the renderer.
- HTML format, Kitty graphics, `Paste`/key/mouse encoders — the bindings have them; nothing here needs them.
- Per-client emulators or per-client sizes — the PTY has one size.
- Replacing the byte ring — it stays the exact-gap path.
- Universal (x86_64) build — arm64 only today; add with the release pipeline when x86_64 ships.
- Persisted scrollback before encryption is decided (Phase 5).
