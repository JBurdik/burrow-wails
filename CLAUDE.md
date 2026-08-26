# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

> **This branch (`rewrite/go-wails`) has replaced the Rust/Tauri backend with Go/Wails v2.**
> Everything below describing `src-tauri/`, Tauri commands, and the Rust event/plugin
> machinery documents the **old** backend (still accurate on `main`) — treat it as an
> architectural reference for *what the Go backend needs to replicate*, not as current
> code on this branch. The actual Go backend lives in `burrow-wails/burrow/`; see its
> command surface across `*.go` files there (one file per subsystem: `workspace.go`,
> `board.go`, `agents.go`, `lsp.go`, etc.) and `src/lib/wailsCompat/` for how the
> unmodified `src/` Vue frontend is wired to it. Progress/remaining-gaps status:
> `~/.claude/plans/ahoj-pros-mt-el-by-distributed-glade.md`.

## What this is

**Burrow** — a desktop app (macOS-first) that wraps real PTYs in a multi-workspace IDE shell, designed to run AI coding agents (Claude Code, Aider, Codex, etc.) side-by-side in terminal tabs. The product name is **Burrow**; the repo/package name is `agentic-ide`.

Stack: Vue 3 + Pinia + xterm.js frontend, Go/Wails v2 backend (`burrow-wails/burrow/`), SQLite for persistence.

## Commands

```bash
# Frontend-only dev (browser, no Tauri)
pnpm dev

# Full Tauri dev (native window, hot-reload)
pnpm tauri:dev

# Type-check + production build
pnpm build          # vue-tsc + vite build
pnpm tauri:build    # full native bundle

# Rust only
cd src-tauri && cargo check
cd src-tauri && cargo build
```

Tests: `pnpm test` (vitest, no DOM env). Currently covers only `src/machines/agentStatus.ts` — the status state machine. **Pin vitest to v2**: v3+ requires vite ≥6, and vite is held at 5 for Tauri.

## Architecture

### Frontend (`src/`)

**Pinia stores** are the backbone — components talk to stores, not each other:

| Store | Owns |
|-------|------|
| `workspace` | List of workspaces (SQLite-backed via Tauri invoke), which one is active, which are "opened" (PTYs kept alive) |
| `terminalTabs` | Lightweight mirror of each workspace's tab list for the Sidebar; the real Terminal component is source of truth |
| `agents` | Configurable agent presets (command, args, shortcut, color) persisted to `localStorage` |
| `ui` | Settings panel open/close, font + scale preferences (persisted to `localStorage`) |
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
      XTerm.vue        ← wraps xterm.js, owns PTY lifecycle via Tauri invoke
  [resize handle]
  RightPanel           ← file tree + git panel
  Spotlight            ← ⌘P command palette
