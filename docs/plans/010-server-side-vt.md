# 010 — A terminal emulator per PTY on the server (go-libghostty)

Status: **Phases 0–2 and 4 built** (2026-11-12, branch `v3/server-side-vt`), exercised in a `just dev-mcp` instance — see "Implementation notes". Phase 3 started beta-only (hidden tabs unmount when the daemon has the emulator). Proposed 2026-11-04.
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

## Implementation notes (Phases 1–2, 2026-11-11)

What was built, and where it departs from the plan below:

- **Code:** `internal/ptycore/vt.go` (all libghostty use, `vt` tag) / `vt_off.go` (stubs → `ErrUnsupported`);
  `session.vtMu` covers ring write + `VTWrite`; `Resize` resizes both; 30 s incremental `Compress` loop.
  Daemon ops `screen_text {id, lines}` and `attach {id}` (`VT` is `[]byte` → base64 on the wire, not an int array);
  frame `pty-resync` → bus `pty-resync-<id>` when a write leaves the alt screen (`Events.OnAltExit`).
  Core: `pty_attach` (`scopeTerminal`, `App.PtyAttach`); `tab_output` + `send_to_tab`'s tail read go through
  `control.Deps.Screen` (`ptyWriter.ScreenText`, not an `App` method) and fall back to the UI on **any** error.
  Client: `ptyStream.attach(fetch, paint)` holds live chunks while the snapshot is in flight, then continues at
  `off`; `XTerm.vue` attaches after `create_pty`, re-attaches on `onLost` and — only if it attached during a
  TUI — on `pty-resync-<id>`.
- **No 2 000-line cut.** Attach sends the whole history: `Format(VT)` of the full 8 MiB cap is 3.4 ms / 1.2 MB,
  which is what today's cold path already shipped as a 1 MiB ring (as a JSON int array, ~4×). Revisit if a phone
  attach is slow.
- **There was no cold replay to replace.** `create_pty` on a live id is a no-op in the daemon and nothing called
  `pty_since` at mount, so a re-mounted view (window reopened, phone) started **blank** until the next output —
  the "ring replay" comments in `XTerm.vue` / `Terminal.vue` described a path that no longer existed. Attach is
  the first thing that rebuilds such a view.
- **Build gating:** `just libghostty` (zig 0.17+, `ZIG=` to override) → `src-wails/third_party/` (gitignored).
  `Taskfile.yml` adds `-tags vt` to burrow-daemon only when the lib exists **and** `CHANNEL` is set (beta/dev);
  stable stays ring-only until Phase 3. The dev `go run` daemon fallback (`daemonclient.go`) uses the tag when the
  lib exists.
- **Verified:** `go test -race -tags vt ./internal/ptycore` (clean text, resize, alt + one resync, snapshot offset
  is a chunk boundary mid-flood); tab_output fallback tests; `ptyStream.attach` tests; real `burrow-daemon` over its
  socket + `@xterm/headless`: a view attached mid-flood and one attached during `top` then re-attached on
  `pty-resync` both match a view fed the whole stream (screen, cursor, all 2 000 history rows).
- **GUI check (`just dev-mcp`, driven over Wails MCP + the dev control API):** `location.reload()` (= a fresh frontend
  against live PTYs) restored 3 000 history rows with no duplicates; a reload during `top` showed `top`, and `q` →
  `pty-resync` brought the 3 000 rows back; a reload mid-`seq 1 20000` ended contiguous at line 20000.
- **Not verified:** a real window close/reopen, the phone.
- **Dev fixes found on the way** (pre-existing since plan 009): `just dev-mcp` killed the core (UI and core both start
  Wails' MCP server on 9099 and a taken port is fatal — the core now defaults `WAILS_MCP_PORT=0`), and the dev core
  (launchd, cwd `/`) could not `go run` the daemon — `Taskfile.yml`'s `run` now builds it into `Burrow.dev.app`.

## Phase 3, first step as built (2026-11-12): unmount hidden tabs — beta/dev only

Gated on the **daemon's capability**, not the channel name: `pty_emulator` (`App.PtyEmulator` → daemon `version`
reply `emulator`, `ptycore.Emulator()`) is true only for a `vt` daemon, i.e. beta/dev builds. `src/lib/ptyEmulator.ts`
asks once per window (retries for a minute — a window can boot before its core) and `Terminal.vue` applies the
phone's existing rule on the desktop too: an `XTerm` is mounted only for a visible tab (or one still owing its first
command). A stable app reads false and keeps every terminal mounted, exactly as before. `Terminal` also gets
`shown` from App.vue (its workspace is the one on screen, not welcome / dashboard / another workspace), so a hidden
workspace's xterms go too. Still mounted: RP terminals, and a leaf whose XTerm has a job left:
- `ptyReady` unset — the PTY does not exist yet (mounting is what runs `create_pty`), e.g. `new_tab` into a
  workspace not on screen; set by XTerm's `pty-ready`, and at restore/adopt for a PTY the daemon already has;
