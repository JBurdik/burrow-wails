---
name: burrow
description: Delegate work to sub-agents in new Burrow tabs, supervise them, and drive the Burrow IDE (workspaces, tabs, worktrees, pull requests) from the command line. Use when the user asks to run work in parallel, hand a task to another agent, fan out independent subtasks, or when you need to see or change what the app is showing.
---

# Working with `burrow`

You are running inside **Burrow**, a desktop IDE that runs coding agents in terminal tabs. The `burrow` CLI is on your PATH and talks to the running app:

    burrow <verb> [POSITIONAL] [--arg value ...]

`burrow help` lists every verb this build supports, generated from the app itself — trust it over any list, including this one.

Sub-agents run **interactively on the user's subscription** (never `claude -p`, `codex exec`, or an Agent SDK), and each one gets its own visible tab, so the user can watch it and take over at any time.

## Delegate

    burrow spawn "<task>" [--agent NAME] [--model ID] [--cwd DIR] [--target tab|chat]

`spawn` takes the **task**, not a command line — Burrow builds the command from the user's configured agents. It prints the new tab's `pty_id` and a result `token`.

- Put the whole brief in the task: what to do, which files or area, what NOT to touch, and what to report back. The sub-agent sees only that text.
- `--agent` picks which configured agent runs it (`burrow list-agents`); omit for the user's default.
- `--model` matches the model to the difficulty: `claude-haiku-4-5-20251001` for mechanical work, `claude-sonnet-5` for normal coding, `claude-opus-5` for the hard cases.
- `--cwd` runs it elsewhere — a worktree, when parallel agents would otherwise fight over one working tree.

Then keep working:

    burrow collect-results          # every finished result, non-blocking, consumed as it prints
    burrow wait <token> [--timeout S]   # block for one result; use sparingly

## Supervise

    burrow agent-status             # every agent in the app and what it's doing
    burrow tab-output <pty_id> [--lines N]   # read an agent's recent output
    burrow send-to-tab <pty_id> "<text>"     # send a follow-up into a running agent

`waiting` and `permission` mean an agent is blocked on the **user** — say which tab, don't try to answer for them. Use `tab-output` on an agent that looks stuck, and `send-to-tab` to correct it mid-task rather than letting it finish wrong.

## Worktrees, repo, pull requests

    burrow create-worktree <branch> [--base REF] [--path DIR]
    burrow worktree-remove <branch> [--force]
    burrow git-status / burrow git-log [--limit N] / burrow git-diff [--rev A..B] [--stat]
    burrow run "<read-only command>"        # ls, cat, grep, rg, find, head, tail, wc, jq
    burrow pr-create --title T --body B [--base main] / pr-list / pr-view N / pr-merge N [--squash]

A new worktree appears in the Sidebar nested under its repo, and its path is what you pass to `spawn --cwd`.

## The app's own state

    burrow list-workspaces / burrow list-tabs [--workspace-id ID]
    burrow focus-workspace <id> / burrow focus-tab <pty_id>
    burrow new-tab [--cmd CMD] [--workspace-id ID]
    burrow tab-rename <pty_id> "<title>" / burrow tab-close <pty_id>
    burrow workspace-create <path> [--name NAME]
    burrow diagram "<mermaid>"

## Rules

- Fan out, keep working, collect later. Don't sit blocked on a sub-agent.
- Prefer `burrow spawn` over your built-in Agent/fork tool: in-process agents get no tab, so the user can't watch or steer them.
- `run` is read-only by design. Anything that changes files gets delegated to a sub-agent.
- Destructive verbs (`tab-close`, `worktree-remove`, `pr-merge`) need the user's explicit confirmation first.
- A verb that fails prints why, written for you — read it and adapt instead of retrying the same call.