```

**Key keyboard shortcuts:** `⌘,` settings, `⌘P` spotlight.

### PTY / Agent state machine (`XTerm.vue`)

Each `XTerm` creates a native PTY in Rust (`create_pty`), streams bytes via a Tauri event `pty-data-{id}`, and sends input back via `write_pty`.

Agent state (running / waiting for input / done) is detected two ways:
1. **Global persistent hooks (primary)** — at startup `install_status_hooks` (`lib.rs`) merges a status hook into each agent's own global config: Claude `~/.claude/settings.json`, Codex `~/.codex/hooks.json` (same schema). The hook command is `[ -n "$BURROW_PTY_ID" ] && '<app-data>/bin/burrow' hook || true` — a **no-op outside Burrow** (BURROW_PTY_ID unset). Inside a Burrow PTY, `burrow hook` reads the hook JSON on stdin, maps `hook_event_name` → state (`UserPromptSubmit`/`PostToolUse`→running; `PreToolUse`→**waiting** for the blocking tools `AskUserQuestion`/`ExitPlanMode`, else running; `PermissionRequest`→**permission** (agent needs an allow/deny decision); `Stop`→done, **except** a Stop carrying still-running `background_tasks`→running (interim stop — Claude auto-resumes the same session, so reporting done here was the premature-completion bug; the `background_tasks` status check is scoped to that array slice, so an unrelated `"status":"running"` elsewhere in the JSON can't false-positive); `SessionStart`→**session** (forwards `model`/`source`/`session_title` as metadata to label the tab); `StopFailure`→**error** (turn ended on an API error; `error_type` passed through as `detail` — e.g. `billing_error`); `Notification`→refined by its `type` field (`permission_prompt`→permission, `idle_prompt`→waiting, else no-op — blanket no-op only for unknown types now); `SubagentStop`/`SessionEnd`→no-op telemetry, not a turn boundary) and `burrow status <state> [--detail/--model/--source/--title]` POSTs `{ptyId,state,…}` to a local `tiny_http` server (`start_hook_server`). The server re-emits Tauri event `pty-hook-{id}` — bare state string for the legacy states, object `{state,detail?|model?|source?|title?}` for `error`/`session`; `XTerm.vue` listens → emits ONE semantic `agentState` (`running`/`waiting`/`permission`/`done`) which `Terminal.vue`'s `onAgentState` turns into a clean status transition (a single event has no ordering hazard, so a trailing `waiting` can't clobber a fresh `done`). **Because the hooks are global + env-driven, status works for every agent session — launched-by-button, typed by hand, or reattached after restart.** The merge is non-destructive (appends, dedupes by marker, writes a `.burrow-bak`). Port survives restart: `burrow status` reads `<BURROW_HOME_DIR>/hook.port` (authoritative — rewritten each launch) else `BURROW_HOOK_PORT`.
   - Per-tab result capture (`burrow wait`) still needs a per-launch `--settings` with a `Stop→burrow capture <token>` hook, since the token is unique to a spawned sub-agent. That's the **only** remaining per-launch injection.
2. **Polling fallback** — every 2 s, `get_pty_foreground` → title only for agent processes. For an agent foreground proc the poll **never fabricates `busy`** (an agent stays foreground whether thinking or idle at its prompt — equating presence with busy was the old stuck-orange bug). It drives `busy` only for plain commands (`npm test`, `vim`), and clears state when the shell returns to foreground (rescues a Ctrl+C'd agent with no `done` hook). **Dead-PTY watchdog**: if an agent leaf is still in-flight (per its last hook: running/waiting/permission) but `get_pty_foreground` returns empty for ≥3 consecutive polls, the poll confirms the PTY is actually dead via `list_pty_sessions` (`alive=false`) and only then emits `interrupt` to settle the stuck dot — covers an agent killed/crashed with no `Stop`. A single empty read is a transient daemon race and is ignored.

**Status surfacing** (`Terminal.vue`): each leaf carries `status: idle|running|waiting|permission|done|review|error`. `permission` (amber pulse + Sidebar bell) means the agent is blocked on an allow/deny decision — distinct from plain `waiting` (blue). `error` (**red pulse**) is a **failed turn** (Claude `StopFailure`: `rate_limit`/`overloaded`/`authentication_failed`/`billing_error`/`server_error`…); the `error_type` rides through as `detail` (shown as the dot tooltip, set on `leaf.statusDetail`). Like `review`, `error` **persists until the tab is seen** (`markTabSeen`) — it never auto-clears, even while watching; a fresh `running` turn clears it. On turn-finish, `settleDone()` checks `isWatching(tab)` (workspace visible + tab active + window focused): watching → transient `done` (lime, 4 s auto-clear); not watching → **`review`** (green pulse, persists until the tab is seen via `markTabSeen`). `tabStatus()` priority (`STATUS_PRIORITY` in `terminalStatus.ts`): **error** > permission > waiting > running > review > done > idle (error is most urgent — the user must see a failed turn first). The `session` event (`SessionStart`) is **not** a status — it's metadata; `XTerm.vue` forwards `{model,source,title}` via a separate `agentMeta` emit and `Terminal.vue` stashes `leaf.model` + `leaf.sessionTitle` (model shown as the tab tooltip; session title fills in only a default "Terminal N" name, never clobbers an agent-set task title). Surfaced as status dots in the tab bar + Sidebar (Superset-style "agent finished while you were away").

**Claude chat sessions** (`ClaudeChat.vue` + `claudeChats.ts`) mirror this model: a session carries the same `status` (`running`/`waiting`/`permission`/`idle`), derived in `chatStatus()` from in-flight `busy` and the pending `control_request` (generic tool / file edit → `permission` + bell; AskUserQuestion / ExitPlanMode → `waiting`). The **Sidebar renders chats and terminal tabs as one list** distinguished only by icon (`ClaudeIcon` vs `PhTerminal`/`PhRobot`) — no separate "Chats" header; "New chat" lives on the workspace header row. A permission request also fires an in-app toast + (when unfocused) a native OS notification via `notifyPermission()`. Switching permission mode / aborting restarts `claude` with `--resume`; the teardown `exit` is squelched by `suppressNextDone` so it no longer fires a spurious "finished" toast.

### `burrow` CLI (`src-tauri/bin/burrow`)

A POSIX `sh` script embedded in the Rust binary (`include_str!`) and written to `<app-data>/bin/burrow` on each PTY spawn (`ensure_burrow_bin`), with that dir prepended to the shell's `PATH` and `BURROW_SESSION_DIR=<app-data>/sessions` exported. Lets an agent delegate work to sub-agents in new tabs — subscription-safe (launches `claude` **interactively**, never `claude -p` / Agent SDK).

**Transport is file-based, NOT the OSC channel.** Claude's Bash tool and hooks run subprocesses with **no controlling tty**, so `> /dev/tty` fails (`Device not configured`) — the OSC trick can't reach the PTY from there. Instead `burrow spawn` drops a request dir that the frontend polls.

Subcommands:
- `burrow spawn [--token T] [--cwd DIR] <cmd...>` — writes a request dir `<session>/requests/req.XXXXXX/` with raw `cmd`/`token`/`cwd`/`ws` files + a `ready` marker (written last, to avoid reading a half-written request). The command is re-quoted (program name bare so XTerm's `claude` check matches; args single-quoted) so it re-parses correctly when typed into the new tab.
- `burrow worktree <branch> [--base-ref REF] [--path DIR]` — writes a request dir with `kind=worktree` + raw `branch`/`base`/`path`/`ws` files + `ready` marker. Same file-based transport + per-`ws` routing as `spawn`. `Terminal.vue`'s poll branches on `kind`: for `worktree` it resolves the parent repo (climbs `parent_id` if this PTY is itself in a worktree — no worktree-of-a-worktree), computes the disk path `<ui.worktreesDir>/<repo>/<branch>` (same convention as the New-worktree dialog), and calls `wsStore.createWorktree(...)`. That runs `git worktree add` in Rust (`create_worktree`) **and** the store's `load()` → the Sidebar watcher fires → the worktree appears nested under its repo, no manual refresh. `--base-ref` is the base for a NEW branch (default `HEAD`, ignored if the branch exists); `--path` overrides the default disk location.
- `burrow wait <token> [--timeout S]` — blocks until `<session>/<token>.done` appears, prints `<token>.result`.
- `burrow capture <token>` — internal; run by the spawned Claude's **Stop hook** (only when the tab has a `resultToken`). Reads the Stop-hook JSON on stdin, extracts the last assistant message from the transcript (via `node`, always present), writes `<token>.result` + `<token>.done`, then **also calls `burrow status done`** — the per-launch `--settings` Stop hook takes precedence over the global `burrow hook` Stop in Claude Code, so without this a spawned sub-agent's status dot would stick orange after it finished. tty-independent.
- `burrow status <running|waiting|permission|done|error|session> [--detail D|--model M|--source S|--title T]` — POSTs `{ptyId,state,…}` to the hook server. `error` adds `detail` (the API error type); `session` adds `model`/`source`/`title` (metadata values JSON-escaped). Port resolution is **file-first**: the live `<BURROW_HOME_DIR>/hook.port` file (authoritative — rewritten every app launch) then `BURROW_HOOK_PORT` env as fallback. (Env-first was a bug: a daemon-reattached PTY carries a stale baked-in port and POSTs to a dead server.) **Sticky states (`waiting`/`permission`/`done`/`error`/`session`) retry** up to 3× with a 1 s sleep + `hook.port` re-read between attempts, so a POST dropped during the ~3 s port-reclaim window self-heals instead of leaving the dot stuck; `running` takes a single fast attempt (it fires on every tool call and a lost one self-corrects on the next event — never block the agent with sleeps). The generic multi-agent status channel.
- `burrow sessions [--count]` — list the live PTY sessions the daemon is holding (or just their count). Talks the daemon's newline-JSON socket protocol (`Auth` then `ListSessions`) via `python3`, reading `daemon.sock` + `daemon.token` from `BURROW_HOME_DIR`.
- `burrow hook` — internal; invoked by the **globally-installed** Claude/Codex status hooks. Reads hook JSON on stdin, maps `hook_event_name` → `burrow status` (incl. `PermissionRequest`→`permission`, `SessionStart`→`session`, `StopFailure`→`error`, `Notification` `type`→`permission`/`waiting`/no-op). `sed`-based, no `node`/`jq`. `SessionStart`/`StopFailure` + the `Notification` `type` split are Claude-only events (`install_status_hooks` registers them in `~/.claude/settings.json` only; Codex stays on its existing lifecycle events).
- `burrow notify <json>` — internal; legacy Codex `notify`-program path (maps `"type"`). Retained as a fallback; the global `~/.codex/hooks.json` hook is now primary.
- **App read/control commands** (supacode-parity) — all use the **same file-based request-dir transport** as `spawn`, routed to the **origin workspace** (`ws == BURROW_CWD`, always mounted & polling), so there's no double-claim race and no "target not mounted" gap:
  - `burrow list-workspaces` — print every workspace as `<id>\t<name>\t<path>`. **Read command**: drops a request dir with a `token`, then blocks polling `<session>/<token>.result` (same convention as `burrow wait`). Answered **entirely in Rust** inside `take_spawn_requests` (DB query → writes `<token>.result`+`.done`); never reaches the frontend.
  - `burrow list-tabs [--ws ID]` — print a workspace's tabs as `<pty_id>\t<title>` (default: the origin workspace, resolved by path). Same Rust-answered read path, querying the `terminal_tabs` table.
  - `burrow focus-workspace ID` — switch the UI to (and `open`) workspace `ID`. **UI action**: `take_spawn_requests` pushes a `SpawnRequest{kind:"focus-workspace", wsid}` to the frontend; `Terminal.vue`'s poll branch calls `wsStore.open(ws)` (shared singleton).
  - `burrow focus-tab ID` — activate the tab with pty id `ID`. Frontend finds its owning workspace via `tabsStore.tabsByWs`, opens that workspace if needed, then `tabsStore.activate(ws, id)`.
  - `burrow new-tab [--ws ID] [--cmd CMD]` — open a new terminal tab. Same-workspace → `addTab(cmd)` directly; other workspace → `wsStore.open` + `tabsStore.add(ws, cmd)` (the store's `add` now carries an optional `cmd`). Distinct from `spawn`: `new-tab` is a plain UI action targeting any workspace by id, `spawn` is sub-agent delegation in the current project.
  - `SpawnRequest` gained `wsid`/`tabid` fields (single-word names so they survive serde without camelCase surprises). Read results are written by the `write_control_result` helper.
- **Manager orchestration commands** — same Rust-answered read transport as `list-workspaces` (drop request dir → block on `<token>.result`), all routed to the origin workspace; used by the floating **Manager** (`FloatChat.vue`): a persistent Mission-Control chat keyed by the **root repo id** (climbs `parent_id`, so it survives switching between a repo's worktrees and is never empty), carrying the `MC_PRIMER` system prompt that teaches it these commands. Its hidden session is flagged `control: true` so it doesn't appear in the Sidebar chat list:
  - `burrow worktree-remove <branch|path> [--force]` — delete a worktree of the origin repo (git `worktree remove` + its DB row). Resolved by branch (preferred) or on-disk path via `remove_worktree_by` in `take_spawn_requests`; `--force` discards uncommitted changes. The Manager confirms with the user first.
  - `burrow pr-create --title T --body B [--base main] [--head BRANCH] [--cwd DIR]` / `burrow pr-list [--state S] [--cwd DIR]` / `burrow pr-view <n> [--cwd DIR]` / `burrow pr-merge <n> [--squash] [--cwd DIR]` — PR management via the `gh` CLI, run by Rust (`gh_in`) in `--cwd` (a worktree dir, so its branch is in context) else the origin repo. No frontend involvement; never the Agents SDK.

`BURROW_*` env exported into every PTY: `BURROW_SESSION_DIR`, `BURROW_CWD`, `BURROW_PTY_ID`, `BURROW_HOOK_PORT`, `BURROW_HOME_DIR` (app-data dir, also holds `hook.port`).

Frontend: each `Terminal.vue` polls `take_spawn_requests(cwd)` every 1 s. **Routing (DB-arbitrated):** for each request the Rust command picks a single *claimant* workspace — the **target dir** `newcwd` when that dir is itself a workspace row (e.g. a worktree, so the tab nests **under the worktree**, not the spawning repo), else the spawning workspace `ws` (covers `spawn --cwd <arbitrary dir>` where the dir isn't its own workspace, and `worktree` requests whose `newcwd` is empty). Only the Terminal whose `cwd == claimant` claims+deletes the dir → no double-claim race. `--cwd` also sets the new tab's own dir via `Leaf.cwd`. (Earlier this routed purely by `ws`, so a `--cwd`-into-a-worktree tab ran in the worktree dir but wrongly nested under the parent repo.) **Caveat:** the target worktree workspace must be mounted (Terminal polling) to claim — it normally is, since `burrow worktree` opens it on create and an expanded repo mounts its worktrees. (`XTerm.vue` still parses an `OSC 9999;spawn` sequence as a latent direct-PTY path, but the CLI no longer emits it.)

**Agent docs install** (`install_agent_docs`, called once at Tauri `setup`): teaches agents the CLI. Claude → global skill `~/.claude/skills/burrow/SKILL.md`; Codex → managed `<!-- BURROW:BEGIN/END -->` block in `~/.codex/AGENTS.md` (non-destructive merge). Doc strings are `BURROW_SKILL_MD` / `BURROW_AGENT_DOC` consts in `lib.rs`.

**Status hooks install** (`install_status_hooks`, also at `setup`): merges the `burrow hook` status hook into `~/.claude/settings.json` + `~/.codex/hooks.json` via `merge_status_hooks` (parse → append-if-absent → `.burrow-bak`). **Copilot CLI** uses a different schema (its own file per config at `~/.copilot/hooks/<name>.json`, camelCase events, `"bash"` field not `"command"`), so it gets a dedicated `write_copilot_hooks` that writes a self-owned `hooks/burrow.json` wholesale (each event bakes in `burrow status <state>` directly — no `burrow hook` stdin parse needed; deleted wholesale on uninstall). Skips files it can't parse. This is what gives every agent session a status dot. Reverse via `unmerge_status_hooks` (drops only entries matching the `BURROW_PTY_ID`+`hook` marker, leaving the user's/Superset's hooks). Exposed as Tauri commands `reinstall_status_hooks` / `remove_status_hooks` for repair/teardown without a restart.

### Backend (`src-tauri/src/lib.rs`)

All Tauri commands are in `lib.rs`. Key areas:
- **PTY management** — `create_pty`, `write_pty`, `resize_pty`, `kill_pty`, `get_pty_foreground` using `portable-pty`
- **SQLite** (`rusqlite`, bundled) — `workspaces` and `terminal_tabs` tables; DB lives in `<app-data>/workspaces.db`
- **Git** — `run_git` wraps the system git binary (checks known paths)
- **FS** — `read_dir_shallow`, `write_text_file`

### OSC escape sequence protocol

| Sequence | Direction | Meaning |
|----------|-----------|---------|
| `\x1b]9998;running\x07` | PTY → app | Claude hook: processing user prompt |
| `\x1b]9998;waiting\x07` | PTY → app | Claude hook: waiting for user input |
| `\x1b]9998;done\x07` | PTY → app | Claude hook: turn complete |

OSC 9998 status writes go to `/dev/tty` with `2>/dev/null || true` (tolerated when no tty; status then falls back to `get_pty_foreground` polling). **`burrow spawn`/`wait`/`capture` do NOT use OSC** — they exchange files in `BURROW_SESSION_DIR` (`requests/` dirs in, `<token>.result`/`.done` out), because agent subprocesses have no controlling tty. `XTerm.vue` retains a latent `OSC 9999;spawn` parser but nothing emits it.

## Auto-update

Tauri's official updater (`tauri-plugin-updater` + `@tauri-apps/plugin-updater`, plus `tauri-plugin-process` for relaunch). Updates hosted on **GitHub Releases** at `JBurdik/burrow` (public). Endpoint in `tauri.conf.json`: `https://github.com/JBurdik/burrow/releases/latest/download/latest.json` — GitHub's `latest/download` alias always resolves to the newest release's `latest.json`, so the endpoint never changes per version.

