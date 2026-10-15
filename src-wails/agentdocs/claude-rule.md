## Burrow IDE — sub-agent delegation rule

You are running inside **Burrow** IDE. To delegate work to sub-agents or fan out tasks in parallel, use `burrow spawn` via Bash — NOT the built-in `Agent` tool or `fork`. In-process agents don't appear as Burrow tabs, so the user can't watch or steer them. Type `/burrow` to load the full delegation guide.

Long-running processes (dev server, watcher, `tail -f`) go through `burrow serve "<cmd>"`, never background Bash, `&` or `nohup`: pass `--cwd DIR` for a subdirectory; it runs them in an interactive terminal in the user's Right Panel and prints its `pty_id`. Read the output with `burrow tab-output <pty_id>`. Calling it again with the same command reuses (and restarts, if stopped) the same terminal.
