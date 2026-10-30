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

Tests: `pnpm test` (vitest, no DOM env). `just` (Justfile task runner, `brew install just`) drives dev/build/release; see `justfile`.

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

> **Full rationale for every subsystem below lives in `docs/architecture.md`** — why each thing is
> built this way and which bug it prevents. Read the matching section there before changing one.

### The chat list is shared state (`src-wails/chats.go`)

Chats are a SQLite table Go owns (`chats`), not a blob in `config.json` — two writers
(desktop + `RemoteCreateChat`) on one whole-file-overwrite blob lost creations and reverted
`chatIdCounter`. Load-bearing: **`INTEGER PRIMARY KEY AUTOINCREMENT`** (ids are referenced by
`chat_stream`, `chat_messages`, `pty_phase` — recycled rowids would hand a new chat a dead one's
transcript); **`SaveChats` upserts and never deletes** (a stale client can't remove a row it never
heard of; removal is explicit `DeleteChat`); `chats-changed` event. No `busy`/`status` columns — the
status **is** the phase (`pty_phase`, key `chat:<id>`).

`parent_chat_id` makes a sub-agent belong to a thread: Sidebar filters on `!parentChatId`, RP's
Sub-agents surface uses `childrenOf(sessions, activeChatId)`, `DeleteChat` cascades to children.
Depth capped at **one level**, derived server-side (`chatIsSubagent`), never trusted from the request.
A settled, seen, **collected** (`collected_at`, read-only on `Chat`) sub-agent is auto-archived after
30 min by `chatAttention`'s desktop sweep (`subagentArchivable` in `chatTree.ts`) — client-side because
`seenAt` is per-device. No Archived shelf for sub-agents: it just leaves the RP list.

**Snooze** (`chatsnooze.go`): `chats.snoozed_until` (unix ms, 0 = none, read-only on the wire) keeps a chat
in Settled until then (`settledFor`); `ChatSnooze` / `chat_snooze` sets or clears it, and `chatScheduleLoop`'s
sweep wakes it — clears the column and the settle pin, bumps `last_activity_at`, emits `chat-snooze-woke`
(`chatAttention` drops the receipt and notifies). Presets: `src/lib/snoozePresets.ts`.

**Linked PR** (`chatpr.go`): `chats.pr_number` / `pr_url` / `pr_state` (open|draft|merged|closed, read-only on the
wire; the forge is the workspace's). `forgePrCreate` auto-links a created PR to the calling chat (`pr_create` verb →
`callerChat`) else to every unarchived chat on that workspace whose `branch` is the head or empty; `chat_link_pr`
(`#N` / URL, refused if the URL is another repo's) / `chat_unlink_pr` from ThreadDetailsCard's PR row. Unlink writes a
`chat_pr_dismissed` tombstone that auto-link respects and a manual link clears. No polling: `chat_pr_refresh` when the
card opens, `chat_prs_refresh(cwd)` after a push from `git.push`; Sidebar rows show the state glyph.

**Launch settings** (`effort`, `permission_mode`, `profile_id`, `append_system_prompt`, + `model`) are
columns too, **read-only on the wire** (SaveChats never writes them, like `collected_at`); backfilled once
from the per-device `config.json` keys (`chat_migrations` marker). `resolveClaudeLaunch` (`chatlaunch.go`)
resolves them + profile (`providers`) + project `claude_config_dir` into Claude start params at every start
(plan: `docs/plans/006-chat-runtime.md`).

### Chat runtime (`src-wails/chatruntime.go`, `chatspawn.go`, `src/lib/chatRuntime.ts`)

Every chat's process is Go's, keyed by chat id — **all three transports** (claude-cli, Codex app-server,
generic ACP); no mounted view needed (`spawn`, the phone, `chat_send` all work with the desktop closed).
`chat_send` **ensures the process itself** (start from the row + resume `claude_session_id` — Go captures it
from Claude `system/init` and from the ACP/Codex handshake; covers a send after `ReapIdle`), then sends; a
per-chat lock makes concurrent ensures one start. Codex/ACP resolve the row's `agent_kind` as a provider
instance (`resolveRPCLaunch`: binary, `transportArgs`, env, project `env_file` / `claude_config_dir`) and
then push the row's model / effort / permission mode into the fresh session — the **mode fails closed**
(Codex starts every thread in Auto). `chat_restart` / `chat_interrupt` (Claude
`interrupt` control request; Codex `turn/interrupt` + a 5 s still-running check; ACP and every refusal →
restart) / `chat_clear` (wipe `chat_messages` + `chat_stream` +
session id) all publish **`session.reset`** (stream kind `chat-runtime`, `{cleared}` for clear); the
`chatSession` reducer — same branch as `session.exited` — drops every pending request, closes the partial,
clears busy. **Steer** (`chat_steer`, `chatsteer.go`): a queued follow-up delivered INTO the running turn —
Claude = a plain stdin user line (the CLI picks it up at the next tool boundary, same turn, one `result`),
Codex = `turn/steer {expectedTurnId}`; generic ACP refuses. Only a `running` phase is steerable (a user line
would repaint a pending approval as running); a refusal leaves the message queued. **Side chat** (`chat_side_create`, `chatside.go`): `kind='side'`
child that **forks the parent's current session** on first start (Claude `--fork-session`, `resume_at="@latest"`;
Codex `thread/fork`; ACP refused) — parent untouched, not a sub-agent (`collect_results` / Sub-agents skip it).
**Review** (`review_changes`, `review.go`): new chat reviewing the workspace diff — Codex native `review/start`, else a
prompt. **Recap** (`chatrecap.go`): one-line `chats.recap` (read-only on the wire) via textgen after a live completed
turn, ≤ 1 per 2 min, Settings → General. **Transcript notes** (`publishSurfaceNote`): Go-written `chat-note` rows with a
`surface` (plan, changed files, Claude background tasks) that open the RP surface; live only — replays never derive
them again. Full rationale: `docs/architecture.md` → "Steer, side chats, review, recaps, transcript notes". **Rewind / fork** (`chatrewind.go`, Claude only — Codex/ACP refuse and the UI hides them):
`chat_rewind(id, index, text)` cuts the transcript before a user message (folded_ord jumps past every recorded
line, ords keep counting) and `chat_fork` copies it through that turn into a new row with the parent's session;
both park the cut's Claude record uuid (`ChatMessage.uuid`, stamped by the fold) in `chats.resume_at`, and the
next start adds the hidden `--resume-session-at <uuid> --fork-session` — the resumed session file is never
modified. The forked session's id (system/init) clears `resume_at`. `chat_set_launch_settings(id, patch)` writes the row and applies live — Claude via confirmed
`set_model` / `set_permission_mode` control requests, Codex/ACP via `AcpSetConfig` / `AcpSetMode` — else
restarts (`row_only` = record only). Process starters: `chatProcs` (`agentproc.Manager` / test fake) and
`App.rpcStarter` (nil = real `CodexStart`/`AcpStart`; tests register a session over a buffer).
`claude_start`/`claude_send`/`acp_start`/`codex_start`/`acp_send`/`codex_send`/`codex_interrupt` are in
`remoteDenied`; `remote_create_chat` only creates the row (Claude and Codex). **An ACP/Codex turn ends on a
Go-emitted `{"_burrow":"turn"[,"error"]}` marker** (→ `turn.completed` / `turn.failed`), written after the
response to the runtime's own `session/prompt` or Codex's terminal turn notification — the rpc id never
leaves Go, so there is no client-side prompt correlation any more.

