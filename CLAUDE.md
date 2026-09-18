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

Per-device and still in `config.json`: `chatActiveByWs`, `burrow.seenAt`, `chatTurns`,
`chatPermissionRules`. Phone-created chats are titled `Chat N (phone)`.

### Chat stream ownership (`src/lib/chatSession.ts` + `src-wails/chatstream.go`)

A chat's stream is owned by a **session registry keyed by chat id**, not by `AgentChat.vue`: the
session holds transcript, turn state, blocking requests and the `claude-data-{id}` / `acp-data-{id}` /
`acp-req-{id}` listeners; components `setHandlers` on mount, `release()` on unmount, and a session is
only torn down when **idle** (a running turn or pending permission keeps streaming behind an unmounted
view). That's what lets chat leaves render with `v-if`.

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
protocol, Claude `system/init`, the ACP handshake, `serverRequest/resolved`, `acpPromptRpcId`.
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
- **Three inputs:** (1) global persistent hooks — `installStatusHooks` merges a hook into each agent's
  own global config non-destructively; `burrow hook` maps `hook_event_name` → state and POSTs to the
  loopback hook server (port from `<BURROW_HOME_DIR>/hook.port`); `hookEvent()` applies the phase. The
  legacy `pty-hook-{id}` event is gone. (2) foreground poll (`phasepoll.go`, 2 s, server-side) — an
  agent being foreground is **never** busy; only the shell branch may clear `is_agent`; three empty
  reads + daemon not listing the PTY ⇒ `stale`. (3) the interrupt keystroke — `App.WritePty` applies
  `agentphase.Interrupt` on a payload that is exactly one byte of `0x03`/`0x1b` (length is what
  separates a cancel from an arrow key); guarded on `cur.InFlight()`.
- **Chats feed the same store** via `chatPhaseEvent()` in `providerruntime.go`, applied in
  `emitChatLine`.

**The client owns the read receipt, not the phase.** `src/runtime/displayStatus.ts` is a pure
`displayStatus(phase, seenAt, watching) → TermStatus` (`idle|running|waiting|permission|done|review|error`)
plus `shouldMarkSeen()`; `review` and lime `done` are *not* phases. `Terminal.vue` persists `seenAt`
per leaf in `localStorage` under `burrow.seenAt` (read-modify-write, never wholesale) and listens on
`phase-pty:{leafId}`. `tabStatus()` priority (`terminalStatus.ts`): **error** > permission > waiting >
running > review > done > idle.

**`src/machines/agentStatus.ts` is chat-only, not dead code** — terminals are off it, but
`claudeChats.ts` / `AgentChat.vue` drive one instance per chat because chat permission state arrives
on the control/permission protocol, not as a phase. A permission request fires a toast + (unfocused)
a native notification via `notifyPermission()`. The Sidebar renders chats and terminal tabs as **one**
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
the source of the MCP tool schemas (`/v1/_verbs`), `burrow help` and the Manager's primer.

**UI-performed verbs** (open a tab, focus a workspace, read scrollback) call `UIBridge.Do`, which emits
`control:action` and **blocks for the frontend's ack** (`AckControlAction`, 15 s).
`src/lib/controlBridge.ts` is the single app-wide listener. This replaced the old polled
request-dir transport.

**The CLI** is a thin generic client — `burrow <verb> [POSITIONAL] [--arg value]`, kebab-case
normalised, `cwd` always riding along as `$BURROW_CWD`. Only `curl` + `sed`, so it works from Claude's
Bash tool and from hooks. Non-verb subcommands (status plumbing, deliberately independent of the
control API): `burrow status <state>` (**POSTs to `/hook`** — serving only `/status` silently killed
every status dot), `burrow hook`, `burrow notify`, `burrow capture <token>` (writes
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

`busEmit` **numbers** every event and keeps the last **512** in a ring (`shellRingSize`). `welcome`
carries `currentSeq()`; a client sends `{t:"resume", since}` and gets deltas or a `resync`. A process
restart always means `resync` (`since > shellSeq`). **No per-client state on the server** — the ring is
shared and `resumeSince` is a pure function over it.

**Not ringed** (`notRingable` in `bus.go`): `pty-data-*` and the chat channels — both high-volume *and*
replayable from their own durable logs. One streaming turn would churn all 512 slots and make every
reconnect a `resync`. Consequence: a dropped connection still loses **PTY bytes** (a hole in
scrollback); phases, workspace and chat state survive.

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

### Manager (`src/components/ManagerPanel.vue`)

A per-repository orchestrator chat in the right panel. One thread per **root repo** (climbs `parent_id`,
so it survives hopping to a worktree), session flagged `control: true` so it stays out of the Sidebar,
kept mounted per engaged repo and toggled with `v-show`. Stream, composer, permission gates and model
picker all come from `AgentChat` — the panel owns only the thread lifecycle and the primer.

Its primer (`src/utils/managerPrimer.ts`) is **generated from the verb registry** (`control_verbs`) plus
the worktree-isolation toggle and the project's `.burrow/manager.md`: orchestrate, never implement, and
both doors described (MCP tools if present, `burrow <verb>` otherwise — any agent can be the Manager,
so the shell is the common denominator).

**Agent docs install** (`agentdocs.go`, at startup): Claude/Copilot get the `burrow` skill
(`agentdocs/skills/burrow/SKILL.md`) plus an always-in-context rule in `~/.claude/CLAUDE.md`; Codex gets
the same content as a managed `<!-- BURROW:BEGIN/END -->` block in `~/.codex/AGENTS.md`.

### Backend (`src-wails/*.go`, bound as `App` methods)

One file per subsystem:

| File | Owns |
|------|------|
| `app.go` | PTY: `CreatePty`, `WritePty`, `ResizePty`, `KillPty`, `ListPtySessions` |
| `db.go` | `workspaces` + `terminal_tabs`; `<app-data>/workspaces.db`, WAL + `busy_timeout(5000)` (the chat-stream writer appends from its own goroutine) |
| `chats.go` | `chats` table, `ListChats`/`CreateChat`/`SaveChats`/`DeleteChat`, `chats-changed`, the one-time migration out of `config.json` |
| `chatstore.go` | `chat_messages`, `SaveChatMessages(chatID, json, foldedOrd)` / `LoadChatMessages` |
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
