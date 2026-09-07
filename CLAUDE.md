# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

> **This branch (`rewrite/go-wails`) has replaced the Rust/Tauri backend with Go/Wails v2.**
> Everything below describing `src-tauri/`, Tauri commands, and the Rust event/plugin
> machinery documents the **old** backend (still accurate on `main`) — treat it as an
> architectural reference for *what the Go backend needs to replicate*, not as current
> code on this branch. The actual Go backend lives in `src-wails/`; see its
> command surface across `*.go` files there (one file per subsystem: `workspace.go`,
> `agents.go`, `lsp.go`, etc.) and `src/lib/wailsCompat/` for how the
> unmodified `src/` Vue frontend is wired to it. Progress/remaining-gaps status:
> `~/.claude/plans/ahoj-pros-mt-el-by-distributed-glade.md`.

# Dictionary
- RP = Right pannel


## What this is

**Burrow** — a desktop app (macOS-first) that wraps real PTYs in a multi-workspace IDE shell, designed to run AI coding agents (Claude Code, Aider, Codex, etc.) side-by-side in terminal tabs. The product name is **Burrow**; the repo/package name is `agentic-ide`.

Stack: Vue 3 + Pinia + xterm.js frontend, Go/Wails v2 backend (`src-wails/`), SQLite for persistence.

## Commands

```bash
# Frontend-only dev (browser, no Wails window)
pnpm dev

# Full native dev (Wails window, hot-reload)
just dev            # cd src-wails && wails dev

# Type-check + production build
pnpm build          # vue-tsc + vite build
just build          # full native bundle (frontend + wails build)

# Go only
cd src-wails && go build ./...
cd src-wails && go test ./...
```

Tests: `pnpm test` (vitest, no DOM env). Currently covers only `src/machines/agentStatus.ts` — the status state machine. `just` (Justfile task runner, `brew install just`) drives dev/build/release; see `justfile`.

## Architecture

### Frontend (`src/`)

**Pinia stores** are the backbone — components talk to stores, not each other:

| Store | Owns |
|-------|------|
| `workspace` | List of workspaces (SQLite-backed via Wails bindings), which one is active, which are "opened" (PTYs kept alive) |
| `terminalTabs` | Lightweight mirror of each workspace's tab list for the Sidebar; the real Terminal component is source of truth |
| `agents` | Configurable agent presets (command, args, shortcut, color) persisted to `localStorage` |
| `ui` | Settings panel open/close, font + scale preferences (persisted to `localStorage`). **Not** the view state — `mode`/`welcomeVisible`/`viewingTabs` are computed from the route |
| `terminal` | Legacy simple terminal store (mostly superseded by XTerm.vue) |
| `fileTree` | File tree state for the sidebar |
| `git` | Git status / diff for the right panel |

**Component hierarchy:**
```
App.vue
  TitleBar
  Settings (overlay)
  Sidebar              ← workspace list + nested tab list from terminalTabs store
  [resize handle]
  Terminal             ← one per opened workspace (kept mounted, hidden when inactive)
    TerminalSplitView  ← manages split panes
      XTerm.vue        ← wraps xterm.js, owns PTY lifecycle via Wails bindings
  [resize handle]
  RightPanel           ← file tree + git panel
  Spotlight            ← ⌘P command palette
```

**Key keyboard shortcuts:** `⌘,` settings, `⌘P` spotlight.

### View state = the URL (`src/router.ts`)

`/` welcome composer · `/ws/:wsId` a workspace's tabs · `/ws/:wsId/tab/:tabId`
one tab · `/dashboard` · catch-all → `/`. Hash history (no server behind the
Wails asset scheme), memory history outside a browser so store tests can import
the module.

**There is no `<router-view>`.** `App.vue` reads the route and shows the surface
itself, because the terminal host must stay mounted across every route —
re-attaching a PTY replays the daemon ring buffer into a re-fitted xterm and
corrupts the scrollback. Two guarded watchers in `App.vue` keep route and
workspace/tab stores in step. Navigation is how you focus something: `focus_tab`
pushes `/ws/:id/tab/:pty`, clicking a tab `replace`s the same shape.

This replaced `ui.mode` + a tri-state `ui.welcomeOpen`, which every new piece of
code had to remember to consult — and one that forgot is how a tab behind the
welcome composer counted as "watched". Plan + rationale (incl. what was taken
from `pingdotgg/t3code` and what deliberately was not):
`docs/plans/003-view-state-routes.md`.

### The chat list is shared state (`src-wails/chats.go`)

Chats used to live in `config.json` under `chatSessions`, and that was the last piece of
**shared** state stored in a client-owned blob. `src/lib/config.ts` reads the whole file
once at boot into a module-level cache and rewrites the whole cache on every `setConfig`;
`App.WriteConfig` is an atomic whole-file overwrite with **no merge**. Two independent
writers — the desktop frontend and Go's `RemoteCreateChat` — therefore did read-modify-write
on one blob, and last writer won on *every key*. Measured, not theorised:

- the desktop never learned about a phone-created chat, because nothing emitted a change
  event for chats (workspaces had `workspaces-changed`, phases had `phase-pty:`; the chat
  list had nothing);
- any desktop `setConfig` between the phone's read and its own next write reverted the
  phone's row **and** the `chatIdCounter` bump — a font preference was enough;
- with the counter reverted, the next desktop chat took the id the phone was already
  using, and its UI attached to the phone's still-running CLI process.

Now it is a table Go owns, and two properties do the work. **`INTEGER PRIMARY KEY
AUTOINCREMENT`**: ids come from the database and are never reused, which matters because
`chat_stream(chat_id)`, `chat_messages` and `pty_phase`'s `chat:<id>` keys all reference
them — a plain `INTEGER PRIMARY KEY` recycles the highest freed rowid and would hand a new
chat a deleted one's transcript. **`SaveChats` upserts and never deletes**: a client whose
list predates another client's creation *cannot* remove a row it has not heard of, so the
lost-creation failure is structurally impossible rather than merely unlikely. Removal is an
explicit `DeleteChat`.

What remains, said out loud: two clients editing the *same field of the same chat* at the
same instant still resolve last-writer-wins. That is per-chat-per-field instead of
per-file-per-anything, and `chats-changed` makes both converge within a round trip.

**`busy` and `status` are not columns.** `busy` was always persisted as `false` anyway, and
the status **is** the phase (`pty_phase`, keyed `chat:<id>`) — a second copy of it in a
second table is exactly the drift the phase work removed.

**What is still per-device, and why.** `chatActiveByWs` stays in `config.json`, next to
`burrow.seenAt`: which chat is selected is this device's business, and the desktop being on
chat 78 says nothing about what the phone should show. Same reasoning as `review` being a
read receipt rather than a phase. `chatTurns` and `chatPermissionRules` also stay — an
activity log and a user preference, neither of them the shared-list problem.

The migration **preserves ids** rather than reassigning them (renumbering would orphan
every transcript in the app), runs before anything serves a client, is a no-op on a
populated table, tolerates a hand-broken `config.json`, and prunes `chatSessions` /
`chatIdCounter` afterwards so no stale copy is left looking authoritative. It does **not**
bump `sqlite_sequence` by hand: SQLite advances the sequence itself on an explicit-rowid
insert that exceeds it, and a test asserts that rather than trusting it. Verified against
the real install — 69 chats migrated, ids intact, next id one past the highest.

A phone-created chat is titled `Chat N (phone)`. The desktop uses `Chat N` too, so a
remotely-created one used to be indistinguishable from the fifty above it in the sidebar —
which is how the chat in the original bug report managed to be on screen and still
impossible to find.

