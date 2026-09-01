## Delegating to sub-agents (`burrow`)

You are running inside Burrow, which puts a `burrow` CLI on your PATH for delegating work to sub-agents in new terminal tabs. Reach for it when the user wants to **delegate to agents**, **spawn an agent**, run work **in parallel**, **fan out** subtasks, or **hand off** a task.

- `burrow spawn <command...>` — open a new tab in this project running <command>, fire-and-forget. Example: `burrow spawn codex "write tests for src/foo"`.
- `burrow spawn --token t1 <command...>`, later `burrow collect t1` — delegate with a tracking token, keep working, then pull the sub-agent's final message (non-blocking). `burrow collect` with no token returns every finished sub-agent.
- `burrow wait <token> [--timeout S]` — block for one result. Use sparingly.
- `burrow spawn --cwd DIR <command...>` — run the new tab in another dir.
- `burrow worktree <branch> [--base-ref REF]` — git worktree off this repo; appears in the Sidebar under the repo.
- `burrow list-workspaces` / `burrow list-tabs [--ws ID]` / `burrow focus-workspace ID` / `burrow focus-tab PTY_ID` / `burrow new-tab [--ws ID] [--cmd CMD]` / `burrow workspace-create NAME PATH` / `burrow tab-rename PTY_ID NAME` / `burrow tab-close PTY_ID`
- `burrow diagram '<mermaid>'` — render a diagram in the UI.

Do NOT block waiting on sub-agents. Fan out, continue your own work, then collect. Respect the soft per-workspace concurrency cap `burrow spawn` reports. Sub-agents run interactively on the subscription (never `codex exec` / `claude -p`). Other subcommands (`pr-*`, `git-*`, `run`, `set-status`, `sessions`, `top`) are not answered by this build — avoid them.
