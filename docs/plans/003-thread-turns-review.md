# 003 — Thread turns, plans, diffs, and review

## Goal

Make a thread explainable and reviewable at the level of an agent turn:
proposed plan → execution → settled outcome → checkpoint-backed diff → review
feedback → follow-up turn.

## Depends on

`001-thread-domain.md` and `002-thread-execution.md`.

## Product rules

- A turn is one user/agent work cycle inside a thread, not a new task.
- A plan is a versioned artifact of the thread, not markdown lost in a chat.
- Git checkpointing is available for both workspace modes. It is unavailable,
  but non-blocking, for non-git workspaces.
- `review` means there is a concrete result/diff awaiting a human decision;
  it is not merely an agent process that exited.

## Persistence

Add `thread_turns`, `thread_plans`, `thread_reviews`, and
`thread_review_comments`. Each turn references its execution and optional
baseline/outcome checkpoints. Each review comment retains file, line range,
selected text, and hunk data so it remains useful after rendering changes.

## Implementation tasks

- [ ] Create/settle `thread_turns` from normalised chat events and terminal
  status hooks. Quiescence, not a short timeout, is the completion boundary.
- [ ] Surface ACP plans and recognised Claude plans as thread plans. A
  terminal-first thread also allows a user or Manager to create/paste a plan.
- [ ] Add plan-card actions: edit, accept, continue planning, and **implement
  plan**. Implementing continues the same thread in a new turn/execution; it
  does not create a second task object.
- [ ] Associate the existing Go checkpoint primitive with the thread turn:
  baseline before work and outcome after it settles.
- [ ] Add a thread review surface over the turn diff: changed files, selected
  range, comment creation, and a follow-up prompt containing only selected
  comments and their anchors.
- [ ] Store review/plan/turn lifecycle transitions in `thread_events`.

## Verification

- [ ] Unit tests for plan versioning, turn status, review-comment serialization,
  and follow-up prompt construction.
- [ ] Go tests for checkpoint association and recovery after capture failure.
- [ ] Manual: current-workspace thread → plan → run → review a diff range →
  follow-up; repeat in a worktree thread.
- [ ] Manual: non-git thread still supports plan and execution, with review
  availability clearly explained.
- [ ] `just check` passes.