### Chat stream ownership (`src/lib/chatSession.ts` + `src-wails/chatstream.go`)

A chat's stream is owned by a **session registry keyed by chat id**, not by
`AgentChat.vue`. The session holds the transcript, turn state, blocking requests
and the `claude-data-{id}` / `acp-data-{id}` / `acp-req-{id}` listeners; the
component installs its reducers with `setHandlers` on mount and `release()`s on
unmount. A session is only torn down when it is **idle** — a running turn or a
pending permission keeps streaming behind an unmounted view (ported from
t3code's `shouldEvictThreadDetailSubscription`). That is what lets chat leaves
render with `v-if` (`Terminal.isChatVisible`), so "is the user looking at this"
is whether the component exists rather than a predicate someone can forget.

Backing it, every agent line is also appended to SQLite `chat_stream(chat_id,
ord, kind, line)` before it is emitted, and the event payload is `{ord, kind,
line}`. `chat_stream_state.folded_ord` records how far the frontend has folded
that log into `chat_messages` (written in the same transaction as the messages),
so the trim can never delete a line nobody rendered. After a restart,
`replayChatStream()` catches a chat up from `folded_ord`.

### Provider protocol is parsed in Go (`src-wails/providerruntime.go`)

Claude stream-json and ACP JSON-RPC are read in **one** place, on the Go side,
and re-emitted as provider-neutral `ProviderRuntimeEvent`s on
`chat-event-{chatId}` (`{ord, events}`) alongside the raw channel. Vocabulary:
`text.delta` · `thinking.delta` · `user.delta` · `tool.started` ·
`tool.completed` · `turn.completed` · `turn.failed` · `session.title` ·
`session.id` · `session.exited`.

The frontend previously had **three** partial parsers (`onLine` in
`AgentChat.vue`, `lib/providerRuntime.ts`, and a thinner one in
`src/mobile/store.ts`), only the first complete — so a second client could not
get a correct transcript without re-implementing the protocol to its own depth.
The parse belongs to whoever owns the process.

What is left on the frontend is rendering: `src/lib/chatProjection.ts` turns
events into `ChatMessage[]` (append-to-partial, tool-result matching), and
`AgentChat.onEvents` does what only a mounted view can — scroll, notify, account
for the turn. `LoadChatEventsSince` replays the recorded log as events, which is
what `replayChatStream()` uses.

**Still on the raw channel, deliberately** — these are UI decisions, not
transcript: the control/permission protocol, Claude `system/init`, the ACP
handshake (`modes`/`configOptions`), `serverRequest/resolved`, and the
`acpPromptRpcId` correlation that settles an ACP turn. `chat_messages` is also
still written by the frontend; making Go its sole writer is fáze 7 in the plan
and has a real prerequisite (the transcript mixes stream-derived messages with
client-authored ones).

### PTY / Agent phase (`src-wails/internal/agentphase`)

Each `XTerm` creates a native PTY in Go (`CreatePty`), streams bytes via a Wails event `pty-data-{id}`, and sends input back via `WritePty`. That is now nearly all `XTerm.vue` does for status: it has **no status emits** any more (agent state used to travel through it as `agentState`/`agentMeta`; both are gone, and its `interrupt` emit moved into Go's `WritePty` — see input 3 below). It still polls `get_pty_foreground` every 2 s, but that poll now drives auto-titling only (naming the tab after the foreground agent/command) — the busy/needs-input derivation and the dead-PTY watchdog that used to run against the same read now run in Go (`phasepoll.go`) instead, where they work for a workspace nobody has mounted and for a client that isn't this window.

**The phase itself is derived in Go, not in the frontend**, so it exists with no client attached at all — the phone asleep, the PWA closed, a cold app restart. This is a hard requirement of the remote-access rewrite: a websocket or MCP client connecting later must be able to ask "what state is this agent in" without having driven any of the events that produced the answer.

- **`internal/agentphase/phase.go`** — a pure function `Next(cur Phase, ev Event, now int64) Phase`, no `database/sql`, no Wails runtime, no `main` import; IO is the store's job, not this package's. States: `idle | running | waiting_input | waiting_approval | done | failed | stale`. Deliberately no `starting` state, and deliberately no `review` (see below — whether a finished turn still needs looking at is per-device, not part of the phase). `Phase` also carries `detail` (error_type / blocking tool), `model`, `title`, `is_agent`, `turn_ended_at` (0 mid-turn), `updated_at`, and is a comparable struct on purpose: the store skips a write and an emit whenever `Next()` returns the input unchanged.
- **`phasestore.go`** — `PhaseStore` keeps one `Phase` per key (`pty:<ptyID>` or `chat:<chatID>`, one type for both so a terminal and a chat can never drift into two derivations of "is this agent busy"), persists it in the `pty_phase` table, and emits `phase-pty:<id>` / `phase-chat:<id>` carrying the whole `Phase`. Two producers — the hook server and the foreground poll — can call `Apply` for the same id from their own goroutines; a per-id monotonic `seq` (not `updated_at`, which can tie at millisecond resolution) guards both the SQLite upsert and the bus emit so an out-of-order write can't win either. The seq check alone was not enough — it and the emit have to be *atomic*, or the loser can still emit last and leave the UI a step behind a correct row — so the whole publish tail (staleness check → `persist` → `busEmit` → `terminal_tabs` mirror) runs under a dedicated `emitMu`, taken after `s.mu` is released so no sink ever runs under the state lock. **Phases have a lifecycle**: `CreatePty` asks the daemon whether it already holds the id, and an id the daemon does *not* list is a fresh spawn, not a reattach — it drops the phase row, the map/seq entries (`PhaseStore.Forget`), the poller's watchdog counter and the legacy hook-status cache. PTY ids **are** reused (the frontend's counter reseeds from `max(saved, daemon-alive)`), so without that a new "Terminal 2" opened wearing the review dot and task title of whatever held id 2 last. `Get`/`All` report a never-seen id as `idle`, never `""`. `PhaseStore.Apply` is also the **only** writer of `terminal_tabs.status` (the old `SetTabLiveStatus` Wails binding the frontend used to push this itself is gone) — so `burrow list-tabs` and MCP `list_tabs` now report the phase vocabulary (`waiting_input`, `waiting_approval`, `failed`, `stale`) straight out of SQLite, and never `review`, which never existed server-side.
- **Inputs, three of them:**
  1. **Global persistent hooks** — unchanged mechanism, because it is what makes status work for every agent session (launched-by-button, typed by hand, or reattached after restart): at startup `installStatusHooks` (`statushooks.go`) merges a status hook into each agent's own global config (Claude `~/.claude/settings.json`, Codex `~/.codex/hooks.json`), non-destructively (appends, dedupes by marker, writes a `.burrow-bak`), a no-op outside Burrow (`BURROW_PTY_ID` unset). Inside a Burrow PTY, `burrow hook` maps `hook_event_name` → state exactly as before (`UserPromptSubmit`/`PostToolUse`→running, `PreToolUse`→waiting for the blocking tools, `PermissionRequest`→permission, `Stop`→done except an interim stop still carrying `background_tasks`, `SessionStart`→session metadata, `StopFailure`→error with `error_type` as `detail`, `Notification`→refined by its `type`) and POSTs `{ptyId,state,…}` to the loopback hook server (`burrow status` reads `<BURROW_HOME_DIR>/hook.port`, rewritten each launch, so the port survives a restart). What changed is what happens next: `hookEvent()` in `hookserver.go` also translates the same payload into an `agentphase.Event` and calls `phases.Apply("pty:"+id, ev)`. The legacy `pty-hook-{id}` bus event is **gone** (phase 6): it survived only for the mobile client's own status derivation, and once the phone moved onto phases it had no consumer left. A hook now has exactly one effect. `HookServer`'s in-memory status cache went with it — `ReplayStatus` replays from `PhaseStore`, which is the better copy anyway since it survives a restart and the map never did.
  2. **Foreground poll** — moved to Go entirely (`phasepoll.go`), ticking every 2 s server-side rather than being driven by `XTerm.vue`. Same semantics as before: an agent's presence in the foreground process group is never treated as `busy` (an agent stays foreground whether it's thinking or idle at its prompt — equating presence with busy was the old stuck-orange-dot bug), so the poll only ever sets `is_agent` for a recognized agent CLI and drives `running`/`done` for plain shell commands — `pollOne()` only ever applies `PollAgent`/`PollBusy`/`PollNotBusy`. **Only the shell branch may CLEAR `is_agent`** — the shell being foreground is the one thing that proves the agent is gone. Any other unrecognized name on a leaf already flagged `is_agent` is *no news* (the agent opened a pager, ran `git`, spawned `node`), and `pollOne` returns without applying anything; treating it as a plain command would strip the flag, un-gate `PollBusy` on the next line, and overwrite a done-but-unseen turn with a permanent `running`, flapping a write and an emit every 2 s. `agentphase.Next` also defines `PollNeedsInput`/`PollGotInput` for a plain command's own `waiting` state, but nothing in the tree emits them (only `phase_test.go` exercises them): the old client-side heuristic that drove `waiting` for a plain command lived in `XTerm.vue` and was not replaced when it moved to Go. **Dead-PTY watchdog**: three consecutive empty foreground reads *and* the daemon no longer listing the PTY settle an in-flight phase to **`stale`** (`agentphase.Dead`) — this replaced the old `interrupt` event name; a single empty read is still treated as a transient daemon race and ignored.
  3. **The interrupt keystroke** — `App.WritePty` watches for a payload that is exactly one byte of `0x03` (Ctrl+C) or `0x1b` (ESC) and applies `agentphase.Interrupt`. Cancelling a turn fires **no** `Stop` hook, the poll may not speak for an agent, and the watchdog can't fire on a live PTY, so this write is the only evidence the turn ended — without it the dot sticks orange until the next turn starts. It lives in Go rather than in `XTerm.vue`'s `onData` (where it used to) so the phase stays derivable server-side. `Next` guards `Interrupt` on `cur.InFlight()`, so a stray ESC at an idle prompt cannot wipe an unseen `turn_ended_at` and erase a review dot; a cancelled turn settles to `idle` with no receipt, hence no badge. Length is what separates a cancel from a cursor key — arrow keys arrive as ESC plus more bytes in one write.
- **Chats feed the same store** via provider runtime events rather than hooks: `chatPhaseEvent()` in `providerruntime.go` maps a `ProviderRuntimeEvent` (`text.delta`/`user.delta`/`thinking.delta`/`tool.started`→running, `turn.completed`→done, `turn.failed`→error, `session.title`→metadata, `session.exited`→`Dead`, i.e. `stale`) onto an `agentphase.Event`, applied at the emit site in `chatstream.go`'s `emitChatLine` (`a.phases.Apply("chat:"+chatID, pev)`). Thinking and tool calls count because a turn that opens with a tool call reaches its first text token much later; `session.exited` counts because the poll only walks `pty:` keys, so nothing else can settle a chat whose CLI died mid-turn (it is a no-op after `turn.completed`, which is the order Claude sends the pair in). **Nothing on the frontend consumes `phase-chat:{id}` yet** — chat status today still comes entirely from `src/machines/agentStatus.ts` (see below); the chat phase is computed and persisted, but not yet wired to a client.

**What the client owns is the read receipt, not the phase.** `src/runtime/displayStatus.ts` is a pure function `displayStatus(phase, seenAt, watching) → TermStatus` (`idle|running|waiting|permission|done|review|error`) plus `shouldMarkSeen()`. `review` and the transient lime `done` are **not phases** — a `done` phase renders as `review` when unwatched and lime `done` when watched, both derived from comparing `phase.turn_ended_at` against the client's own `seenAt` for that leaf. `Terminal.vue` persists `seenAt` per leaf in `localStorage` under `burrow.seenAt` (shared across every mounted workspace, so it's read-modify-written, never overwritten wholesale) and listens directly on `phase-pty:{leafId}` (`applyPhase()`), recomputing the dot and firing side effects (sounds, notifications, checkpoint snapshots, round counter) only on genuinely *entering* a status — there is no longer a client-side state machine for terminals to race against a `markTabSeen()` call. `tabStatus()` priority is unchanged (`STATUS_PRIORITY` in `terminalStatus.ts`): **error** > permission > waiting > running > review > done > idle.

**`src/machines/agentStatus.ts` still exists — it was not deleted.** Terminals are fully off it (replaced by the phase + `displayStatus` above). `src/stores/claudeChats.ts` and `AgentChat.vue` still drive one instance of it per chat session, because chat permission state today arrives on the control/permission protocol (a pending `control_request`: generic tool/file edit → `permission`+bell, `AskUserQuestion`/`ExitPlanMode` → `waiting`) rather than as a phase — the chat phase computed above has no frontend consumer yet. Read it as chat-only, not dead code.

**Claude chat sessions** (`ClaudeChat.vue` + `claudeChats.ts`) carry the same `status` vocabulary (`running`/`waiting`/`permission`/`idle`) via `chatStatus()` and the machine above. The **Sidebar renders chats and terminal tabs as one list** distinguished only by icon (`ClaudeIcon` vs `PhTerminal`/`PhRobot`) — no separate "Chats" header; "New chat" lives on the workspace header row. A permission request also fires an in-app toast + (when unfocused) a native OS notification via `notifyPermission()`. Switching permission mode / aborting restarts `claude` with `--resume`; the teardown `exit` is squelched by `suppressNextDone` so it no longer fires a spurious "finished" toast.

### Control API + `burrow` CLI (`src-wails/internal/control`, `src-wails/bin/burrow`)

**One implementation of every app action, three doors.** `internal/control` holds a
registry of *verbs* (`spawn`, `agent_status`, `focus_tab`, `create_worktree`,
`pr_merge`, …) and knows nothing about HTTP, MCP or Wails — it takes its
capabilities as interfaces (`Deps`: DB, git/gh/exec runners, PTY writer,
worktrees, `UIBridge`). Transports sit on top:

| Transport | Client | Auth | Verbs |
|-----------|--------|------|-------|
| loopback HTTP `POST /v1/<verb>` (on the hook server's port) | `burrow` CLI (curl), `burrow-mcp` | `control.token` (0600, next to `hook.port`), `Authorization: Bearer` | all |
| tailnet HTTP (`httpserver.go`) | mobile / PWA | `http.token` + pairing code | `ScopeRemote` only |
| Wails bindings | the desktop UI | in-process | as needed |

`Scope` is a field on the verb, and a verb is **local-only unless it opts in** —
a new verb that never thought about the network stays off it. The registry is
also the single source of truth for the MCP tool schemas (`/v1/_verbs`), the
CLI's `burrow help`, and the Manager's primer, so none of them can drift from
what the app supports.

**UI-performed verbs.** Opening a tab, focusing a workspace and reading a
terminal's scrollback can only be done by the frontend, so those verbs call
`UIBridge.Do`, which emits a `control:action` Wails event and **blocks for the
frontend's ack** (`AckControlAction`, 15 s timeout). `src/lib/controlBridge.ts`
is the single app-wide listener that performs them and acks with a JSON result —
so `spawn` can hand the caller the new tab's `pty_id`, and an unreachable UI is a
real error instead of a hang. This replaced the old file-based request-dir
transport (`take_spawn_requests` polled by every `Terminal.vue` at 1 Hz), and with
it the 1 s latency, the double-claim routing rules, and the "target workspace must
be mounted" caveat.

**The `burrow` CLI** is a thin generic client: `burrow <verb> [POSITIONAL] [--arg value]`,
where the verb and flag names are normalised from kebab-case, positionals map to
the verb's primary arguments (`_primary` in the script), and `cwd` always rides
along as `$BURROW_CWD` so agents never handle workspace row ids. It needs only
`curl` and `sed` — no `python3`, no `node`, no tty — which is what makes it work
from Claude's Bash tool and from hooks. `burrow help` prints the live registry.

Its remaining non-verb subcommands are the status plumbing, unchanged and
deliberately independent of the control API (they must work before it is up):
- `burrow status <state> [--detail/--model/--source/--title/--pid]` — POSTs to `/hook`; sticky states retry 3× with a `hook.port` re-read. **`/hook` is the path this has always used; serving only `/status` in Go silently killed every status dot, because the CLI's `curl -sf` failed on the 404 and exited 0.**
- `burrow hook` — invoked by the globally-installed Claude/Codex hooks; maps `hook_event_name` → state.
- `burrow notify '<json>'` — legacy Codex notify-program path.
- `burrow capture <token>` — run by a spawned agent's per-launch Stop hook (`XTerm.vue` injects `--settings` when a leaf has a `resultToken`): writes `<session>/<token>.result` + `.done`, calls `burrow status done`, then POSTs `/agent-done` so the app can emit `control:result`. `wait_result`/`collect_results` read those files, which is why results survive an app restart.

**`burrow-mcp`** (`cmd/burrow-mcp`, built into the bundle by `just build`) is the
same verbs as MCP tools: `tools/list` is `/v1/_verbs` translated to JSON schemas,
`tools/call` is one POST. It holds no DB and no logic, so an MCP tool cannot
behave differently from `burrow <verb>`. It's injected into chat sessions by
`burrowMcpServers` (Claude: `--mcp-config`) and `acpMcpServers` (ACP:
`session/new`), and skipped silently when the sidecar isn't next to the
executable (a `wails dev` run) — the CLI still works.

`BURROW_*` env exported into every PTY: `BURROW_SESSION_DIR`, `BURROW_CWD`,
`BURROW_PTY_ID`, `BURROW_HOOK_PORT`, `BURROW_HOME_DIR` (app-data dir, which also
holds `hook.port` and `control.token`).

### Desktop transport (`src/runtime/transport.ts` + `src-wails/remoteapi.go`/`remoteproto.go`/`remotews.go`)

The desktop's entire data path — every PTY byte, every chat event, every SQLite-backed
list/save call — travels over a websocket, `ws://127.0.0.1:<hookPort>/v2/ws`, mounted on
the hook server's **always-on loopback mux** (not the tailnet HTTP server above, which
starts and stops with the remote-access toggle). This is what a later phone client will
connect to as well, against a different URL: remote access stops being a second API
surface that can drift from the first, because there is no longer a first one to drift
from. `src/lib/wailsCompat/core.ts` used to be a ~130-case switch mapping wire names onto
Wails-generated bindings; it is now a thin **pass-through** onto this socket — the switch's
old body is what became the command table below. What is left in `core.ts` is a short
`CLIENT_SIDE_COMMANDS` set — `detach_pty` (resolves locally; there is nothing to detach,
the PTY keeps running server-side for the next reattach) and `send_float_snapshot`/
`notify_float_grid`/`open_git_panel_window` (stay on Wails bindings because they answer
over `runtime.EventsEmit`, which only the native window ever receives, or serve the
removed float-window feature outright) — plus a few cases that reshape args before
forwarding (`list_pty_sessions` normalizes daemon ids into session records,
`acp_start` nests flat call-site args into the one Go struct parameter the table names,
`save_chat_messages` restores the `foldedOrd ?? -1` sentinel the old switch supplied).

**`remoteapi.go`'s `remoteAllowed`** is the security boundary: wire command name → `App`
method + positional argument names + a `remoteScope`. Argument names live in the table,
not the method signature, because they do not exist at runtime for either side to read
off — Go's `reflect` does not carry parameter names, and Wails-generated bindings are
`arg1..argN`. Scope is enforced **per command**, not per connection: holding a ticket is
not authorization to call everything it reaches. Two tests guard the table from opposite
directions: `TestRemoteSurfaceIsExhaustive` walks every `App` method and fails if it is in
neither `remoteAllowed` nor `remoteDenied` — a new method is unreachable until someone
decides on purpose which list it belongs in — and `src/lib/wailsCompat/
commandSurface.test.ts` walks every literal `invoke("...")` call site under `src/` —
including `src/mobile`, since phase 6 put the phone on this same table — and fails if the
wire name is in neither the table nor `CLIENT_SIDE_COMMANDS`. The first test cannot catch a wire name the
frontend calls that the table forgot; the second cannot catch a table entry nothing calls
— only together do they cover both directions the flip could break.

**`remoteproto.go`** frames the wire in four tags: `call` (client → server), `reply`,
`event`, `welcome`. Event names on the wire are identical to the internal bus names
(`pty-data-7`, `phase-pty:7`) on purpose — no translation table to forget an entry in. A
`call` frame with a non-positive id is rejected at decode time, because id `0` would
vanish from a reply under `omitempty` and arrive indistinguishable from an event.

**`remotews.go`** mounts `/v2/ws` on the hook server's mux (`StartHookServer`, unrelated to
and unaffected by the remote-access toggle) and issues a single-use, 30-second ticket per
connection attempt (via `App.LocalEndpoint()`, below) rather than a long-lived token —
browsers cannot set headers on a WS handshake, so the ticket has to ride in the query
string, and a token that dies on first use is safe to put there in a way a durable one is
not. Each connection has **one outbound queue and one writer goroutine**: two goroutines
must never write the same socket, and a single queue is what keeps a reply and the event
that follows it in the order they were produced. A full queue **drops the client** rather
than blocking — `busEmit` runs under `PhaseStore`'s `emitMu`, so a sink that blocked would
stall every phase change in the app, not just this one connection's view of it. Each call
frame is dispatched on its own goroutine behind a **64-slot per-connection semaphore**: one
slow call — `generate_commit_message`'s 180 s CLI budget, say — used to head-of-line-block
every other frame on the same connection (keystrokes to a terminal included), because the
desktop uses exactly one connection for the whole app; a `recover` per call turns a
panicking one (several `App` methods dereference `a.daemon` with no nil guard) into a
failed reply and a dropped connection, never a dead process. Read limit, write/read
deadlines and ping/pong keepalive reclaim a peer that vanished without a TCP FIN — a phone
leaving the tailnet, a sleeping laptop — within one missed cycle instead of parking the
reader in a syscall forever.