- `initialCmd` not yet typed — `cmdSent` (XTerm's `cmd-sent`) ends that, so a spawned agent tab can unmount and a
  remount never types `claude …` again. `initialCmd` itself stays (agent icon, `claude --resume` on restore).

- **Fixed on the way (all channels):** `safeFit` sent the FitAddon's 2×1 floor to the PTY when a view mounted into
  a pane not laid out yet — the shell/TUI redrew at 2 columns for ~150 ms (measured with a `pty_size` poll), leaving
  chopped lines in the scrollback. A 2×1 proposal is now "not measured yet", not a size.
- **Verified live (`just dev-mcp`):** a hidden tab has no xterm; back on it: 3 000 history rows + 500 rows printed
  while hidden, both contiguous, no 2-column lines; `top` started then hidden shows `top` on return and `q` brings the
  3 000 rows back.
- **Known gap (beta):** a hidden tab gets no OSC title and no foreground-name auto-title until shown (both come
  from the mounted XTerm); the phase title from hooks still applies (`Terminal.vue`). Same trade-off the phone has.
- **Verified live too:** a spawned tab (`new_tab --cmd`) hidden and re-shown twice ran its command once; welcome /
  dashboard unmount the workspace's xterms and the route back restores them; `new_tab` on the welcome screen gets a
  PTY at once (`send_to_tab` works) and its xterm unmounts after.
- **Not done yet (rest of Phase 3):** deleting `frozen` / the float `SerializeAddon` relay / `sanitizeTitle`'s mid-OSC
  case — they still serve stable, which keeps everything mounted; they go when the `vt` tag is dropped for stable.

## Phase 4 as built (2026-11-12)

The daemon's `foreground` reply carries `alt` / `atPrompt` (`Manager.ScreenFlags`; OSC 133 via `CursorAtPrompt`).
The poll asks (`DaemonClient.ForegroundScreen`) only in its "unknown foreground, not an agent" branch:
`atPrompt` ⇒ treated as a shell at its prompt (nu / xonsh / pwsh used to sit `running` forever); `alt` ⇒ new
`agentphase.PollInteractive` (running → idle, **no** Done, so quitting vim leaves no review dot). Never above hooks,
`claudestatus.go` or an `IsAgent` leaf. Title: not carried — nothing needed it. Verified live: a `vim` tab stays
idle and ends idle with `turn_ended_at = 0`; `sleep 20` next to it goes running → done as before.

## Phase 0 results (2026-11-11, zig 0.17.1, arm64, M-series)

Spike programs lived outside the repo (throwaway); the numbers:

| Question / metric | Answer |
|---|---|
| Q1 — Plain/VT formatter includes scrollback? | **Yes**, the whole history of the active screen |
| Q2 — primary readable while alt screen is up? | **No.** The formatter sees only the alt screen; after `?1049l` the primary (+ history) is back. ⇒ `pty-resync-<id>` on alt-screen exit **is needed** (Phase 2 step 3) |
| `libghostty-vt.a` build | ~1.5 min cold (`just libghostty`), 12 MB `.a` |
| burrow-daemon size (`-s -w`) | 3.3 MB → **5.7 MB** (+2.4 MB), libc/system frameworks only |
| Throughput | today's pump (ring + JSON int array) **94 MB/s**; `VTWrite` alone 442 MB/s (SGR-heavy) / 124 MB/s (`yes`); pump + emulator ≈ **78 MB/s = 83 %** of today. The JSON int-array wire is the real ceiling, not the emulator |
| Memory at the cap (7.5k rows, 8 MiB) | 8.8 MB resident → **1.6 MB** after `Compress(Full)` |
| `Format(VT)` time | 3.4 ms for the **whole** 7.5k-row history (1.2 MB), 2.1 ms Plain ⇒ under the 5 ms lock budget without the 2 000-line cut |
| Default scrollback cap | libghostty's default **byte** cap is small (677 rows of a git log); `WithMaxScrollbackBytes` must always be set |
| xterm round trip (shell, `git log --color`, vim, top, claude; `@xterm/headless` raw vs. VT snapshot, 120×40) | **text, cursor, buffer type, modes identical** in all five. Known diffs: (1) Ghostty re-encodes 16-colour SGR as `38;5;N` — same palette index, but xterm's default `drawBoldTextInBrightColors` brightens only the 16-colour form, so **bold + basic colour renders a shade darker** after attach; (2) a lone invalid UTF-8 byte shows as U+FFFD instead of nothing; (3) soft-wrap flags on a few alt-screen rows (copy/selection only) |

Exit criteria met (size < 20 MB, throughput > 50 %, round trip identical).

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
