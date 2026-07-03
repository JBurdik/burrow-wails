# Mission Control → Kanban Board — implementation plan

Status: design only, no code written yet.

## 0. Key existing-architecture facts this plan builds on

- **The live Manager component is `src/components/ManagerBar.vue`**, not `FloatChat.vue`. `FloatChat.vue` is dead code (never imported anywhere) — near-duplicate of ManagerBar's logic. Do not build on FloatChat.vue; extend ManagerBar.vue and treat FloatChat.vue as stale (leave it or delete it in a follow-up, out of scope here).
- **The Manager system prompt is `src/utils/managerPrimer.ts` → `getDefaultManagerPrimer(worktreeMode)`** (there is no literal `MC_PRIMER` constant). It's interpolated with `SPAWN_MODE_WORKTREE`/`SPAWN_MODE_BRANCH` blocks and passed to `<ClaudeChat :append-system-prompt="managerPrimer">`. A project can override it via `.burrow/manager.md`.
- **The Manager talks to the app exclusively through the `burrow` CLI via its own Bash tool call** — there is no MCP server for burrow in this repo (confirmed by exhaustive search: no `@modelcontextprotocol/sdk` usage, `docs/burrow.html` explicitly says "no port, no socket, no MCP server"). The `mcp__burrow__*` tool names visible in *this* session come from an outer harness, not from anything in `mission-control-kanban`. **New "board" capability for the Manager/agents must follow the same pattern: a new `burrow` CLI subcommand, resolved via the existing file-based request-dir transport, answered either purely in Rust (like `git-status`/`list-workspaces`) or via a frontend-pushed `SpawnRequest` (like `focus-tab`).** No new MCP server is needed or in scope.
- **A proto-task table already exists**: `mission_tasks` (`src-tauri/src/lib.rs:3512-3523`), columns `id TEXT PK, workspace_id, pty_id, title, cwd, model, status, turns, created_at` (+ migrated-in `handed_off`, `profile_id`), with existing CRUD Tauri commands `list_mission_tasks` / `upsert_mission_task` / `delete_mission_task` (`lib.rs:3852-3904`). **This plan extends `mission_tasks` into the Kanban board's task table rather than introducing a parallel `board_tasks` table** — same purpose, avoids a duplicate concept. (Rename is optional/cosmetic; functionally we add columns.) If the team prefers a clean-slate name, doing `board_tasks` as a plain rename migration is trivial, but there is no functional reason to avoid extending `mission_tasks`.
- **Terminal (PTY) and ACP chat are two structurally different process transports, not "the same session rendered two ways".** `XTerm.vue` PTYs are created via `create_pty`/`portable-pty`, run in a Rust-owned daemon keyed by numeric `pty_id`, and can be detached/reattached (daemon keeps the process alive; `detach_pty` only stops the frontend's data stream). `ClaudeChat.vue` sessions (`claude_start`/`acp_start`) spawn `claude`/ACP-agent binaries directly via `std::process::Command` with piped stdio, keyed by `chatId`/agent-native `sessionId`, entirely separate from the daemon/PTY infra. **There is no existing mechanism, and no cheap way to add one, for a PTY view and an ACP view to be live over the literal same running process simultaneously.** The pragmatic model (detailed in §5) is: only one transport is "live" for a task at a time; switching views tears down one transport and starts the other **resumed from the same underlying Claude session id** (`--resume <session_id>` for the PTY path, `resumeSessionId` for ACP) — continuity of conversation history, not literal shared process attachment. This is called out explicitly as a design constraint, not a bug to silently work around.
- **Daemon-level multi-attach to one `pty_id` is actually supported** (broadcast channel + ring-buffer replay in `daemon_main.rs`), but the **Tauri-process-level `DaemonClient.streams` map is single-thread-per-`pty_id`** (`daemon_client.rs`) — mounting two `XTerm.vue` instances on the same `ptyId` in the same window today causes duplicate/corrupted byte delivery. So even within "terminal-only" (no ACP), two live terminal views of one task must not be mounted concurrently; reuse the existing `adoptPty()` exclusive-focus pattern (`Terminal.vue:1043-1060`).
- **Image attachments today**: ACP path sends images as base64 data-URIs over JSON-RPC (`session/prompt`, ACP `ContentBlock{type:"image",mimeType,data}`) or, for stream-json Claude, as Messages-API `image` blocks — no file staging, pure in-memory strings, and `ClaudeChat.vue` already exposes `sendMessage(text, images: string[])` via `defineExpose`, proven to be driven externally by `ManagerBar.vue` (`chatRefs.get(id)?.sendMessage(text, imgs)`). Raw-PTY path has **no image channel** — `initialCmd` is a plain typed string; there is no existing Tauri command to write binary files to disk (`write_text_file` is text-only).
- **Manager keys to the root repo** by climbing `parent_id` on the active workspace (`root`/`rootId` computed in ManagerBar.vue) — same convention the Kanban board must use.
- **`control` sessions are hidden from the Sidebar structurally**, not by filtering `sessions.value` — they simply never get a `terminal_tabs`/tab-list row created for them, because `ensureControlSession()` calls `chats.create()` directly rather than going through the normal "+chat" tab-creation path.

## 1. Data model (SQLite, `src-tauri/src/lib.rs` schema block)

### 1.1 Extend `mission_tasks` → the board's task table

Current (`lib.rs:3512-3523`, plus migrated columns):
```sql
CREATE TABLE mission_tasks (
    id           TEXT PRIMARY KEY,
    workspace_id INTEGER NOT NULL,
    pty_id       INTEGER,
    title        TEXT NOT NULL,
    cwd          TEXT,
    model        TEXT,
    status       TEXT,
    turns        INTEGER,
    created_at   INTEGER NOT NULL,
    handed_off   INTEGER,     -- migrated in
    profile_id   TEXT         -- migrated in
);
```

Add via idempotent `ALTER TABLE ... ADD COLUMN` migrations (same pattern as existing `workspaces`/`terminal_tabs` migrations):

```sql
ALTER TABLE mission_tasks ADD COLUMN repo_workspace_id INTEGER;   -- root repo id (climbed parent_id), NOT the worktree id — board is keyed by this
ALTER TABLE mission_tasks ADD COLUMN board_column TEXT NOT NULL DEFAULT 'backlog';
  -- 'backlog' | 'todo' | 'in_progress' | 'for_review' | 'done'
ALTER TABLE mission_tasks ADD COLUMN description TEXT;            -- markdown body, shown in Backlog card before any agent exists
ALTER TABLE mission_tasks ADD COLUMN agent_kind TEXT;              -- 'claude' | 'codex' | 'aider' | ... (chatAgents registry id)
ALTER TABLE mission_tasks ADD COLUMN transport TEXT;               -- 'pty' | 'acp' | 'stream-json' | NULL (no agent yet)
ALTER TABLE mission_tasks ADD COLUMN use_worktree INTEGER NOT NULL DEFAULT 1;  -- 0 = work directly on current branch, no worktree
ALTER TABLE mission_tasks ADD COLUMN worktree_branch TEXT;         -- set only if use_worktree=1 and a worktree was created
ALTER TABLE mission_tasks ADD COLUMN task_workspace_id INTEGER;    -- the actual workspace row the agent runs in (== repo_workspace_id if use_worktree=0, else the worktree's workspace id)
ALTER TABLE mission_tasks ADD COLUMN chat_id INTEGER;              -- claudeChats.ts session id, when transport='acp'/'stream-json'
ALTER TABLE mission_tasks ADD COLUMN session_id TEXT;              -- agent-native session id (for --resume across transport switches) — reuses existing `session_id` concept from terminal_tabs
ALTER TABLE mission_tasks ADD COLUMN board_order REAL NOT NULL DEFAULT 0;  -- float, for drag-reorder within a column (avoids renumbering siblings)
ALTER TABLE mission_tasks ADD COLUMN updated_at INTEGER;
```

Notes:
- `pty_id` (existing column) is reused for the terminal-view transport; `chat_id` (new) for the ACP-view transport. A task may have one, the other, or (after a view switch) have had both at different times — never both live simultaneously, per §0.
- `status` (existing column, currently free-text) stays as the **agent turn status** (`running`/`waiting`/`permission`/`done`/`error`/`idle`), independent from `board_column` (the Kanban column) — these are different axes. Reuse `TermStatus`/`STATUS_PRIORITY`/`aggregateStatus` from `src/lib/terminalStatus.ts` for the card's status dot, exactly as leaves/chats already do.
- A **Backlog** card (no agent yet) has `pty_id/chat_id/transport/session_id/task_workspace_id` all NULL, and only `title`/`description`/`agent_kind`/`model`/`use_worktree` set. Moving Backlog→Todo (see §7) is what triggers worktree creation (if `use_worktree=1`) + agent spawn, at which point `transport`, `pty_id` or `chat_id`, `task_workspace_id`, and (once the agent reports it) `session_id` get filled in.

### 1.2 New `task_attachments` table

```sql
CREATE TABLE IF NOT EXISTS task_attachments (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id     TEXT NOT NULL,
    ord         INTEGER NOT NULL,
    mime_type   TEXT NOT NULL,        -- e.g. 'image/png'
    file_path   TEXT NOT NULL,        -- on-disk path under <app_data_dir>/attachments/<task_id>/<n>.<ext>
    created_at  INTEGER NOT NULL,
    FOREIGN KEY(task_id) REFERENCES mission_tasks(id) ON DELETE CASCADE
);
```

Design choice: **store attachments as files on disk, not as DB blobs.** Rationale: (a) both the ACP delivery path (base64 data-URI) and the raw-terminal delivery path (file path referenced in prompt text, per §6) need file bytes at some point anyway — staging to disk once avoids double-encoding; (b) keeps the SQLite DB small; (c) a new Tauri command `write_task_attachment(taskId, base64, mimeType) -> path` is a natural, minimal addition next to the existing (text-only) `write_text_file`. Reading back for ACP delivery = read file + re-base64-encode (cheap, small images).

## 2. New Tauri commands (`src-tauri/src/lib.rs`)

All follow existing naming/error conventions (`Result<T, String>`, rusqlite via `DbState`).

- `list_board_tasks(repo_workspace_id: i64) -> Vec<MissionTask>` — extends existing `list_mission_tasks`; filter by `repo_workspace_id` instead of `workspace_id` so it returns tasks across the repo's worktrees, not just one workspace row.
- `upsert_board_task(task: MissionTaskInput) -> MissionTask` — extends existing `upsert_mission_task` with the new columns.
- `move_board_task(task_id: String, column: String, order: f64) -> ()` — dedicated narrow command (vs. full upsert) since this is the hot path both the UI drag-drop AND the new `burrow board-move` CLI subcommand hit; emits a `board-task-moved` Tauri event so any open Kanban view updates live regardless of which workspace/tab issued the move.
- `delete_board_task(task_id: String) -> ()` — extends `delete_mission_task`; should also cascade-delete `task_attachments` rows (FK `ON DELETE CASCADE` handles DB rows) and unlink (not necessarily kill) any live pty/chat — see §7 open questions on whether deleting a task should kill its agent.
- `write_task_attachment(task_id: String, base64_data: String, mime_type: String) -> String` (returns file path) — decodes base64, writes to `<app_data_dir>/attachments/<task_id>/`, inserts a `task_attachments` row.
- `list_task_attachments(task_id: String) -> Vec<TaskAttachment>`.
- `delete_task_attachment(attachment_id: i64) -> ()`.
- `read_task_attachment_base64(attachment_id: i64) -> (String /*base64*/, String /*mime*/)` — used by the frontend to feed `sendMessage(text, images)` on the ACP path without the frontend needing its own file-read+base64 code (mirrors how `read_text_file` already exists for text).

No new Rust command is needed for "toggle worktree vs branch" — it's just the `use_worktree` column plus reusing existing `create_worktree`/`remove_worktree` commands at the point the task transitions Backlog→Todo (§7).

## 3. New `burrow` CLI subcommands (agent/Manager-facing, file-based transport per §0)

Add to `src-tauri/bin/burrow` (embedded via `include_str!`) and to `take_spawn_requests` in `lib.rs`, following the exact same two patterns already used:

- **`burrow board-move <taskId> <column>`** — the requirement-2 command: an agent (or the Manager) calls this when it finishes work or needs review. Drops a request dir `kind=board-move, task_id, column, ws, ready`. **Answered purely in Rust** inside `take_spawn_requests` (same "answered entirely in Rust, no frontend" pattern as `git-status`/`list-workspaces`): validates `column ∈ {backlog,todo,in_progress,for_review,done}`, calls the same logic as `move_board_task`, writes `<token>.result` = `"ok"`, emits `board-task-moved` event so any mounted Kanban view refreshes. No frontend involvement needed since it's a pure DB write + event emit (unlike `focus-tab`, which needs the frontend to actually change what's on screen).
- **`burrow board-list [--column C]`** — read command (blocking, like `list-workspaces`): lists tasks for the *origin* repo (`ws == cwd` claiming rule, per §0), tab-separated `id\tcolumn\ttitle\tstatus`. Lets the Manager answer "what's in progress" without a dedicated MCP layer.
- **`burrow board-create --title T [--description D] [--agent claude|codex|aider] [--model M] [--worktree|--no-worktree]`** — write command creating a new Backlog-column task row (fire-and-forget like `spawn`/`workspace-create`); requires a small frontend or pure-Rust handler — since task creation only touches the DB (no PTY/UI action needed), this can also be answered **purely in Rust**, matching `git-status`'s "no frontend" model, not `spawn`'s "frontend must act" model.
- **`burrow board-attach <taskId> <imagePath>`** — optional convenience so an *agent itself* (not just the card UI) could attach a screenshot it produced mid-task (e.g. a Codex agent saving a rendered diff screenshot back onto its own card for the user to see in review). Lower priority; flag as optional/phase-2.