**`App.LocalEndpoint()`** is the **one Wails binding left on the desktop's data path**, and
the reason is worth stating on purpose: being in-process IS the desktop's authorization,
which is not a claim anything reachable over the network can make. It returns a fresh
ticket scoped to every scope that exists — including `scopeUIAck`, granted nowhere else
(see `ack_control_action` just below) — and the frontend calls it again on every
reconnect, since a ticket is spent by the handshake that redeems it. `LocalEndpoint` is
itself in `remoteDenied`: reachable over a connection it authorizes, it would let an
already-authenticated remote client mint itself a fresh full-scope ticket.

**`ack_control_action` has its own scope, `ui:ack`**, granted only by `LocalEndpoint`. The
bus sink `remotews.go` subscribes with is unfiltered — every connected client sees every
`control:action` frame, including its id — so `AckControlAction` authenticates nothing
beyond matching that id against a pending request; the scope is an identity claim ("I am
the UI a verb is waiting for"), not a level of authority, and is kept off
`orchestration:operate` so that a future paired phone inheriting that scope for legitimate
reasons does not also inherit the ability to forge a reply to a control verb it never
performed.

**`src/runtime/transport.ts`** is the client half, and the *only* place the desktop and a
remote client differ — both use this transport, and what varies is the
`EndpointSource` handed to it (`src/runtime/boundary.test.ts` keeps `src/runtime` from
importing `src/components`, `src/stores`, `src/mobile` or xterm, so it cannot grow a
dependency only one side has). It queues calls made before the socket opens (the frontend
calls `invoke()` from `onMounted` while the connection is still being made), keeps its
listener map across reconnects (the server fans every event out to every connection, so
this map is the client's own routing table and has to outlive a socket, or a reconnect
silently stops delivering PTY bytes), asks for a fresh ticket on every attempt (a ticket is
single-use), and **rejects every queued call rather than replaying it** once a connection
actually drops — a queued call replayed against the next connection could arrive for an id
whose promise was already rejected, with nowhere for the eventual reply to go.

### The shell stream: `seq`, the ring, resume (`src-wails/shellstream.go` + `shellsnapshot.go`)

A dropped connection used to be data loss. `busEmit` now **numbers** an event before it
fans out — one counter, one `shellEvent{seq,name,payload}` every sink sees — and keeps the
last **512** in an in-memory ring (`shellRingSize`). The `welcome` frame carries
`currentSeq()`, so a reconnecting client knows where the server is; it sends
`{t:"resume", since}` and gets either a `shell` frame of the deltas or a `resync` telling it
to start over from a snapshot. **A process restart always means `resync`**: the numbering
begins at zero, so `since > shellSeq` is the give-away and there is nothing to hand back.
There is deliberately **no per-client state on the server** — the ring is shared and
`resumeSince` is a pure function over it, so a client the server has never heard of
resumes exactly as well as one it just dropped.

**What the ring does not hold** (`notRingable` in `bus.go`) is everything that is both
high-volume **and** already replayable from a durable log of its own: `pty-data-*` (the
daemon keeps its own ring and replays it on reattach) and the chat channels
`claude-data-*`, `acp-data-*`, `acp-req-*`, `chat-event-*` (persisted to `chat_stream`
with an `ord` before they are ever emitted; `replayChatStream`/`LoadChatEventsSince` catch
a client up from `chat_stream_state.folded_ord`). This is not only about the cost of
ringing them — it is what makes resume work at all. One streaming agent emits hundreds of
chat lines per turn, so ringing them would churn all 512 slots in seconds and every
reconnect would come back a `resync`, making the ring dead weight. What is left is shell
state — phases, workspaces, pty exits, control results — which changes a handful of times
a minute, so 512 slots is hours of history. The consequence to keep in mind: a dropped
connection still loses **PTY bytes** with no reattach to replay them (`pty-data-<id>` is
emit-once), which is a hole in a terminal's scrollback; phase dots, workspace and chat
state all survive.

**`shell_snapshot`** (`scopeOrchRead`) is the first paint in one round trip: workspaces,
tabs for **every** workspace (a phone opens on one the desktop never mounted), all phases,
chats, and the `seq` that state is current as of. It reads `seq` **first, before any
data** — taken afterwards, an event landing between the data read and the seq read would be
numbered as already seen and lost for good; taken first, the worst case is the client sees
something twice, which its reducers tolerate. Every collection marshals empty rather than
null, because a client indexes them without a guard. Deliberately **not** in the snapshot:
chat transcript bodies and PTY scrollback, both of which already have the replay paths
named above — a second one for the same data is worse than none.

Client side, `src/runtime/transport.ts` tracks the position, sends `resume` in `onopen`
before flushing queued calls, drops any event at or behind where it already is (a
reconnect delivers some events both live and in the deltas, and these handlers fire sounds
and OS notifications), and exposes `onResync(cb)` plus `noteSeq(seq)` — the caller has to
report a snapshot's `seq` itself, since the snapshot is one opaque RPC from the
transport's point of view. `src/runtime/shellSnapshot.ts` is the read model and
`applyShellEvent` its reducer; unknown event names are ignored on purpose, since the
stream carries every bus event and this model tracks part of it.

**Binary PTY frames are deferred, on purpose.** Spec §2 wants them and `pty-data` still
travels as JSON. It is a bandwidth optimization, not a prerequisite for anything above.

### Pairing and device tokens (`src-wails/remoteauth.go` + `remotedevices.go` + `remoteguard.go`)

A phone gets in through three hops, and the middle one is the whole point:

```
POST /v2/pair       {code, name, kind}      → {device_token, environment_id, scopes}
POST /v2/ws-ticket  Authorization: Bearer   → {ticket}
GET  /v2/ws?ticket=…
```

A device token is long-lived, so it **never travels in a URL** — proxy logs, browser
history, `tailscale serve` diagnostics all keep those. Browsers cannot set headers on a
WebSocket handshake, so the handshake credential *has* to be in the query string; the
answer is that it is a **different** credential — single-use, at most 30 s old, worthless
by the time it reaches a log. That is why `/v2/ws-ticket` exists as its own hop, and a
test refuses a device token both as a query parameter and as a ticket. (The old `/ws` took
its token exactly the way this refuses to.)

`/v2/pair` is unauthenticated by necessity — pairing without a credential is what pairing
*is*. What keeps it honest: six random digits, a **3-minute TTL**, single use (a success
rotates the code), and a **five-guess lockout** that holds even against someone who has
since learned the code. An expired or locked code is reported to Settings as *absent*
rather than as digits that will not work.

**One row per device** (`remote_devices`), each with its own token, so revoking one leaves
the others paired — which the shared `http.token` could never do. The DB stores only the
token's SHA-256: not a defence against someone already reading this disk (`control.token`
sits next to it in plaintext) but a defence against a usable token leaving in a backup, a
sync folder or an error dump, for four lines. There is no way to read a token back after
pairing. Scopes live per row, so narrowing one device later is an `UPDATE`. **A revoke
closes that device's live sockets** (`remoteWS.dropDevice`, keyed by the `deviceID` the
ticket carries) — a revoke that leaves yesterday's socket running is not a revoke, and
that socket is the whole app.