**Auto-sleep** (`chatsleep.go`): a sweep (1 min) stops a live chat process whose phase is settled
(idle/done/failed/stale, no open ACP turn) and that had no activity for `uiPrefs.chatAutoSleepMinutes`
(Settings → General, default 60, 0 = off; read fresh from `config.json`). Activity = a send, a start,
any streamed line, or `chat_mark_active` — `chatAttention` pings it every minute for each chat on screen
while the window has focus, which is how "never sleep a watched chat" reaches Go. The row gets
`slept_at` (read-only on the wire; moon in Sidebar + composer); the next `chat_send` resumes and clears
it — opening a chat does not wake it. A **manual** settle (`settle()` in `claudeChats.ts` → `chat_sleep`) sleeps the chat immediately through the same `sleepChat` (same phase/open-turn guards, no idle window; unsettle does not wake it). It replaced `agentproc.ReapIdle`, which reaped on stdout silence
and so killed a CLI blocked on a permission prompt. PTY agent tabs are **not** slept: killing one loses the
TUI, and restoring it means relaunching `claude --resume <id>` into the same tab (not built).

**Sub-agent notices** (`chatsubagentnotice.go`): a sub-agent (`parent_chat_id`, not `kind='side'`) whose
turn ends (done/failed) or that enters `waiting_*` gets a row in `subagent_notices`, keyed per (child, turn /
blocked episode) — recorded in `emitChatLine` on the phase transition, delivered by the 15 s schedule sweep
(ahead of `chat_schedules`) into the **parent** via `ChatSend` only once the parent `chatSettled`. A completion
marks the child collected unless cut at 4000 chars. The user's queued follow-ups are client-side, so a drained one
can run first. Off with `uiPrefs.subagentAutoDeliver = false` (Settings → General).

