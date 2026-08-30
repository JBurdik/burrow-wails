# 004 — Manager orchestrates threads

## Goal

Turn Manager into an assistant that understands and coordinates project
threads. It should no longer reconstruct work only from currently open tabs.

## Depends on

`001-thread-domain.md` and `002-thread-execution.md`; `003-thread-turns-review.md`
is recommended for the full review loop.

## Scope

Manager proposes and manages threads. It requires explicit user confirmation
before it launches concurrent work or creates a worktree. “Use a worktree” is
always a choice, never an inference made silently by the Manager.

## Implementation tasks

- [ ] Extend Burrow MCP/CLI with thread operations: `list_threads`,
  `get_thread`, `create_thread`, `update_thread`, `launch_thread`,
  `stop_thread`, and `thread_add_review_feedback`.
- [ ] Feed Manager compact root-project context: thread titles, states, active
  execution, workspace mode, and attention needs. Do not inject terminal
  transcripts wholesale.
- [ ] Update the Manager primer: clarify intent → propose thread(s) → request
  confirmation for launch/worktree → execute → report state and review links.
- [ ] Add a project thread board/list: Draft, Running, Needs input, Review,
  Done. It is a lightweight attention/navigation view, not a Jira clone.
- [ ] Once the board is stable, add optional `thread_dependencies`. Completing
  a dependency makes the next thread ready; it never auto-launches it.
- [ ] Apply the existing agent-cap limit at `launch_thread`; represent overflow
  as a visible queued state.

## Deferred decisions

- Automated prioritisation.
- Autonomous PR merge.
- Cross-repository dependency graphs.
- Mandatory worktrees.

## Verification

- [ ] MCP/CLI tests cover depth/authorization, malformed commands, and
  root-project ownership.
- [ ] UI tests cover board ordering and attention states.
- [ ] Manual: Manager splits a change into two threads; user launches only one
  and selects current workspace; the other stays Draft/Ready.
- [ ] `just check` passes.
