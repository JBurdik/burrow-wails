---
name: kanban
description: Read or manage this repo's Burrow Mission Control Kanban board (Backlog → Todo → In Progress → For Review → Done). Use when the user says "/kanban", "add this to the board", "create a task", "what's on the board", "move this card", or wants work tracked/started through the board instead of a plain spawned agent.
---

# Kanban board (Mission Control)

Burrow's board backs its cards with `mission_tasks` rows. Board tasks run as **embedded ACP chat sessions only** — Claude, Codex, Gemini, opencode, whatever `agent` names — never a terminal tab. Starting one never opens a tab in the main view; only an explicit worktree creation shows up there (nested under its repo).

## Prefer the Burrow MCP tools

If you have MCP tools named `board_list`, `board_create`, `board_move` (served by the **burrow** MCP server), **use those** — typed, validated, structured results. The `burrow` shell CLI below is the **fallback** for shells and non-MCP contexts; every CLI subcommand maps to an equivalent tool.

- `board_list({column?})` / `burrow board-list [--column backlog|todo|in_progress|for_review|done]` — list this repo's cards. **Always call this before creating a task**, to avoid duplicating one that already exists.
- `board_create({title, description?, agent?, model?, worktree?})` / `burrow board-create --title T [--description D] [--agent claude|claude-acp|codex|gemini|opencode] [--model M] [--worktree|--no-worktree]` — create a card in **Backlog**. Omit `agent` to default to `claude-acp`. Returns/prints the new task id — report it back to the user. Backlog is the default: spawning is the human's/board's job unless the user explicitly says "start it now".
- `board_move({taskId, column})` / `burrow board-move <taskId> <backlog|todo|in_progress|for_review|done>` — move a card. You may move a card up to `for_review` (e.g. when a sub-agent you spawned for that task finishes and you judge the work ready). You may **not** move a card to `done` — that's rejected outright; only a human marks a task done from the board UI.

## Starting a task now

If asked to start a Backlog task immediately: create it, then move it to `todo`. The actual worktree creation + agent spawn only happens once the app's own Backlog→Todo handler picks up the move (async — the call returns once it's requested, not once the agent is running) — confirm with the user that the card moved, and have them check the board if the agent doesn't appear to start.

## When NOT to use this

Ad-hoc work with no need for board tracking (a quick refactor, a one-off question) — just do it, or use `spawn`/`burrow spawn` for a fire-and-forget sub-agent. Reach for the board when the user wants the work visible/tracked as a card, or explicitly asks for "/kanban" / "the board".