**Mounting `/v2/ws` on the tailnet listener is what makes any of this reach the phone.**
It was on the hook server's loopback mux only, which the tailnet cannot see. The tailnet
server mounts the app's **own** `remoteWS` and ticket store — a second store would mean a
ticket minted by `/v2/ws-ticket` is unknown to the handler that redeems it, and a second
`remoteWS` would keep its connections in a registry `RevokeRemoteDevice` never looks at.

**Two fail-closed startup guards** (`remoteguard.go`, both pure functions so the invariants
are testable without a tailnet). `tailscale funnel` publishes *this* handler — same host,
same `:443`, same `/burrow` path — on the open internet, where a six-digit code is not a
defence, so remote access **refuses to start** while funnel is on anywhere on the node.
Measured against the real `tailscale serve status --json`: with funnel off the
`AllowFunnel` key is **absent, not false**, so a check written against `== false` would
read "off" as "on"; an unreadable config counts as **on**, because we cannot claim the
handler is private if we cannot read the config that decides it. And the listener binds
loopback only — not only for auth: plain HTTP on a private IP is not a secure context, so
a browser reaching us that way gets no service worker, no installable PWA and later no
push. A wildcard bind (`":37892"`) is the mistake the assert exists to catch, not a
default. `SetHttpEnabled` does not write the pref file when a start was refused, and
Settings now shows the refusal instead of leaving the switch looking on with nothing
listening.