These four are the only new CLI surface. They compose with the **existing** `worktree`/`spawn`/`wait`/`collect` commands rather than duplicating them — task creation with `use_worktree=1` still uses `burrow worktree` + `burrow spawn --token` under the hood (see §7), `board-move`/`board-list`/`board-create` are purely the new Kanban-specific primitives.

Update `managerPrimer.ts` (§8) to document these four commands so the Manager can list/move/create tasks without needing new tool schemas.

## 4. Frontend components

### New
- `src/components/KanbanBoard.vue` — the board view itself: five columns (Backlog/Todo/In Progress/For Review/Done), one card per `mission_tasks` row for the current root repo, drag-and-drop reorder/column-move (writes via `move_board_task`, optimistic local update + reconcile on `board-task-moved` event so agent-driven moves from `burrow board-move` also live-update the board while it's open). Reuses `aggregateStatus`/`STATUS_PRIORITY` from `terminalStatus.ts` for each card's status dot.
- `src/components/TaskCard.vue` — single card: title, truncated description, model/agent badge, attachment thumbnail strip, status dot, worktree/branch badge. Click opens `TaskDetail.vue`.
- `src/components/TaskDetail.vue` — the modal/panel for one task: description editor + attachment manager (Backlog state, before any agent exists), and once an agent is spawned, the **view switcher** (chat ⟷ terminal, §5) plus the **worktree/branch toggle** (§6, editable only pre-spawn; read-only badge post-spawn since switching mid-flight would require moving the whole worktree, out of scope). Owns the "move Backlog→Todo triggers spawn" action (§7).
- `src/stores/boardTasks.ts` — new Pinia store, mirrors the `workspace`/`terminalTabs` store pattern: `tasksByRepo: Record<repoId, MissionTask[]>`, `attachmentsByTask`, actions wrapping the new Tauri commands, a listener on the `board-task-moved` Tauri event (so both local UI-driven moves and CLI-driven `board-move` calls converge through the same store).

### Modified
- `src/components/Sidebar.vue` — add a "Board" entry per repo (next to the existing workspace/chat tab list), opening `KanbanBoard.vue` for that repo's root id — mirrors how `ManagerBar` is anchored to `rootId`.
- `src/components/ManagerBar.vue` + `src/utils/managerPrimer.ts` — see §8.
- `src/components/Terminal.vue` — extend `Leaf` (in `TerminalSplitView.vue:75-102`) with an optional `taskId?: string`, so a terminal-view task's tab can be found/adopted via the existing `adoptPty()` path (`Terminal.vue:1043-1060`) instead of a new mechanism; extend `PersistedTab`/`save_terminal_tabs` schema and the `terminal_tabs` table with a matching `task_id TEXT` column if task↔tab linkage must survive app restart (it should, so a restarted app can still find "which tab belongs to which card").
- `src/stores/claudeChats.ts` — extend `ClaudeSession` with an optional `taskId?: string` for the same reason on the ACP side, and a `create()` overload accepting `{taskId, cwd, agentKind}` so `TaskDetail.vue` can create a session already scoped to the task's worktree.
- `src/components/ClaudeChat.vue` — no structural change required; its existing `sendMessage(text, images)` `defineExpose` API (already proven by `ManagerBar.vue`'s `chatRefs.get(id)?.sendMessage(...)` pattern) is reused verbatim for first-prompt delivery (§6). Consider adding a `ready` emit (currently absent — `ManagerBar.vue` uses `nextTick()` heuristics) so `TaskDetail.vue` can reliably know when to fire the first `sendMessage()` after mount, rather than copying the same heuristic a third time.

## 5. Session-sharing between the ACP view and the terminal view

Given §0's finding that PTY and ACP sessions are genuinely different OS processes with no shared-attach mechanism, the plan is **conversation continuity via `--resume`/`resumeSessionId`, not literal process sharing**:

1. Whichever transport is spawned first (per the task's `agent_kind`/initial choice) becomes `transport = 'pty'` or `'acp'`/`'stream-json'`, and `mission_tasks.pty_id` or `.chat_id` is set accordingly.
2. Every transport already surfaces (or can surface) the agent-native `session_id` once the turn starts: PTY path via the `SessionStart` hook event (`XTerm.vue` already emits `agentMeta.model/source/title` from this event — extend it to also forward the session id if the hook JSON carries one, or capture it the same way `Terminal.vue`'s `leaf.sessionId` is already populated for `--resume` support on app-restart); ACP/stream-json path already captures it into `ClaudeChat.vue`'s local `sessionId` ref and persists it via `chats.sync(id, {claudeSessionId})`. Both write back into `mission_tasks.session_id`.
3. **Switching view** (terminal → chat, or chat → terminal) on a task:
   - Tear down the currently-live transport (`kill_pty`/`detach_pty` for terminal, `acp_stop`/`claude_stop` for chat) — **detach, don't necessarily kill**, matching existing `detach_pty` semantics, so a same-session PTY reattach later doesn't lose daemon-side state if the user switches back before ever tearing down (i.e. prefer detach over kill when going PTY→ACP, since the daemon can keep it alive cheaply; going ACP→PTY has no equivalent "detach", ACP processes must fully stop).
   - Start the other transport **resumed**: PTY path types `claude --resume <session_id>` as the `initialCmd` (exactly the pattern `Terminal.vue`'s restart-after-crash logic already uses for dead PTYs with a `session_id`); ACP/stream-json path calls `chats.create()` + mounts `ClaudeChat.vue` with `resumeSessionId: session_id` (exactly `acpStartPayload()`'s existing `resumeSessionId` field).
   - Update `mission_tasks.transport`/`pty_id`/`chat_id` to reflect the new live transport.
4. **Net effect**: the user sees the same conversation history (native session resume), but there is a brief transport handoff, not an instantaneous live-swap of a running process. This should be stated plainly in the UI (e.g. a short "resuming session…" transition state) rather than implied to be seamless — flagged as a risk in §9.
5. If a task was created with `use_worktree=0` (branch mode), both transports simply run with `cwd = repo root` instead of a worktree path — no special-casing needed beyond what `cwd` already is.

## 6. Attachment flow (screenshots → first prompt)

**Backlog/Todo stage (no agent yet):**
- `TaskDetail.vue` lets the user paste/drop images (reuse `ClaudeChat.vue`'s existing paste-to-dataURL pattern for the *widget*, but instead of holding them in a JS ref, immediately call `write_task_attachment(taskId, base64, mime)` so they're durably staged as files under `<app_data_dir>/attachments/<taskId>/` per §1.2, independent of any chat/terminal session existing yet.

**On Backlog→Todo transition (agent spawn, §7), first-prompt delivery:**
- **ACP/stream-json path**: after `ClaudeChat.vue` mounts and its `ready` state is reached (§4), call `read_task_attachment_base64()` for each `task_attachments` row (ordered by `ord`) to rebuild the `images: string[]` array, then call the exposed `sendMessage(description, images)` — this is a direct reuse of the exact mechanism `ManagerBar.vue` already uses to prime a session (`chatRefs.get(id)?.sendMessage(text, imgs)`), no new plumbing needed on the ACP/Rust side at all.
- **Raw-terminal path**: no JSON-RPC channel exists over a plain PTY (per §0). The task's `description` plus **absolute file paths** to each attachment must be composed into the `initialCmd` text handed to `XTerm.vue`'s existing `initialCmd` prop, e.g.:
  `claude "<description text>\n\nAttached screenshots:\n- /Users/.../attachments/<taskId>/0.png\n- .../1.png"`
  This relies on the target CLI agent (Claude Code, and per requirement 7 also Codex/Aider) being able to read an image given its file path when referenced in prompt text — true for Claude Code's CLI; **must be verified per-agent for Codex/Aider (open question, §9)**, since not every CLI agent necessarily resolves image paths mentioned in free text the same way.
  Since `write_task_attachment` already stages real files on disk (§1.2), no new binary-file-write command is needed beyond what's already added for the ACP path — the file staging step is shared between both delivery paths, only the "how it's told to the agent" differs (JSON content block vs. path reference in text).

## 7. Backlog → Todo transition (worktree/spawn trigger) and column semantics

- **Backlog**: pure metadata card, no `pty_id`/`chat_id`/`task_workspace_id`. Freely editable (title, description, attachments, agent_kind, model, use_worktree toggle).
- **Todo**: user clicks "Start" on a Backlog card (or drags it to Todo — either UI action should trigger the same logic, not just the drag). This is the one moment worktree/spawn actually happens:
  1. If `use_worktree = 1`: call the existing `create_worktree` Tauri command with a generated branch name (e.g. `task/<slug>`), same as `burrow worktree` already does for the Manager — set `mission_tasks.task_workspace_id` and `.worktree_branch` from the result.
     If `use_worktree = 0`: `task_workspace_id = repo_workspace_id` directly (no worktree call at all), `worktree_branch = NULL`.
  2. Spawn the agent per `agent_kind`/chosen transport: either mount a fresh `ClaudeChat.vue` (ACP path) or `addTab()` a new terminal leaf with `initialCmd` (terminal path) — both scoped to `task_workspace_id`'s cwd.
  3. Deliver description + attachments as the first prompt per §6.
  4. Set `board_column = 'todo'` immediately (so the card visibly moves even before the agent's first turn completes), then let the agent's own turn-status (`running`/`waiting`/etc., independent axis) reflect progress via the existing status-dot machinery.
- **In Progress / For Review / Done**: from here on, **moves are driven by the agent itself or by the Manager**, via `burrow board-move <taskId> <column>` (§3) — this is the literal implementation of requirement 2 ("after Todo, the task/card moves itself through the columns"). The human can still drag manually in the UI (writes via `move_board_task` directly), which is just the same underlying primitive.
- Recommended convention (documented in the Manager primer, §8, and ideally in a short per-agent instruction injected alongside the task's first prompt): agent moves its own card to `in_progress` on starting real work, `for_review` when it believes the task is complete and wants human/Manager review, and **should not** move itself to `done` — reserve `done` for a human/Manager decision, so nothing silently disappears from view without a human ack. This is a policy choice worth confirming with the user (see §9).

## 8. Manager integration

The Manager (`ManagerBar.vue`) stays a separate floating/docked chat — the board does not replace it, per requirement 8. Changes needed:

- **`src/utils/managerPrimer.ts`**: add a new section (alongside the existing "App/navigation", "Orchestration", "Pull requests" sections) documenting the four new commands from §3: `board-list [--column C]`, `board-create --title T [...]`, `board-move <taskId> <column>`, and (phase 2) `board-attach`. Emphasize in the primer text: the Manager should use `board-list` before creating a duplicate task, should create a task in `backlog` by default (not `todo`, since spawning is the human's/board's job unless explicitly asked to "start it now" — in which case the Manager can itself do `board-create ... && board-move <id> todo`, understanding that only actually starts the worktree/spawn flow if the frontend/Rust handler for the Todo transition — §7 step 1-3 — is *also* reachable from the pure-Rust `board-move` handler, which raises an open question, see §9), and should report task ids back to the user so they can find the card in the UI.
- **No change needed to the Manager's own session/control-flag/hidden-tab plumbing** (`claudeChats.ts` `control: true`, `ensureControlSession()`, `rootId` climbing) — the board is a fully independent read/write surface over the same `mission_tasks`/`workspaces` tables, keyed by the same root-repo convention, but the Manager remains just an ordinary Bash-tool-driven `ClaudeChat` session with an extended primer, not a special integration point.
- **Optional nice-to-have**: since ManagerBar already listens for the `agent-done` Tauri event to inject a "sub-agent finished, run `burrow collect`" nudge (`ManagerBar.vue:701-726`), add a parallel listener on the new `board-task-moved` event so the Manager can proactively mention "task X moved to For Review" in its own chat if the user has it open — purely cosmetic, low priority.

## 9. Risks / open questions (needs user decision before implementation)

1. **View-switch is a resumed-session handoff, not a live shared attach** (§5) — is a brief "switching…" transition acceptable, or does the user expect true simultaneous dual-rendering of one process? If the latter is a hard requirement, it needs a deeper daemon-level change (ref-counted `DaemonClient.streams`, §0) that's materially bigger scope than the rest of this plan — recommend confirming resumed-handoff is acceptable before scoping further.
2. **Does `burrow board-move` need a frontend round-trip at all**, or can it always be answered purely in Rust? Board column moves are pure DB+event, so pure-Rust is fine *except* the Backlog→Todo transition, which needs worktree creation + agent spawn — those specifically require the frontend (existing `create_worktree`/PTY-spawn code paths live client-side). Recommendation: `board-move` to `todo` specifically triggers a frontend-pushed `SpawnRequest` (like `focus-tab`), while moves to any other column are pure-Rust. Needs explicit confirmation this asymmetry is acceptable/understood.
3. **Can non-Claude agents (Codex, Aider) actually resolve an image file path mentioned in prompt text** the way Claude Code's CLI does (§6, raw-terminal attachment delivery)? Needs a quick empirical check per agent before relying on it — if not, the raw-terminal path may need a different in-prompt convention per agent type (e.g. some tools expect `@path` syntax).
4. **Should an agent be allowed to move its own card to `done`**, or should `done` require a human/Manager decision (§7)? Affects both the primer wording and whether `board-move` should reject `done` from CLI/agent callers and require it come from the UI's `move_board_task` call specifically.
5. **Deleting a task**: should `delete_board_task` also kill the live pty/chat and remove the worktree, or just unlink/orphan them (leaving the terminal tab / worktree as an ordinary standalone tab/workspace the user can still deal with manually)? Recommend the latter (safer, matches "detach don't destroy" philosophy already used for `detach_pty`), but needs confirmation since it means "delete" doesn't fully clean up.
6. **`mission_tasks` rename**: confirm whether to keep the table name `mission_tasks` (functional, no migration risk) or rename to `board_tasks` for clarity (needs a `CREATE TABLE ... AS SELECT` + drop-old migration, purely cosmetic, slightly higher migration risk for zero functional gain).
7. **Multi-repo worktree-of-worktree**: `create_worktree` already enforces `parent_id IS NULL` on the parent (no nested worktrees) — a task's worktree is always a direct child of the root repo, never of another task's worktree, which is consistent with requirement 1 (board is per-repo) but should be double-checked against any future "sub-task" concept.
