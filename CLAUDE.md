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

### PTY / Agent state machine (`XTerm.vue`)

Each `XTerm` creates a native PTY in Go (`CreatePty`), streams bytes via a Wails event `pty-data-{id}`, and sends input back via `WritePty`.

Agent state (running / waiting for input / done) is detected two ways:
1. **Global persistent hooks (primary)** — at startup `installStatusHooks` (`statushooks.go`) merges a status hook into each agent's own global config: Claude `~/.claude/settings.json`, Codex `~/.codex/hooks.json` (same schema). The hook command is `[ -n "$BURROW_PTY_ID" ] && '<app-data>/bin/burrow' hook || true` — a **no-op outside Burrow** (BURROW_PTY_ID unset). Inside a Burrow PTY, `burrow hook` reads the hook JSON on stdin, maps `hook_event_name` → state (`UserPromptSubmit`/`PostToolUse`→running; `PreToolUse`→**waiting** for the blocking tools `AskUserQuestion`/`ExitPlanMode`, else running; `PermissionRequest`→**permission** (agent needs an allow/deny decision); `Stop`→done, **except** a Stop carrying still-running `background_tasks`→running (interim stop — Claude auto-resumes the same session, so reporting done here was the premature-completion bug; the `background_tasks` status check is scoped to that array slice, so an unrelated `"status":"running"` elsewhere in the JSON can't false-positive); `SessionStart`→**session** (forwards `model`/`source`/`session_title` as metadata to label the tab); `StopFailure`→**error** (turn ended on an API error; `error_type` passed through as `detail` — e.g. `billing_error`); `Notification`→refined by its `type` field (`permission_prompt`→permission, `idle_prompt`→waiting, else no-op — blanket no-op only for unknown types now); `SubagentStop`/`SessionEnd`→no-op telemetry, not a turn boundary) and `burrow status <state> [--detail/--model/--source/--title]` POSTs `{ptyId,state,…}` to a local Go HTTP server (`StartHookServer`, `hookserver.go`). The server re-emits Wails event `pty-hook-{id}` — bare state string for the legacy states, object `{state,detail?|model?|source?|title?}` for `error`/`session`; `XTerm.vue` listens → emits ONE semantic `agentState` (`running`/`waiting`/`permission`/`done`) which `Terminal.vue`'s `onAgentState` turns into a clean status transition (a single event has no ordering hazard, so a trailing `waiting` can't clobber a fresh `done`). **Because the hooks are global + env-driven, status works for every agent session — launched-by-button, typed by hand, or reattached after restart.** The merge is non-destructive (appends, dedupes by marker, writes a `.burrow-bak`). Port survives restart: `burrow status` reads `<BURROW_HOME_DIR>/hook.port` (authoritative — rewritten each launch) else `BURROW_HOOK_PORT`.
   - Per-tab result capture (`burrow wait`) still needs a per-launch `--settings` with a `Stop→burrow capture <token>` hook, since the token is unique to a spawned sub-agent. That's the **only** remaining per-launch injection.
2. **Polling fallback** — every 2 s, `get_pty_foreground` → title only for agent processes. In Go this is `ptycore.Foreground()`: `TIOCGPGRP` (tcgetpgrp) on the PTY master fd — the terminal's own notion of foreground, i.e. whatever the kernel would send Ctrl-C to — resolved to a command name via `p_comm` from sysctl rather than forking `ps`. It travels over the daemon protocol (`kind:"foreground"`) because the daemon holds the master fd; the app-side binding reports failure as an empty name, since the caller polls it and already reads "" as "nothing to say". **The shell IS reported by name** when it is foreground — that is how `XTerm.vue`'s `SHELL_RE` branch learns a command or agent has exited. For an agent foreground proc the poll **never fabricates `busy`** (an agent stays foreground whether thinking or idle at its prompt — equating presence with busy was the old stuck-orange bug). It drives `busy` only for plain commands (`npm test`, `vim`), and clears state when the shell returns to foreground (rescues a Ctrl+C'd agent with no `done` hook). **Dead-PTY watchdog**: if an agent leaf is still in-flight (per its last hook: running/waiting/permission) but `get_pty_foreground` returns empty for ≥3 consecutive polls, the poll confirms the PTY is actually dead via `list_pty_sessions` (`alive=false`) and only then emits `interrupt` to settle the stuck dot — covers an agent killed/crashed with no `Stop`. A single empty read is a transient daemon race and is ignored.

**Status surfacing** (`Terminal.vue`): each leaf carries `status: idle|running|waiting|permission|done|review|error`. `permission` (amber pulse + Sidebar bell) means the agent is blocked on an allow/deny decision — distinct from plain `waiting` (blue). `error` (**red pulse**) is a **failed turn** (Claude `StopFailure`: `rate_limit`/`overloaded`/`authentication_failed`/`billing_error`/`server_error`…); the `error_type` rides through as `detail` (shown as the dot tooltip, set on `leaf.statusDetail`). Like `review`, `error` **persists until the tab is seen** (`markTabSeen`) — it never auto-clears, even while watching; a fresh `running` turn clears it. On turn-finish, `settleDone()` checks `isWatching(tab)` (workspace visible + tab active + window focused): watching → transient `done` (lime, 4 s auto-clear); not watching → **`review`** (green pulse, persists until the tab is seen via `markTabSeen`). `tabStatus()` priority (`STATUS_PRIORITY` in `terminalStatus.ts`): **error** > permission > waiting > running > review > done > idle (error is most urgent — the user must see a failed turn first). The `session` event (`SessionStart`) is **not** a status — it's metadata; `XTerm.vue` forwards `{model,source,title}` via a separate `agentMeta` emit and `Terminal.vue` stashes `leaf.model` + `leaf.sessionTitle` (model shown as the tab tooltip; session title fills in only a default "Terminal N" name, never clobbers an agent-set task title). Surfaced as status dots in the tab bar + Sidebar (Superset-style "agent finished while you were away").

**Claude chat sessions** (`ClaudeChat.vue` + `claudeChats.ts`) mirror this model: a session carries the same `status` (`running`/`waiting`/`permission`/`idle`), derived in `chatStatus()` from in-flight `busy` and the pending `control_request` (generic tool / file edit → `permission` + bell; AskUserQuestion / ExitPlanMode → `waiting`). The **Sidebar renders chats and terminal tabs as one list** distinguished only by icon (`ClaudeIcon` vs `PhTerminal`/`PhRobot`) — no separate "Chats" header; "New chat" lives on the workspace header row. A permission request also fires an in-app toast + (when unfocused) a native OS notification via `notifyPermission()`. Switching permission mode / aborting restarts `claude` with `--resume`; the teardown `exit` is squelched by `suppressNextDone` so it no longer fires a spurious "finished" toast.

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
- **Chat transcripts** (`chatstore.go`) — `chat_messages`, `SaveChatMessages(chatID, json, foldedOrd)` / `LoadChatMessages`
- **Chat stream log** (`chatstream.go`) — append-only `chat_stream` + `chat_stream_state`; `emitChatLine` is the single door for agent output (persist, then emit), used by both `claudechat.go` and `acp.go`
- **Git** (`git.go`) — `RunGit` wraps the system git binary (checks known paths)
- **Text generation** (`textgen.go`) — `GenerateCommitMessage`, `GeneratePrContent`, `GenerateBranchName`, `GenerateChatTitle` (see below)
- **FS** (`fs.go`) — `ReadDirShallow`, `WriteTextFile`

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