**Signing:** updater artifacts signed with an ed25519 keypair (separate from the Apple codesign identity). Private key `~/.tauri/burrow_updater.key`, password in login Keychain (`BURROW_UPDATER_PWD`). **Public** key baked into `tauri.conf.json` `plugins.updater.pubkey`. `bundle.createUpdaterArtifacts: true` makes the build emit `Burrow.app.tar.gz` + `.sig` next to the dmg.

**Frontend:** `src/stores/update.ts` (Pinia) wraps `check()`/`downloadAndInstall()`/`relaunch()` — plugin imports are lazy so browser-only `pnpm dev` (no Tauri) doesn't crash. `App.vue` calls `update.check({silent:true})` 3 s after mount and every 6 h. `UpdateBanner.vue` = persistent floating card (bottom-right): *available* → Install/Later (Later dismisses **per-version** via localStorage so a newer one re-nags), *downloading* → progress bar, *installed* → Restart. Settings → **About** tab mirrors it with the live app version (`@tauri-apps/api/app` `getVersion`) + manual "Check for updates".

**Releasing (`just release [patch|minor|major]`, default patch):** `just bump` lifts the version in lockstep across `tauri.conf.json` + `package.json` + `Cargo.toml`; then build (signed) → notarize/staple dmg → generate `latest.json` (version, notes from git log since last tag, `pub_date`, `darwin-aarch64` url + signature) → commit bump → tag `vX.Y.Z` → push → `gh release create` with dmg + `Burrow.app.tar.gz` + `.sig` + `latest.json`. `version :=` in the justfile reads `tauri.conf.json` live (single source of truth). First-time only: `just gh-init` creates the public repo + `origin` remote. Keychain creds: `BURROW_NOTARY_PWD` (Apple) + `BURROW_UPDATER_PWD` (updater key).

## Documentation (`docs/`)

Standalone HTML reference pages (no build step — open directly in a browser). Keep these in sync when you change the corresponding code:

| File | Covers | Update when |
|------|--------|-------------|
| `docs/context.html` | Whole-project overview: architecture, features, key files, Tauri commands, shortcuts | Adding/removing a component, store, Tauri command, agent, or shortcut |
| `docs/burrow.html` | The `burrow` CLI: spawn/wait/capture, OSC 9999 flow, result capture, agent-docs install | Changing the `burrow` script, OSC 9999 format, or `install_agent_docs` |
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

This project is indexed by GitNexus as **burrow** (4442 symbols, 8263 relationships, 300 execution flows). Use the GitNexus MCP tools to understand code, assess impact, and navigate safely.

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
| `gitnexus://repo/burrow/context` | Codebase overview, check index freshness |
| `gitnexus://repo/burrow/clusters` | All functional areas |
| `gitnexus://repo/burrow/processes` | All execution flows |
| `gitnexus://repo/burrow/process/{name}` | Step-by-step execution trace |

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