**Self-management verbs** (`internal/control/verbs_self.go`, local-only, default to the caller's chat):
`schedule` / `schedule_list` / `schedule_cancel` (`chatschedule.go`) — SQLite `chat_schedules`, a 15 s sweep
delivers due items via `ChatSend` **only once the chat has settled** (`chatSettled`: phase not in flight, no
open Codex/ACP turn) — `ChatSend` into a running turn is a steer, not a queue — one per chat per sweep, 5
retries; `DeleteChat` drops a chat's items. `move_to_worktree` / `leave_worktree` (`chatmove.go`) rewrite
`chats.workspace_id`/`branch` — **read-only to `SaveChats`** since then (a client saves per streamed message
and would move it back) — and flag `chatRT.moved`; the next ensure **after the turn settles**
(`restartIfMoved`) stops the old-cwd process so the start resumes in the new cwd. Claude's session file is
symlinked into `<config>/projects/<new-cwd-slug>/` (2.1.311 resumes by id from any cwd and appends to the
original; older CLIs / another `CLAUDE_CONFIG_DIR` look only there). Codex resumes with the new cwd; generic
ACP refuses.

**`spawn` of a thread's sub-agent runs in Go** (`spawnSubagent`, `control.Deps.ChatRuntime`): profile
(defaults duplicated from `subagentProfiles.ts`, test-guarded) → agent (arg / profile / parent's / default
chat agent / first enabled) → row with launch settings (arg / profile / the parent's **when it runs on the
same transport**) → `ChatSend(task)`, every transport. The parent's transcript row is Go's: a `chat-note`
row (`subagentChatId`/`subagentAgent`) in the **parent's** `chat_stream` (`publishSubagentRow`), projected as
`message.note`. The Right Panel's button reaches the same code via `spawn_subagent_chat`. Top-level chat and
tab spawns stay UI. There is no hidden host mounting sub-agents any more: a child's view mounts only when
the Right Panel shows it, and a permission request from a chat with **no session in the registry** is
toasted by `chatAttention` off its `waiting_*` phase.

Per-device and still in `config.json`: `chatActiveByWs`, `burrow.seenAt`, `chatTurns`,
`chatPermissionRules`. Phone-created chats are titled `Chat N (phone)`.

### Chat stream ownership (`src/lib/chatSession.ts` + `src-wails/chatstream.go`)

A chat's stream is owned by a **session registry keyed by chat id**, not by `AgentChat.vue`: the
session holds transcript, turn state, blocking requests and the `claude-data-{id}` / `acp-data-{id}` /
`acp-req-{id}` listeners, **and reduces the stream itself** (`onEvents`/`onLine`/`onAcpData`/`onAcpReq`,
turn end, queue drain, notifications via `ChatHost` in `src/lib/chatHost.ts`). A component
`attachView(ChatViewHooks)`s on mount — scroll, question/plan draft resets, ACP selector restore, `send`
for the drain — and `release()`s on unmount; the last view's hooks stay installed. A session is only torn
down when **idle** (`pendingRequests` — a typed `PendingRequest` union — empty and not busy; every answer
goes through the one `respond(PendingAnswer)` door, which also closes a generic ACP prompt on write since
only Codex sends `serverRequest/resolved` — a running turn or pending permission keeps
streaming behind an unmounted view). That's what lets chat leaves render with `v-if`. **The interface is
closed**: `ChatSession` hands every ref back `Readonly` (writable `SessionState` is module-private) plus
intents — **`send` / `restart` / `interrupt` / `clear`** (forward to `chat_*`; the one door for AgentChat,
the queue drain via `ChatViewHooks.send`), `notice`, `suppressDoneToast`, `requestContextReport`,
`hydrate`, … — so nothing outside can break the idle invariant. The turn clock
(`turnStartedAt` → `turnMs`) is the session's too. Deps (`invoke`, `listen`, host) are injectable:
`chatSession.test.ts` drives it **only** through that interface and fake listeners.

Every agent line is appended to SQLite `chat_stream(chat_id, ord, kind, line)` *before* it is emitted;
`chat_stream_state.folded_ord` records how far the frontend folded it into `chat_messages`, so a trim
can't delete an unrendered line. `replayChatStream()` catches a chat up after restart.

### Provider protocol is parsed in Go (`src-wails/providerruntime.go`)

Claude stream-json and ACP JSON-RPC are parsed in **one** place (Go) and re-emitted as neutral
`ProviderRuntimeEvent`s on `chat-event-{chatId}` (`{ord, events}`): `text.delta` · `thinking.delta` ·
`user.delta` · `tool.started` · `tool.completed` · `turn.completed` · `turn.failed` · `session.title` ·
`session.id` · `session.exited`. The frontend only renders (`src/lib/chatProjection.ts` → `ChatMessage[]`,
`AgentChat.onEvents` for scroll/notify/accounting); `LoadChatEventsSince` replays the log as events.

**Deliberately still on the raw channel** (UI decisions, not transcript): the control/permission
protocol, Claude `system/init`, the ACP handshake, selector replies, `serverRequest/resolved`.
`chat_messages` is still written by the frontend.

### PTY / Agent phase (`src-wails/internal/agentphase`, `phasestore.go`, `phasepoll.go`)

**The phase is derived in Go**, so it exists with no client attached (phone asleep, cold restart) —
a hard requirement of remote access. `XTerm.vue` has no status emits left; it only polls
`get_pty_foreground` every 2 s for auto-titling.

- `internal/agentphase/phase.go` — pure `Next(cur, ev, now) Phase`, no SQL/Wails/`main`. States:
  `idle | running | waiting_input | waiting_approval | done | failed | stale`. No `starting`, no
  `review`. `Phase` carries `detail`, `model`, `title`, `is_agent`, `turn_ended_at`, `updated_at`, and
  is a comparable struct on purpose (unchanged → no write, no emit).
- `phasestore.go` — one `Phase` per key (`pty:<id>` / `chat:<id>`, one type for both), persisted in
  `pty_phase`, emitted as `phase-pty:<id>` / `phase-chat:<id>`. Per-id monotonic `seq` guards upsert
  and emit against out-of-order writes; the whole publish tail runs under a dedicated `emitMu` (taken
  after `s.mu` is released) so the loser can't emit last. `CreatePty` `Forget`s a phase the daemon
  doesn't list — **PTY ids are reused**, so without that a fresh tab wore the old one's review dot.
  `PhaseStore.Apply` is the **only** writer of `terminal_tabs.status`.
- **Three inputs:** (1) persistent hooks — Claude's live in the burrow plugin's `hooks/hooks.json`
  (`hooks` key, PTY-scoped via `CLAUDE_CODE_PLUGIN_DIRS`, so chats and non-Burrow sessions never run
  them); `installStatusHooks` still merges a hook into Codex's global config non-destructively and
  writes Copilot's file, and for Claude only strips the entries older versions merged into
  `settings.json` (migration, keep ≥ 1 release); `burrow hook` maps `hook_event_name` → state and POSTs to the
  loopback hook server (port from `<BURROW_HOME_DIR>/hook.port`); `hookEvent()` applies the phase. The
  legacy `pty-hook-{id}` event is gone. (2) foreground poll (`phasepoll.go`, 2 s, server-side) — an
  agent being foreground is **never** busy; only the shell branch may clear `is_agent`; three empty
  reads + daemon not listing the PTY ⇒ `stale`. (3) the interrupt keystroke — `App.WritePty` applies
  `agentphase.Interrupt` on a payload that is exactly one byte of `0x03`/`0x1b` (length is what
  separates a cancel from an arrow key); guarded on `cur.InFlight()`. **Plus a fourth, claude-only
  source inside the poll** (`claudestatus.go`): when the foreground is `claude`, the poll reads Claude's
  own `<CLAUDE_CONFIG_DIR|~/.claude>/sessions/<pid>.json` (pid = foreground pgid, from the daemon's
  `foreground` reply `Pid`; `kind:"interactive"`, `pid` must match) and applies `busy`→`HookRunning`,
  `waiting`(+`waitingFor:"permission prompt"`)→`HookPermission`/else `HookWaiting`, `idle`/`shell`→
  `StatusIdle` (settles an *in-flight* turn to `done`, no-op otherwise). **On change only** (a per-pty
  baseline), so an unchanged file never overwrites a fresher hook. Absent/foreign/unknown file, pid 0
  (old daemon) or an old CLI ⇒ exactly the previous behaviour.
- **Chats feed the same store** via `chatPhaseEvent()` in `providerruntime.go`, applied in
  `emitChatLine`.

**The client owns the read receipt, not the phase.** `src/runtime/displayStatus.ts` is a pure
`displayStatus(phase, seenAt, watching) → TermStatus` (`idle|running|waiting|permission|done|review|error`)
plus `shouldMarkSeen()`; `review` and lime `done` are *not* phases. `Terminal.vue` persists `seenAt`
per leaf in `localStorage` under `burrow.seenAt` (read-modify-write, never wholesale) and listens on
`phase-pty:{leafId}`. `tabStatus()` priority (`terminalStatus.ts`): **error** > permission > waiting >
running > review > done > idle.

**Chats use the same derivation.** Go derives waiting/permission for every provider from neutral
`request.opened` / `request.resolved` events (Claude `control_request`, ACP `session/request_permission`,
Codex approvals/user input); an answer applies `agentphase.Resume`, an abort/stop `Interrupt` (and a
trailing turn end on an idle chat is dropped), a generic ACP turn settles on its own `session/prompt`
response. `src/stores/chatAttention.ts` (instantiated in `App.vue`) is the **only writer of
`session.status`**: `displayStatus(phase-chat, seenAt, watching)`, receipts in `burrow.seenAt` as
`chat:<id>`, watching = a count held by the mounted `AgentChat` / RP sub-agent detail while the window
has focus. There is no chat state machine any more. The phone does the same with its own receipt
(`burrow.chatSeenAt.mobile`). A permission request fires a toast + (unfocused) a native notification
via `notifyPermission()`. The Sidebar renders chats and terminal tabs as **one**
list, distinguished only by icon.

### Control API + `burrow` CLI (`src-wails/internal/control`, `src-wails/bin/burrow`)

**One implementation of every app action, three doors.** `internal/control` is a registry of verbs
(`spawn`, `agent_status`, `focus_tab`, `create_worktree`, `pr_merge`, …) that knows nothing about
HTTP/MCP/Wails — it takes capabilities as interfaces (`Deps`). Transports:

| Transport | Client | Auth | Verbs |
|-----------|--------|------|-------|
| loopback HTTP `POST /v1/<verb>` (hook server's port) | `burrow` CLI, `burrow-mcp` | `control.token` (0600), Bearer | all |
| tailnet HTTP (`httpserver.go`) | mobile / PWA | device token + pairing | `ScopeRemote` only |
| Wails bindings | desktop UI | in-process | as needed |

`Scope` is a field on the verb and a verb is **local-only unless it opts in**. The registry is also
the source of the MCP tool schemas (`/v1/_verbs`) and `burrow help`.

**UI-performed verbs** (open a tab, focus a workspace, read scrollback) call `UIBridge.Do`, which emits
`control:action` and **blocks for the frontend's ack** (`AckControlAction`, 15 s).
`src/lib/controlBridge.ts` is the single app-wide listener. This replaced the old polled
request-dir transport.

**The CLI** is a thin generic client — `burrow <verb> [POSITIONAL] [--arg value]`, kebab-case
normalised, `cwd` always riding along as `$BURROW_CWD`. Only `curl` + `sed`, so it works from Claude's
Bash tool and from hooks. Non-verb subcommands (status plumbing, deliberately independent of the
control API): `burrow status <state>` (**POSTs to `/hook`** — serving only `/status` silently killed
every status dot), `burrow hook`, `burrow approve`, `burrow notify`, `burrow capture <token>` (writes
`<session>/<token>.result` + `.done`, so results survive a restart).

**`burrow-mcp`** (`cmd/burrow-mcp`) is the same verbs as MCP tools — `/v1/_verbs` → JSON schemas,
`tools/call` → one POST; no DB, no logic. Injected via `burrowMcpServers` / `acpMcpServers`, skipped
silently when the sidecar isn't next to the executable.

`BURROW_*` in every PTY: `BURROW_SESSION_DIR`, `BURROW_CWD`, `BURROW_PTY_ID`, `BURROW_HOOK_PORT`,
`BURROW_HOME_DIR`.

### Desktop transport (`src/runtime/transport.ts` + `src-wails/remoteapi.go`/`remoteproto.go`/`remotews.go`)

The desktop's **entire** data path — PTY bytes, chat events, every SQLite-backed call — travels over
`ws://127.0.0.1:<hookPort>/v2/ws` on the hook server's always-on loopback mux. A phone connects to the
same surface at a different URL, so remote access can't drift from a separate desktop API.
`src/lib/wailsCompat/core.ts` is now a pass-through; what's left there is `CLIENT_SIDE_COMMANDS`
(`detach_pty`, `send_float_snapshot`, `notify_float_grid`, `open_git_panel_window`) plus a few cases
that reshape args.

- **`remoteapi.go`'s `remoteAllowed`** is the security boundary: wire name → `App` method + positional
  argument names + `remoteScope`. Argument names live in the table because they don't exist at runtime
  (Go reflection has none; Wails bindings are `arg1..argN`). Scope is enforced **per command**. Two
  tests guard both directions: `TestRemoteSurfaceIsExhaustive` (every `App` method is in
  `remoteAllowed` or `remoteDenied`) and `commandSurface.test.ts` (every literal `invoke("...")` under
  `src/`, including `src/mobile`, is in the table or `CLIENT_SIDE_COMMANDS`).
- **`remoteproto.go`** — four tags: `call`, `reply`, `event`, `welcome`. Event names on the wire are
  identical to the bus names (`pty-data-7`, `phase-pty:7`) so there's no translation table. A `call`
  with a non-positive id is rejected at decode (id `0` would vanish under `omitempty`).
- **`remotews.go`** — single-use 30 s tickets (browsers can't set headers on a WS handshake, so the
  credential in the query string has to be one that dies on first use); **one outbound queue + one
  writer goroutine** per connection (ordering, and never two writers); a full queue **drops the
  client** rather than blocking (`busEmit` runs under `emitMu`); each call on its own goroutine behind
  a **64-slot semaphore** with `recover` (one slow call used to head-of-line-block keystrokes);
  read limit, deadlines, ping/pong.
- **`App.LocalEndpoint()`** is the **one Wails binding left on the data path** — being in-process *is*
  the desktop's authorization. Returns a fresh all-scope ticket (including `scopeUIAck`) on every
  reconnect, and is itself in `remoteDenied`.
- **`ack_control_action` has its own scope `ui:ack`**, granted only by `LocalEndpoint` and kept off
  `orchestration:operate`: the bus sink is unfiltered, so the scope is an identity claim, not authority.
- **`src/runtime/transport.ts`** is the client half and the *only* place desktop and phone differ (via
  the `EndpointSource`; `boundary.test.ts` keeps `src/runtime` from importing components/stores/xterm).
  It queues pre-open calls, keeps its listener map across reconnects, asks for a fresh ticket on every attempt,
  and **rejects queued calls rather than replaying them** on a real drop.

### The shell stream: `seq`, the ring, resume (`src-wails/shellstream.go` + `shellsnapshot.go`)

`busEmit` **numbers** every event and keeps the last **512** in a ring (`shellRingSize`); numbering and
fan-out share one lock (`busOrderMu`) — two emitters otherwise deliver seq N+1 before N and the client's
high-water dedup drops N for good. `welcome`
carries `currentSeq()`; a client sends `{t:"resume", since}` and gets deltas or a `resync`. A process
restart always means `resync` (`since > shellSeq`). **No per-client state on the server** — the ring is
shared and `resumeSince` is a pure function over it.

**Not ringed** (`notRingable` in `bus.go`): `pty-data-*` and the chat channels — both high-volume *and*
replayable from their own durable logs. One streaming turn would churn all 512 slots and make every
reconnect a `resync`. PTY bytes resume by **byte offset** instead: the daemon numbers each PTY's
output and keeps the last 1 MiB (`internal/ptycore/ring.go`); `pty-data-<id>` is `{off, data}`;
`src/runtime/ptyStream.ts` dedupes by offset and, on reconnect or a hole, fetches exactly the gap via
`pty_since` (never a full replay). Gap past the ring / old daemon ⇒ the hole stays, as before.

**`shell_snapshot`** (`scopeOrchRead`) is the first paint in one round trip: workspaces, tabs for
*every* workspace, all phases, chats, and the `seq` it's current as of — **`seq` read first, before any
data** (the other order loses an event for good; this order at worst repeats one). Collections marshal
empty, never null. Deliberately excludes transcripts and scrollback.

Client side: `transport.ts` tracks position, sends `resume` in `onopen` before flushing, drops events
at or behind its position (handlers fire sounds and OS notifications), exposes `onResync(cb)` /
`noteSeq(seq)`. `src/runtime/shellSnapshot.ts` is the read model, `applyShellEvent` its reducer;
unknown event names are ignored on purpose.

**Binary PTY frames are deferred on purpose** — a bandwidth optimization, not a prerequisite.

### Pairing and device tokens (`src-wails/remoteauth.go` + `remotedevices.go` + `remoteguard.go`)

```
POST /v2/pair       {code, name, kind}      → {device_token, environment_id, scopes}
POST /v2/ws-ticket  Authorization: Bearer   → {ticket}
GET  /v2/ws?ticket=…
```

The middle hop is the point: a long-lived device token **never travels in a URL** (proxy logs, history),
so the handshake credential is a *different*, single-use, ≤30 s one. `/v2/pair` is unauthenticated by
necessity; what keeps it honest: six random digits, **3-minute TTL**, single use (success rotates),
**five-guess lockout**. An expired or locked code is reported to Settings as *absent*.

**One row per device** (`remote_devices`), own token (SHA-256 at rest — the defence is against a token
leaving in a backup or error dump), own scopes, no read-back. **A revoke closes that device's live
sockets** (`remoteWS.dropDevice`). The tailnet server mounts the app's **own** `remoteWS` and ticket
store — a second one would mint tickets the redeeming handler doesn't know and keep connections
`RevokeRemoteDevice` never looks at.

**Two fail-closed startup guards** (`remoteguard.go`, pure functions): remote access **refuses to
start** while `tailscale funnel` is on anywhere on the node (funnel publishes this same handler to the
open internet, where six digits is not a defence) — with funnel off the `AllowFunnel` key is **absent,
not false**, and an unreadable config counts as **on**; and the listener binds **loopback only**
(`assertLoopbackAddr`) — also because plain HTTP on a private IP is not a secure context, so no service
worker, no installable PWA, no push.

**Scopes are not a containment boundary.** A paired device is the owner's own phone and holds authority
by design; the real boundary is **paired or not paired**. `access:write` and `ui:ack` are withheld to
avoid handing out the names. `remoteapi.go`'s LOAD-BEARING NOTE lists what a genuinely limited device
role would need.

### The phone (`src/mobile/` + `src/runtime/remoteEndpoint.ts`)

**The PWA is a client of the same socket, command table and event names as the desktop.** Only the
ticket source differs; `core.ts`'s `activeTransport()` picks Wails runtime → desktop, stored
credentials → remote, neither → a `no endpoint` throw. Adding the phone added **no** second data path,
so `commandSurface.test.ts` covers `src/mobile` too.

Credentials live in `localStorage`, not `@/lib/config` — config reads through `invoke`, which needs the
transport, which needs the credentials (a cycle that deadlocks on first load). A **401 from
`/v2/ws-ticket` clears them** and throws `RevokedError`; a 500 does not.

Gone from `src/mobile/store.ts` because the shared runtime does it: its own websocket client and
reconnect loop; its own status derivation (dots now come from `phase-pty:{id}` through `displayStatus`,
with a per-**device** receipt under `burrow.seenAt.mobile`); an N+1 first paint (one `shell_snapshot`
replaced `list_workspaces` + one `list_terminal_tabs` per workspace). `transport.onState` is what the
dashboard reads.

Still mobile-specific: the view stack, the `WorkspaceGroup` shape, and the **chat permission channel**
(still raw — the control/permission protocol is deliberately not in the neutral vocabulary). `store.ts`
was **not** deleted and mobile was **not** moved onto the desktop's Pinia stores: no gain in capability,
different shape and lifetime. `DiffView.vue` completes the PWA v1 surface, read-only (untracked files
diff against `/dev/null`).

**The v1 surface is gone**: `/ws`, `/rpc/`, `/pair`, `Broadcast`, `installWSSink`, and the shared
`http.token` — **deleted from disk at startup** rather than migrated (a token with no scopes, identity
or revocation can't be translated honestly). `TestV1SurfaceIsGone` requires a **404**, not a 401.

**No manual GUI verification of any of this has been done.** Try first: pair a phone; kill the socket
mid-turn; revoke a device; heavy terminal output; ESC mid-turn.

### Orchestration is the main thread's job

There is **no Manager panel**. The per-repo orchestrator chat, its generated primer
(`managerPrimer.ts`), the `.burrow/manager.md` project prompt and the `control: true` session flag are
all gone — a normal thread orchestrates by spawning sub-agents, which land in the Right Panel's
Sub-agents surface (`parent_chat_id`). The `chats.control` column is still on the table, unread, because
dropping it would need a migration for nothing — but `archiveLegacyManagerChats()` (startup, once)
archives the rows that still carry `control = 1` and clears the flag, or an upgrade would resurrect one
Manager thread per project as an ordinary chat. Consequence in `spawn`: a call with a `parent_chat_id`
**always** forces `target: "chat"` — the exemption for a control chat is gone with it.

**Agent docs install** (`agentdocs.go`, at startup): Claude/Copilot get the `burrow` skill
(`agentdocs/skills/burrow/SKILL.md`); Codex gets the same content as a managed `<!-- BURROW:BEGIN/END -->`
block in `~/.codex/AGENTS.md`. The Claude delegation rule (`agentdocs/claude-rule.md`) is **no longer** in
`~/.claude/CLAUDE.md` (startup removes the old block): chats get it via `--append-system-prompt`, PTY tabs
via the **burrow Claude Code mod**.

**The burrow mod** (`agentdocs/mod/burrow`, written to `<app-data>/claude-mod/burrow`, loaded through
`CLAUDE_CODE_PLUGIN_DIRS` in every PTY; needs Claude Code ≥ 2.1.287). Only in a PTY (`BURROW_PTY_ID`):
`tool.call` reports running/waiting in-process and sets `BURROW_MOD=1`, so `burrow hook` skips
`PreToolUse`/`PostToolUse` (no sh+curl per tool call); `prompt.submit` attaches the rule as `context`
once per session. **Load-bearing:** on a Team/Enterprise login the built-in `cc-plugin-sec-default`
guard bypasses user-tier mods on `classic.*` and `prompt.compose/context/section` — so no turn-end,
permission or system-prompt hooks in the mod's `modules`; those are classic `hooks` entries in the
same `hooks/hooks.json` (the `cc-plugin-sec-default` guard governs mods, not settings hooks), each
running `burrow hook`. A second `PermissionRequest` group in that array runs `burrow approve` (below). Tests:
`claude plugin test src-wails/agentdocs/mod/burrow`.

### Answerable PTY permission prompts (`src-wails/ptypermissions.go`, `bin/burrow approve`)

A Claude tab's tool-permission prompt can be answered from Burrow (toast with Allow / Always / Deny **plus**
a banner above that tab's xterm showing the oldest request and a `+N` counter) while the TUI prompt stays
up. The second `PermissionRequest` hook (`burrow approve`, `timeout: 86400`) long-polls loopback
`POST /v1/permission_request?pty_id=` (control.token auth); `permRegistry` parks it, keyed by
`pty_id` + hash(`tool_name`,`tool_input`) (the payload has no `tool_use_id`), and publishes the pty's whole
pending list as `pty-permissions` (state, not delta). **Never falls back to allow:** every path that is not an
explicit answer — app down, any error, hook disconnect, phase leaving `waiting_approval`, `AskUserQuestion` /
`ExitPlanMode` (out of v1, TUI decides) — is empty stdout + exit 0 = TUI decides. Answers: `allow`, `deny`
(+message), `always` = `updatedPermissions` `addRules` with `destination: "session"` (Claude's first `addRules`
suggestion re-pointed at the session, else the exact Bash command / the tool). `chatPermissionRules` do **not**
apply to PTY tabs. Commands: `list_pty_permissions` (read), `answer_pty_permission` (operate) in `remoteAllowed`.
**Load-bearing (spiked, claude 2.1.291): Claude does NOT kill the hook when the user answers in the TUI** — it
lingers until Claude exits (then SIGTERM). So a request answered in the terminal is cleared by the **phase**
leaving `waiting_approval` (`PhaseStore.onChange` → `permRegistry.phaseChanged`; before `waiting_approval` was
ever observed only a turn end closes it, because the status hook races the approve hook), not by the
disconnect.

### Backend (`src-wails/*.go`, bound as `App` methods)

One file per subsystem:

| File | Owns |
|------|------|
| `app.go` | PTY: `CreatePty`, `WritePty`, `ResizePty`, `KillPty`, `ListPtySessions` |
| `db.go` | `workspaces` + `terminal_tabs`; `<app-data>/workspaces.db`, WAL + `busy_timeout(5000)` (the chat-stream writer appends from its own goroutine) |
| `chats.go` | `chats` table, `ListChats`/`CreateChat`/`SaveChats`/`DeleteChat`, `chats-changed`, the one-time migration out of `config.json` |
| `chatstore.go` | `chat_messages`, `SaveChatMessages(chatID, json, foldedOrd)` / `LoadChatMessages` |
| `chatsearch.go` | `chat_fts` (FTS5) over user/assistant `$.text` of `chat_messages`, kept in sync by **triggers** (every writer — fold upsert, replace-all, prune, `DeleteChat` — is covered without remembering to); backfilled once when the table is first created; `SearchChats(query, limit)` → Spotlight's "IN CHATS". Triggers are why the DSN has `_txlock=immediate`: a deferred tx reads FTS5 shadow tables before taking the write lock, and SQLite skips `busy_timeout` for that upgrade |
| `chatstream.go` | append-only `chat_stream` + `chat_stream_state`; `emitChatLine` is the single door for agent output (persist, then emit) |
| `git.go` | `RunGit` wraps the system git binary |
| `forge.go`, `internal/forge/` | Provider-neutral PR operations over each forge's own CLI (`gh`, `glab`, `az repos`, `tea`). `internal/forge` owns the `Forge` interface, one normalized `PullRequest` struct and the four adapters; it takes an injected `Runner` and never imports `main`, so every adapter is tested against captured JSON with no network. The provider is detected from the remote URL, with a per-repo override (`workspaces.forge_provider`) that a worktree inherits by climbing `parent_id`. Optional fields ARE the capability model: a provider that cannot supply checks leaves them empty and the panel hides that section, rather than the app keeping a capability registry that can drift |
| `textgen.go` | `GenerateCommitMessage`, `GeneratePrContent`, `GenerateBranchName`, `GenerateChatTitle` |
| `fs.go` | `ReadDirShallow`, `WriteTextFile` |
| `bus.go` | `busEmit` is the single door for **every** client-visible event; numbers it into the ring unless `notRingable`; exactly **one** subscriber (`/v2/ws`'s per-connection subscription) |
| `phasestore.go`, `phasepoll.go`, `internal/agentphase` | agent phase (above) |
| `environment.go` | `environmentID()` — random id in `<app-data>/environment.json`; client records key off this, not IP/hostname |
| `endpoints.go`, `endpoints_tailscale.go` | `EndpointProvider` registry, `selectEndpoint()`, `remote_endpoints`. **Nothing consumes this yet** |
| `remoteapi.go`, `remoteproto.go`, `remotews.go`, `shellstream.go`, `shellsnapshot.go` | the `/v2/ws` surface (above) |
| `remoteauth.go`, `remotedevices.go`, `remoteguard.go` | pairing, devices, startup guards (above) |

There is deliberately **no** bus → Wails-runtime sink: the desktop reads the bus through its own
`/v2/ws` connection like any client. The only names still on the Wails event channel (`menu-*`,
`lsp-msg-*`, `float-*`, `extension-task:*`, `update:*`) are emitted with `runtime.EventsEmit` directly,
and `src/lib/wailsCompat/event.ts` routes exactly those prefixes. `events_test.go` greps every non-test
`.go` file for a direct `EventsEmit(` against an explicit allowlist, so a new event can't quietly skip
remote clients.

### Background text generation (`src-wails/textgen.go`)

Commit messages, PR title/body, worktree branch names and chat titles are one-shot non-interactive CLI
calls: no PTY, no session, prompt over stdin, JSON schema out. Ported from t3code.

**One preference drives all four.** `ui.textGenerationModel` is `"kind::provider::model::effort"`
(effort optional; older shapes still parse). `ui.textGenerationPolicy`: `default` ·
`conventional_commits` · `repo_conventions` (the last shows the model `git log -20 --format=%s`).
`ui.textGenerationRules` is a free-text house rule appended to **commit and PR** prompts only. All three
live in Settings → General and **`src/stores/git.ts` reads them itself** (`textGenPrefs()`) — the model
*was* an argument, and every new feature forgot the policy.

Per-provider contracts (`generateTextJSONContext`), 180 s budget each:
- **Claude** — `-p --output-format json --json-schema <inline> [--model] [--effort]`, answer from
  `structured_output`. `claudeCliEffort`: `ultracode`→`xhigh`, `ultrathink` dropped.
- **Codex** — `exec --ephemeral --skip-git-repo-check -s read-only --config model_reasoning_effort="…"
  --output-schema <file> --output-last-message <file> -`. **`--output-schema` is what makes Codex answer
  in JSON at all.** Effort defaults to `low`.
- **Gemini / OpenCode** — prose-tolerant: `extractGeneratedJSON` digs the object out.

Every generated string passes a sanitizer (`sanitizeCommitSubject`, `sanitizePrTitle`,
`sanitizeChatTitle`, `sanitizeBranchFragment`) — the output goes straight into `git worktree add -b`.
Branch name and chat title return `""` on failure so the caller keeps what it already showed.

### OSC escape sequence protocol

| Sequence | Direction | Meaning |
|----------|-----------|---------|
| `\x1b]9998;running\x07` | PTY → app | Claude hook: processing user prompt |
| `\x1b]9998;waiting\x07` | PTY → app | Claude hook: waiting for user input |
| `\x1b]9998;done\x07` | PTY → app | Claude hook: turn complete |

Writes go to `/dev/tty` with `2>/dev/null || true`. **No `burrow` subcommand uses OSC**: app actions go
over the loopback control API and results are exchanged as files in `BURROW_SESSION_DIR`, because agent
subprocesses have no controlling tty. `XTerm.vue` keeps a latent `OSC 9999;spawn` parser that nothing emits.

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
| `docs/architecture.md` | Full rationale per subsystem — the long-form version of the Architecture section above | Changing any subsystem whose "why" is recorded there |
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

This project is indexed by GitNexus as **burrow-wails** (13682 symbols, 43194 relationships, 1189 execution flows).

> Index stale? Run `node .gitnexus/run.cjs analyze --index-only` from the project root — it auto-selects an available runner. No `.gitnexus/run.cjs` yet? Bootstrap with `npx`, `bunx`, or `pnpm dlx` — e.g. `bunx gitnexus@latest analyze` (npm 11 npx crash; #1939).

## Always Do

- **MUST run impact before editing.** Use `impact({target: "symbolName", direction: "upstream"})` or `node .gitnexus/run.cjs impact "symbolName" --direction upstream --repo .`; report callers, processes, and risk. Never substitute grep for graph analysis.
- **MUST analyze graph changes before committing.** Use `detect_changes({scope: "all"})` (MCP) or `node .gitnexus/run.cjs detect-changes --scope all --repo .` (CLI fallback). `partial: true` or `truncated: true` is not a clean check — a zero means unseen, not unaffected; re-run it. For regression review: `detect_changes({scope: "compare", base_ref: "main"})` or `node .gitnexus/run.cjs detect-changes --scope compare --base-ref "main" --repo .`.
- MUST warn on HIGH/CRITICAL `risk` pre-edit; never use `riskSharedAxes` to waive a HIGH/CRITICAL `risk` warning. Compare File/symbol: MCP File omits axes; Graph-RAG expands File.
- **MUST treat `risk: UNKNOWN` as unresolved, not as low.** An empty caller set is not evidence the symbol is unused — it can also mean the callers are not resolvable by the index (plain-object property access, dynamic dispatch, cross-language calls). `impact` pairs `UNKNOWN` with a `riskNote` saying so. Confirm with a text search before treating the symbol as safe to change or delete; do not proceed on the strength of a zero.
- **MUST use `query({search_query: "concept"})` for concepts/flows, `context({name: "symbolName"})` for a named symbol, or `impact` for blast radius, on read-only callers, dependencies, imports, or execution flow.** Graph first; text search only for empty/`UNKNOWN`/literals.
- For security review, `explain({target: "fileOrSymbol"})` lists taint findings (source→sink flows; needs `analyze --pdg`).

## Never Do

- NEVER edit a function, class, or method before MCP/CLI impact analysis.
- NEVER ignore HIGH or CRITICAL risk warnings from impact analysis, and never read `UNKNOWN` as an all-clear — it means the walk could not answer, which is the one verdict that requires confirming by other means.
- NEVER rename symbols with find-and-replace — use `rename` which understands the call graph.
- NEVER commit before MCP/CLI graph change analysis.

## Resources

| Resource | Use for |
| --- | --- |
| `gitnexus://repo/burrow-wails/context` | Codebase overview, check index freshness |
| `gitnexus://repo/burrow-wails/clusters` | All functional areas |
| `gitnexus://repo/burrow-wails/processes` | All execution flows |
| `gitnexus://repo/burrow-wails/process/{name}` | Step-by-step execution trace |

## CLI

| Task | Read this skill file |
| --- | --- |
| Understand architecture / "How does X work?" | `.claude/skills/gitnexus-exploring/SKILL.md` |
| Blast radius / "What breaks if I change X?" | `.claude/skills/gitnexus-impact-analysis/SKILL.md` |
| Trace bugs / "Why is X failing?" | `.claude/skills/gitnexus-debugging/SKILL.md` |
| Rename / extract / split / refactor | `.claude/skills/gitnexus-refactoring/SKILL.md` |
| Tools, resources, schema reference | `.claude/skills/gitnexus-guide/SKILL.md` |
| Index, status, clean, wiki CLI commands | `.claude/skills/gitnexus-cli/SKILL.md` |

<!-- gitnexus:end -->

## Agent skills

### Issue tracker

Issues live in GitHub Issues (`JBurdik/burrow-wails`, via `gh`). See `docs/agents/issue-tracker.md`.

### Triage labels

Default five canonical labels (`needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`). See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: one root `CONTEXT.md` + `docs/adr/`, created lazily. See `docs/agents/domain.md`.