**Scopes are not a containment boundary, and phase 5 answered that rather than building
one.** A paired device is the owner's own phone and holds authority over this machine by
design — the PWA's job includes driving a terminal and answering an agent's y/n prompt, and
a client that can do that can do anything (t3code ships the same model). A `Scope` is a
record of which door a call came through plus the forcing function that makes a *new* verb
decide whether it joins the network surface at all; `access:write` (pairing bootstrap) and
`ui:ack` (the desktop UI's identity claim) are still withheld from a paired device, not
because it could not reach them by spawning a shell, but because the point is not to hand
out the names. The real boundary is **paired or not paired** — which is where all the work
above went. `remoteapi.go`'s LOAD-BEARING NOTE keeps the list of what a genuinely *limited*
device role would need (an `fs.go` path guard, per-connection event filtering, an exec
admission check) for whoever wants to let in a device that is not the owner's.

### The phone (`src/mobile/` + `src/runtime/remoteEndpoint.ts`)

**The PWA is a client of the same socket, the same command table and the same event names
as the desktop.** The only thing that differs is where the ticket comes from: the desktop
calls `LocalEndpoint()` because being in-process *is* its authorization; the phone trades
its stored device token for a ticket at `/v2/ws-ticket`. `core.ts`'s `activeTransport()`
picks between them — Wails runtime → desktop, stored credentials → remote, neither → a
`no endpoint` throw, which is what an unpaired phone hits when `@/lib/config` invokes
`read_config` at module scope and what `config.ts`'s catch is written for.

Because `invoke` is transport-agnostic, adding the phone added **no** second data path and
no new table entry. `commandSurface.test.ts` therefore no longer skips `src/mobile`: a
wire name the phone calls and `remoteAllowed` forgot now fails in CI rather than on a
train.

Credentials live in `localStorage`, not in `@/lib/config`, on purpose: config reads
through `invoke`, `invoke` needs a transport, the transport needs the credentials — via
config that is a cycle that deadlocks on first load. A **401 from `/v2/ws-ticket` clears
them** and throws `RevokedError`, so a revoked phone lands back on the pairing screen
instead of retrying a dead token behind a spinner; a 500 does not, because a restart
mid-request is not a revocation.

What `src/mobile/store.ts` no longer contains, because the shared runtime does it:
- **its own websocket client** (`api.ts`, deleted) and its own reconnect loop with its own
  backoff and generation guard;
- **its own status derivation.** Terminal dots come from `phase-pty:{id}` through
  `displayStatus`, with a per-**device** read receipt under `burrow.seenAt.mobile` — a
  separate key from the desktop's on purpose, since `review` is a receipt precisely
  because the desktop may be staring at a tab the phone has never opened;
- **an N+1 first paint.** One `shell_snapshot` replaced `list_workspaces` plus one
  `list_terminal_tabs` per workspace, each round trip paying a phone's latency before
  anything rendered. Its `seq` is reported with `noteSeq`, so a reconnect resumes from it;
  a `resync` retakes the snapshot, which is the half of resume the client never had.
- `transport.onState` is what the dashboard reads, so it says "Odpojeno" over a dead
  socket rather than "Připojeno" over a stale screen. The socket is the only thing that
  knows; without this the store would have opened a second one just to observe the first.

What stays mobile-specific: the view stack, the `WorkspaceGroup` shape its views are
written against, and the **chat permission channel**, which is still raw
(`claude-data-*` / `acp-req-*`) because the control/permission protocol is deliberately
not part of the neutral event vocabulary. Chat status still comes from
`busy`/`pendingPermission` rather than `phase-chat:`, which has no consumer on either
client yet. `store.ts` was **not** deleted and mobile was **not** moved onto the desktop's
Pinia stores (spec §5 asks for both): that is a refactor with no gain in capability, and
the desktop stores have a different shape and a different lifetime (`terminalTabs` is a
mirror whose truth is a mounted `Terminal.vue`).

`DiffView.vue` completes the PWA v1 surface — read what an agent changed, per workspace,
read-only. No staging or committing: a destructive git action behind a mis-tap on a phone
is a bad trade. Untracked files diff against `/dev/null`, because a plain `git diff` prints
nothing for them and "nothing" reads as "no changes" for a file that is entirely new.

**The v1 surface is gone** (phase 6, once a client existed that did not need it): `/ws` and
its hand-written `dispatch`, `/rpc/`, `/pair`, `Broadcast`, `installWSSink`, and the shared
`http.token` — which is **deleted from disk at startup** rather than migrated, because a
token with no scopes, no device identity and no revocation cannot be translated honestly
into a scoped per-device session. Devices paired against it pair again. The listener now
serves the `/v2` surface, `/healthz` and the embedded bundle, and `TestV1SurfaceIsGone`
requires a **404** on the old paths — a 401 would mean the handler is still mounted and
merely refusing this caller.

**No manual GUI verification of any of this has been done** — nobody in this process could
launch the app. The load-bearing things to try first: pair a phone and watch the device
appear in Settings; kill the socket mid-turn and check the dots catch up rather than
freeze; revoke the device and confirm the phone drops to the pairing screen; heavy terminal
output under backpressure; and pressing ESC mid-turn.

### Manager (`src/components/ManagerPanel.vue`)

A per-repository orchestrator chat living in the right panel. One thread per
**root repo** (climbs `parent_id`, so it survives hopping between a repo and its
worktrees), session flagged `control: true` so it stays out of the Sidebar's chat
list, kept mounted per engaged repo and toggled with `v-show` so a busy Manager
keeps streaming while the user looks elsewhere. Message stream, composer,
permission gates and model picker all come from `AgentChat` — the panel only owns
the thread lifecycle and the primer.

Its primer (`src/utils/managerPrimer.ts`) is **generated from the verb registry**
(`control_verbs`) plus the worktree-isolation toggle and the project's
`.burrow/manager.md`. It tells the Manager to orchestrate and never implement,
and describes both doors (MCP tools if it has them, `burrow <verb>` otherwise) —
any configured agent can be the Manager, so the shell is the common denominator.

**Agent docs install** (`agentdocs.go`, at startup): teaches every agent the CLI.
Claude/Copilot get the `burrow` skill (`agentdocs/skills/burrow/SKILL.md`) plus an
always-in-context rule in `~/.claude/CLAUDE.md` (so Claude reaches for
`burrow spawn` before its own `Agent` tool); Codex gets the same content as a
managed `<!-- BURROW:BEGIN/END -->` block in `~/.codex/AGENTS.md`.
### Backend (`src-wails/*.go`, bound as `App` methods)

Go/Wails methods on `App` replace the old Tauri commands, one file per subsystem:
- **PTY management** (`app.go`) — `CreatePty`, `WritePty`, `ResizePty`, `KillPty`, `ListPtySessions`
- **SQLite** (`db.go`) — `workspaces` and `terminal_tabs` tables; DB lives in `<app-data>/workspaces.db`, opened with `journal_mode(WAL)` + `busy_timeout(5000)` because the chat-stream writer appends from its own goroutine
- **Chat list** (`chats.go`) — the `chats` table, `ListChats`/`CreateChat`/`SaveChats`/`DeleteChat`, the `chats-changed` event, and the one-time migration out of `config.json`. See "The chat list is shared state" below
- **Chat transcripts** (`chatstore.go`) — `chat_messages`, `SaveChatMessages(chatID, json, foldedOrd)` / `LoadChatMessages`
- **Chat stream log** (`chatstream.go`) — append-only `chat_stream` + `chat_stream_state`; `emitChatLine` is the single door for agent output (persist, then emit), used by both `claudechat.go` and `acp.go`
- **Git** (`git.go`) — `RunGit` wraps the system git binary (checks known paths)
- **Text generation** (`textgen.go`) — `GenerateCommitMessage`, `GeneratePrContent`, `GenerateBranchName`, `GenerateChatTitle` (see below)
- **FS** (`fs.go`) — `ReadDirShallow`, `WriteTextFile`
- **Event bus** (`bus.go`) — `busEmit(name, payload)` is the single door for **every** event a client may care about (`emitAll` is gone); `busSubscribe` registers a sink. `busEmit` **numbers** the event into the replay ring first (unless `notRingable` excludes it) and hands every sink the same `shellEvent{seq,name,payload}` — one struct rather than a growing parameter list, and the same `seq` for all sinks so the ring's order is the order clients receive. There is exactly **one** subscriber: `/v2/ws`'s per-connection subscription (`remotews.go`) — so `phase-pty:{id}`/`phase-chat:{id}` (and every other bus event) reach a connected phone and the desktop's own socket by the same path. The v1 tailnet broadcaster went away in phase 6 with the client that needed it; `busEmit` being the single *door* is about where events are published, not about how many things happen to listen. There is deliberately **no** bus → Wails-runtime sink: the desktop reads the bus through its own `/v2/ws` connection like any other client, and the only names still delivered on the Wails event channel (`menu-*`, `lsp-msg-*`, `float-*`, `extension-task:*`, `update:*`) are emitted with `runtime.EventsEmit` directly, never through `busEmit` (`src/lib/wailsCompat/event.ts` routes exactly those prefixes to `EventsOn`). `events_test.go` greps every non-test `.go` file for a direct `EventsEmit(` call and fails unless the file is on an explicit allowlist (menu items, updater progress, LSP messages — genuinely desktop-only), so a new event can't quietly skip remote clients the way `emitWorkspacesChanged` once did
- **Agent phase** (`phasestore.go`, `phasepoll.go`, `internal/agentphase/phase.go`) — see "PTY / Agent phase" above
- **Environment identity** (`environment.go`) — `environmentID()` creates and persists a random id in `<app-data>/environment.json` on first run; every client-side record (known environments, endpoint preferences, seen-at receipts) is meant to key off this rather than IP/hostname, which change. `EnvironmentID()` is reachable over `/v2/ws` as `environment_id` (`scopeOrchRead`) — not a Wails binding call any more
- **Remote endpoints** (`endpoints.go`, `endpoints_tailscale.go`) — `EndpointProvider` registry (currently loopback + Tailscale) contributing `AdvertisedEndpoint`s, `selectEndpoint()` implementing t3code's selection order (preferred kind → hosted-HTTPS-compatible → default → non-loopback → loopback-if-same-machine), surfaced as `remote_endpoints` (`scopeAccessRead`) over `/v2/ws`. **Nothing consumes this yet** — it exists for Settings/pairing in a later phase
- **Remote command table** (`remoteapi.go`) — `remoteAllowed`/`remoteDenied`: every `App` method the desktop and a future remote client may reach over `/v2/ws`, with its wire name, positional argument names and required scope. See "Desktop transport" above
- **Remote wire protocol** (`remoteproto.go`) — the tagged `call`/`resume` client frames and `reply`/`event`/`shell`/`resync`/`welcome` server frames for `/v2/ws`
- **Remote socket server** (`remotews.go`) — `/v2/ws` itself: ticket issuance/redemption, per-connection outbound queue, per-call goroutine dispatch with a concurrency cap and panic recovery, `resume` handling, keepalive
- **Shell event stream** (`shellstream.go`) — the `seq` counter and the 512-event replay ring; `resumeSince` is a pure function over it, so there is no per-client state to keep. See "The shell stream" above
- **First paint** (`shellsnapshot.go`) — `ShellSnapshot()`: workspaces, tabs for every workspace, phases, chats and the `seq` they are current as of, in one round trip (`shell_snapshot`, `scopeOrchRead`)
- **Pairing** (`remoteauth.go`) — `/v2/pair` and `/v2/ws-ticket`, the pairing code's TTL/budget/rotation, and `RemotePairStatus`/`RemoteRegeneratePairCode` for Settings. See "Pairing and device tokens" above
- **Paired devices** (`remotedevices.go`) — the `remote_devices` table, tokens hashed at rest, `pairDevice`/`deviceForToken`/`RemoteDevices`/`RevokeRemoteDevice`
- **Startup guards** (`remoteguard.go`) — `funnelEnabledIn` and `assertLoopbackAddr`, both fail-closed and both pure so the invariants are testable without a tailnet

### Background text generation (`src-wails/textgen.go`)

The small writing jobs the app does *for* the user — commit messages, PR title
and body, worktree branch names, chat titles — are one-shot non-interactive CLI
calls: no PTY, no session, prompt over stdin, a JSON schema on the way out.
Ported from t3code's `apps/server/src/textGeneration/*`, so the prompts, the
per-section truncation (`limitSection`) and the sanitizers are theirs; what
differs is that they resolve a provider instance through an Effect registry
while we switch on one persisted selection string.

**One preference drives all four.** `ui.textGenerationModel` is
`"kind::provider::model::effort"` (effort optional — every earlier shape,
including a bare Claude model id, still parses). `ui.textGenerationPolicy` is
t3code's `TextGenerationPolicyKind`: `default` · `conventional_commits` ·
`repo_conventions` (the last one shows the model `git log -20 --format=%s`, which
is what makes their "follow the repo's style **when examples are available**"
preset actually have examples). Both live in Settings → General; **`src/stores/git.ts`
reads them itself** (`textGenPrefs()`) rather than having each call site pass
them, because the model *was* an argument and every new generated-text feature
forgot the policy the moment it existed.

**Per-provider CLI contracts** (`generateTextJSONContext`), 180 s budget each
(t3code's `CLAUDE_TIMEOUT_MS`/`CODEX_TIMEOUT_MS`):
- **Claude** — `-p --output-format json --json-schema <inline> [--model] [--effort]`, answer read from the envelope's `structured_output`. `claudeCliEffort` mirrors their `normalizeClaudeCliEffort`: `ultracode`→`xhigh` (it is a settings flag), `ultrathink` dropped (it is a prompt-prefix mode) — neither is a `--effort` value.
- **Codex** — `exec --ephemeral --skip-git-repo-check -s read-only --config model_reasoning_effort="…" --output-schema <file> --output-last-message <file> -`. **`--output-schema` is what makes Codex answer in JSON at all**; without it it replies in prose as often as not, and scraping an object out of that silently dropped whole generations. Effort defaults to `low` (their `CODEX_GIT_TEXT_GENERATION_REASONING_EFFORT`) rather than whatever the user's `config.toml` says.
- **Gemini / OpenCode** — prose-tolerant: `extractGeneratedJSON` digs the object out of a fenced or prefixed answer, and a bare title still reaches a caller that can use one.

Every generated string passes a sanitizer before it reaches git or the UI
(`sanitizeCommitSubject`, `sanitizePrTitle`, `sanitizeChatTitle`,
`sanitizeBranchFragment`) — "the model followed the rules" is not something to
rely on when the output goes straight into `git worktree add -b`. The two
best-effort generators (branch name, chat title) return `""` on any failure so
the caller keeps the name it already showed; `gh pr create` falls back to
`--fill` the same way.

### OSC escape sequence protocol

| Sequence | Direction | Meaning |
|----------|-----------|---------|
| `\x1b]9998;running\x07` | PTY → app | Claude hook: processing user prompt |
| `\x1b]9998;waiting\x07` | PTY → app | Claude hook: waiting for user input |
| `\x1b]9998;done\x07` | PTY → app | Claude hook: turn complete |

OSC 9998 status writes go to `/dev/tty` with `2>/dev/null || true` (tolerated when no tty; status then falls back to `get_pty_foreground` polling). **No `burrow` subcommand uses OSC**: app actions go over the loopback control API, and result capture exchanges files in `BURROW_SESSION_DIR` (`<token>.result`/`.done`), because agent subprocesses have no controlling tty. `XTerm.vue` retains a latent `OSC 9999;spawn` parser but nothing emits it.

## Auto-update

Self-updater in Go (`src-wails/updater.go`). Manifest layout: a `latest.json` on **GitHub Releases** at `JBurdik/burrow-wails` (public), fetched from `https://github.com/JBurdik/burrow-wails/releases/latest/download/latest.json` — GitHub's `latest/download` alias always resolves to the newest release, so the endpoint never changes per version. `updateRepo` in `updater.go` and `repo :=` in the `justfile` must stay in sync.

**Go bindings:** `CheckUpdate()` → `UpdateInfo{available, version, current_version, notes, url, sha256}`; `InstallUpdate(url, sha256)` downloads → verifies → swaps the running `.app` → returns; `RelaunchApp()` re-`open`s the bundle and quits. Download progress is emitted on the `update:progress` event as `{received,total}`.

**Verification (two mandatory gates, both hard-fail):** the download's sha256 must match the digest in the HTTPS-fetched manifest, **and** the extracted bundle must be codesigned by team `9QY36KZ8JP` (`codesign --verify --strict --deep` + `TeamIdentifier=` check). No separate ed25519/minisign keypair — the Apple Developer ID signature the release build already carries *is* the trust anchor. Extraction shells out to `/usr/bin/tar` (preserves the symlinks/xattrs a bundle signature depends on).

**Frontend**: `src/stores/update.ts` + `UpdateBanner.vue` import `@tauri-apps/plugin-updater` / `-process` as their API surface, but Vite aliases both to shims in `src/lib/wailsCompat/` (`updater.ts` / `process.ts`) that call the Go bindings above and translate the `update:progress` event into the `Started`/`Progress`/`Finished` callback shape those imports expect — so the Vue components themselves never talk to Tauri, only to the compat shim.

**Releasing (`just release [patch|minor|major]`, default patch):** `just bump` lifts the version in lockstep across `src-wails/wails.json` (`info.productVersion`, the single source of truth), `package.json` and `src-wails/version.go` (`appVersion`); then `build` (frontend → `wails build -s` → daemon binary into the bundle) → `sign` (Developer ID, hardened runtime, `src-wails/build/darwin/entitlements.plist`; inner binaries first, then the bundle) → `notarize-app` (zip → notarytool → staple) → `dmg` (hdiutil → notarize → staple) → `pack` (`Burrow.app.tar.gz` + `latest.json` with the sha256) → commit bump → tag `vX.Y.Z` → push → `gh release create` with dmg + tarball + `latest.json`. `just verify` runs the full codesign/Gatekeeper/staple audit. Keychain creds: `BURROW_NOTARY_PWD` + the `BURROW_NOTARY` notarytool profile (`just notary-creds '<app-specific-password>'`).

## Documentation (`docs/`)

Standalone HTML reference pages (no build step — open directly in a browser). Keep these in sync when you change the corresponding code:

| File | Covers | Update when |
|------|--------|-------------|
| `docs/context.html` | Whole-project overview: architecture, features, key files, Go/Wails bindings, shortcuts | Adding/removing a component, store, Go binding, agent, or shortcut |
| `docs/burrow.html` | The control API + `burrow` CLI: verb registry, transports, spawn/supervise/collect, result capture, agent-docs install | Adding or changing a verb, the `burrow` script, or `installAgentDocs` |
| `docs/superset-concept/index.html` | Concept study: how superset-sh/superset detects terminal/agent status (HTTP lifecycle hooks vs Burrow's OSC 9998 channel) | Reference only — reverse-engineered comparison, update if porting the hook model into Burrow |

`assets/` holds logos (`logo.png`, `burrowlogo-CUTOUT.png`). `index.html` is the **Vite app entry**, not documentation — do not treat it as a docs page.

## Vocabulary
- MC = mission control

## Plans (`docs/plans/`)

Feature plans and implementation notes live in `docs/plans/`. Read the relevant plan before starting a feature batch. Current plans:

| File | Covers |
|------|--------|
| `docs/plans/burrow-features-2026-06-02.md` | Status dots bug, tab reorder, Ctrl+1-9 tabs, ⌘1-9 workspace switch, project icons, git branch in title bar |

<!-- gitnexus:start -->
# GitNexus — Code Intelligence

This project is indexed by GitNexus as **burrow-wails** (6314 symbols, 11895 relationships, 300 execution flows). Use the GitNexus MCP tools to understand code, assess impact, and navigate safely.

> If any GitNexus tool warns the index is stale, run `npx gitnexus analyze` in terminal first.

## Always Do

- **MUST run impact analysis before editing any symbol.** Before modifying a function, class, or method, run `gitnexus_impact({target: "symbolName", direction: "upstream"})` and report the blast radius (direct callers, affected processes, risk level) to the user.
- **MUST run `gitnexus_detect_changes()` before committing** to verify your changes only affect expected symbols and execution flows.
- **MUST warn the user** if impact analysis returns HIGH or CRITICAL risk before proceeding with edits.
- When exploring unfamiliar code, use `gitnexus_query({query: "concept"})` to find execution flows instead of grepping. It returns process-grouped results ranked by relevance.
- When you need full context on a specific symbol — callers, callees, which execution flows it participates in — use `gitnexus_context({name: "symbolName"})`.

## Never Do

- NEVER edit a function, class, or method without first running `gitnexus_impact` on it.
- NEVER ignore HIGH or CRITICAL risk warnings from impact analysis.
- NEVER rename symbols with find-and-replace — use `gitnexus_rename` which understands the call graph.
- NEVER commit changes without running `gitnexus_detect_changes()` to check affected scope.

## Resources

| Resource | Use for |
|----------|---------|
| `gitnexus://repo/burrow-wails/context` | Codebase overview, check index freshness |
| `gitnexus://repo/burrow-wails/clusters` | All functional areas |
| `gitnexus://repo/burrow-wails/processes` | All execution flows |
| `gitnexus://repo/burrow-wails/process/{name}` | Step-by-step execution trace |

## CLI

| Task | Read this skill file |
|------|---------------------|
| Understand architecture / "How does X work?" | `.claude/skills/gitnexus/gitnexus-exploring/SKILL.md` |
| Blast radius / "What breaks if I change X?" | `.claude/skills/gitnexus/gitnexus-impact-analysis/SKILL.md` |
| Trace bugs / "Why is X failing?" | `.claude/skills/gitnexus/gitnexus-debugging/SKILL.md` |
| Rename / extract / split / refactor | `.claude/skills/gitnexus/gitnexus-refactoring/SKILL.md` |
| Tools, resources, schema reference | `.claude/skills/gitnexus/gitnexus-guide/SKILL.md` |
| Index, status, clean, wiki CLI commands | `.claude/skills/gitnexus/gitnexus-cli/SKILL.md` |

<!-- gitnexus:end -->
