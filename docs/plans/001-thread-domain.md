# 001 — Thread as the core work unit

## Goal

Make a **thread** the one durable unit of work in Burrow. A thread represents
one piece of intent and its complete history: conversation, terminal session,
agent, optional worktree, plan, turns, diffs, review, and outcome.

There is no separate Task entity. In product language, “start a task” means
“create a thread”.

## Target model

```text
Project (root repository workspace)
  └─ Thread
       ├─ execution: chat | terminal
       ├─ provider / agent configuration
       ├─ workspace mode: current | worktree
       ├─ optional workspace/worktree binding
       ├─ messages and terminal activity
       ├─ plans and turns                 (later phases)
       ├─ checkpoints/diffs/review        (later phases)
       └─ status and event history
```

## Product rules

- `current` workspace is the default. It uses the selected workspace path and
  does not create a branch or worktree.
- `worktree` is optional. It is provisioned only when the thread is launched,
  never while drafting it.
- A thread may be chat-first or terminal-first; neither is a second-class path.
- A plain utility terminal may remain unthreaded. An agent terminal created for
  a unit of work must belong to a thread.
- Existing chats and terminal tabs stay usable. Their migration to threads is
  lazy/on-open, never a destructive bulk conversion at app startup.

## Persistence

The existing `mission_tasks` / `agent_turns` tables are dead legacy schema.
Leave them untouched; do not rename or repurpose them.

Add new tables in `src-wails/db.go`:

```text
threads
  id TEXT PRIMARY KEY                 -- UUID
  repo_workspace_id INTEGER NOT NULL  -- root repository workspace
  title TEXT NOT NULL
  description TEXT NOT NULL DEFAULT ''
  status TEXT NOT NULL                -- draft|running|needs_input|review|done|failed|cancelled|archived
  execution_kind TEXT NULL            -- chat|terminal, nullable before launch
  agent_id TEXT NULL
  workspace_mode TEXT NOT NULL        -- current|worktree
  workspace_id INTEGER NULL           -- current workspace or provisioned worktree
  worktree_branch TEXT NULL
  created_at INTEGER NOT NULL
  updated_at INTEGER NOT NULL

thread_events
  id INTEGER PRIMARY KEY AUTOINCREMENT
  thread_id TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE
  kind TEXT NOT NULL                  -- created|edited|launched|status_changed|…
  payload_json TEXT NOT NULL DEFAULT '{}'
  created_at INTEGER NOT NULL
```

This is not event sourcing: `threads` is the current read model and events are
an append-only audit/timeline source.

## Implementation tasks

- [ ] Add Go types, migrations, and Wails bindings: `ListThreads`, `GetThread`,
  `CreateThread`, `UpdateThread`, `ArchiveThread`.
- [ ] Add `src/stores/threads.ts` as the only frontend owner of thread state.
- [ ] Add a minimal create-thread UI from Sidebar and Manager: title,
  description, agent, `chat|terminal`, and **Current workspace (default)** /
  **New worktree**.
- [ ] Add a project-scoped thread list showing title, state, execution kind,
  and workspace mode. Keep the existing tab feed intact for this phase.
- [ ] On legacy chat/agent-tab open, offer/link it to a thread or lazily create
  a minimal thread with its existing title and workspace. Never reinterpret a
  normal shell tab as agent work without user action.
- [ ] Write `thread_events` for every user-visible mutation.

## Constraints for the implementing agent

- Before editing any function, class, or method, run the repository-required
  GitNexus impact analysis and report HIGH/CRITICAL findings before proceeding.
- Preserve current worktree actions in Sidebar and `burrow worktree`.
- Use Go/Wails bindings and the existing compatibility layer; do not add Tauri
  code or a parallel persistence system.

## Verification

- [ ] Go tests cover validation, create/update/archive, and event order.
- [ ] Vitest covers the thread store’s loading/error transitions.
- [ ] Manual: create both modes, restart, and confirm persistence; verify a
  worktree-mode draft creates no worktree.
- [ ] `just check` passes.
