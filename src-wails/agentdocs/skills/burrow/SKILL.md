---
name: burrow
description: Delegate work to sub-agents by spawning new terminal tabs from inside the Burrow IDE. Use when the user asks to run work in parallel, hand a task to another agent, or when you want to fan out independent subtasks and collect their results without blocking.
---

# Delegating with `burrow`

You are running inside **Burrow**. The `burrow` CLI is on PATH in every tab and opens new tabs running sub-agents. The model is **fire-and-forget + collect**: spawn, keep working, pull results when you want. Never sit blocked on a sub-agent.

Sub-agents run **interactively on the subscription** (plain `claude`, never `claude -p` or the Agent SDK).

## Spawn

- `burrow spawn <command...>` — new tab in the current project, returns immediately.
  `burrow spawn claude "write unit tests for src/foo"`
- `burrow spawn --token T <command...>` — same, tagged so you can pick the result up later.
- `burrow spawn --cwd DIR <command...>` — run the new tab in another directory (a worktree, say).

Put the whole task in ONE quoted argument to `claude`, and say what to do, which files, what not to touch, and what to report back — the sub-agent sees only that prompt.

## Collect

- `burrow collect [token...]` — non-blocking recap of finished sub-agents; consumes what it prints. No token = every finished one.
- `burrow wait <token> [--timeout S]` — block until that one finishes, print its final message. Use sparingly; prefer collect.

## Worktrees

`burrow worktree <branch> [--base-ref REF] [--path DIR]` — create a git worktree off this repo. It appears in the Sidebar nested under the repo, and `burrow spawn --cwd <that path>` runs an agent in it. Use one worktree per agent when parallel agents would otherwise fight over the same working tree.

## Workspaces and tabs

- `burrow list-workspaces` — `<id>\t<name>\t<path>` per workspace.
- `burrow list-tabs [--ws ID]` — `<pty_id>\t<title>` per tab (default: this workspace).
- `burrow focus-workspace ID` / `burrow focus-tab PTY_ID` — move the user's view.
- `burrow new-tab [--ws ID] [--cmd CMD]` — plain new terminal tab, any workspace.
- `burrow workspace-create NAME PATH` — add a workspace.
- `burrow tab-rename PTY_ID NAME` / `burrow tab-close PTY_ID [--force]`
- `burrow diagram '<mermaid>'` — render a mermaid diagram as a tab in the UI.

## Rules

Fan out, keep working, then collect. Respect the soft per-workspace concurrency cap `burrow spawn` reports. Prefer `burrow spawn` over your built-in Agent/fork tool — in-process agents get no Burrow tab, so the user can't watch or steer them.

Other subcommands exist in the CLI (`pr-*`, `git-*`, `run`, `set-status`, `sessions`, `top`, `worktree-remove`, …) but are **not answered by this build** and will hang or error. Stick to the commands above.
