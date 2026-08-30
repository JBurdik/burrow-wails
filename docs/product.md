# Burrow — Product Direction

## The idea

**Burrow is a workspace for parallel AI-assisted coding work. Its fundamental
unit is the thread.**

A thread is not merely a chat, terminal tab, or short-lived agent session. It
is the durable record of one piece of work from intent to reviewed outcome.

```text
Project
  └─ Thread
       ├─ intent and context
       ├─ agent and execution style
       ├─ chat or interactive terminal
       ├─ current workspace or optional worktree
       ├─ turns and plans
       ├─ checkpoints, diffs, and review feedback
       └─ outcome and history
```

There is deliberately no separate Task object above a thread. In user language,
starting a task means creating a thread.

## How work flows

1. Open a project.
2. Create a thread with a clear goal and context.
3. Choose an agent and its execution view: structured chat or a real,
   interactive terminal.
4. Choose where it runs:
   - **Current workspace** is the default: quick, direct, no new branch.
   - **New worktree** is opt-in: use it for isolated, risky, or parallel work.
5. The agent works through one or more turns. Burrow exposes progress, tool
   activity, approvals, errors, plans, and completion state.
6. A settled turn can produce a checkpoint-backed diff for review. Feedback on
   a selected file/range becomes context for a follow-up turn in the same
   thread.
7. The thread finishes, fails, is cancelled, or is archived with its history
   intact.

## Why threads

Threads keep the work’s intent, execution, and result together. This avoids
losing context across a growing mix of terminal tabs, chat tabs, worktrees, and
agent sessions. They also provide a shared model for local desktop work, the
Manager, and later remote/mobile control.

## Worktrees are optional

Burrow must never make a worktree mandatory just to create a thread. The
default is the selected current workspace.

When a thread uses a worktree, Burrow creates it only when the thread actually
launches. Drafting a thread must not change the repository. For concurrent,
write-capable threads in the same workspace, Burrow should warn about the
collision risk and offer a worktree; it must not silently switch modes.

## Chats and terminals are equal execution views

Some work is best done in a structured provider chat; other work needs an
interactive, real terminal that the user can take over at any moment. Burrow
supports both on the same thread model:

- A chat-first thread has provider-native messages, tools, plans, and approval
  controls.
- A terminal-first thread has a real PTY and agent status hooks, while still
  retaining the thread’s goal, context, workspace choice, and outcome.
- Ordinary utility terminals may stay outside the thread model.

This is Burrow’s key distinction from a purely chat-first coding-agent client.

## Manager

Manager is the project-level orchestrator for threads. It helps clarify work,
propose a decomposition, create threads, and report attention needs. It must
ask before creating worktrees or launching concurrent/meaningful work; it is
an assistant for deliberate control, not an autonomous project manager.

## Review and completion

“Agent stopped” is not the same as “work is ready.” A thread enters review
only when it has a concrete result to inspect, normally a diff backed by a
checkpoint. The user can annotate a selected change and send that feedback into
the next turn of the same thread.

## Remote and mobile

Remote access should be an attention-first thread surface: see what is running,
respond to approvals, review diffs, and continue a thread. A terminal is an
explicit detail view, not the only remote experience. Sensitive actions require
scoped permissions, not merely possession of a pairing token.

## Product principles

- **Thread-first:** one durable home for one unit of work.
- **Interactive by default:** agents are real sessions the user can observe and
  take over.
- **Worktree by choice:** isolation is available when useful, never imposed.
- **Parallel without chaos:** concurrent work is visible, bounded, and warned
  about when it risks collisions.
- **Reviewable outcomes:** plans, diffs, feedback, and completion are connected.
- **Local-first control:** desktop is primary; remote extends control safely.

## Delivery roadmap

The implementation roadmap lives in:

1. [`001-thread-domain.md`](plans/001-thread-domain.md)
2. [`002-thread-execution.md`](plans/002-thread-execution.md)
3. [`003-thread-turns-review.md`](plans/003-thread-turns-review.md)
4. [`004-manager-thread-orchestration.md`](plans/004-manager-thread-orchestration.md)
5. [`005-thread-remote-reliability.md`](plans/005-thread-remote-reliability.md)
