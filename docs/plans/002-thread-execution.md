# 002 — Launch threads in chat or terminal

## Goal

Bind a thread to a real chat session or an interactive terminal/PTy. The thread
is the owner; chat and terminal tabs are execution views attached to it.

## Depends on

`001-thread-domain.md`.

## Persistence

Add `thread_executions`:

```text
thread_executions
  id TEXT PRIMARY KEY
  thread_id TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE
  kind TEXT NOT NULL                   -- chat|terminal
  workspace_id INTEGER NOT NULL
  chat_id INTEGER NULL
  terminal_tab_id INTEGER NULL
  pty_id TEXT NULL
  agent_id TEXT NULL
  provider_session_id TEXT NULL
  status TEXT NOT NULL                 -- queued|running|needs_input|review|done|failed|cancelled|interrupted
  started_at INTEGER NOT NULL
  finished_at INTEGER NULL
```

A thread has any number of historical executions but exactly one active one.

## Launch contract

1. Validate that the thread has no active execution.
2. Resolve its workspace:
   - `current`: use saved `workspace_id`.
   - `worktree`: create/open it once, save `workspace_id`, then run there.
3. Persist an execution and `thread.launched` event before opening the UI view.
4. Open the selected chat leaf or terminal tab and bind its identifiers.
5. Supply the agent a concise thread header: title, description, workspace mode,
   and thread id.

## Implementation tasks

- [ ] Add atomic Go operations for execution creation/binding and optional
  worktree provisioning. Failures leave the thread runnable and record a
  failure event; they must not leave half-linked state.
- [ ] Extend `Terminal.vue` leaf/tab metadata with `threadId` and
  `threadExecutionId`; preserve enough metadata to reattach after restart.
- [ ] Extend `ClaudeSession` with equivalent optional fields.
- [ ] Add `launchThread(threadId)` in the thread store. It delegates view
  creation to `Terminal.vue`, instead of duplicating tab/split logic.
- [ ] Translate existing terminal and chat XState state into one execution
  coordinator: running; waiting/permission → needs_input; review; done; error
  → failed. Terminal and chat listeners must not race to update the thread.
- [ ] Add thread actions: open execution, stop, retry as a new execution, and
  detach an execution without deleting its history.

## Non-goals

- No scheduler or agent dependency graph yet.
- No automatic diff/checkpoint yet.
- A hand-created terminal remains valid without a thread relation.

## Verification

- [ ] Tests for current-workspace and worktree launch success/failure paths.
- [ ] Tests that restart reattaches the active execution or marks it
  interrupted when the runtime is gone.
- [ ] Tests that one execution cannot be active for two threads.
- [ ] Manual: run one thread in each workspace mode, stop/retry both, and
  confirm their histories stay separate.
- [ ] `just check` passes.
