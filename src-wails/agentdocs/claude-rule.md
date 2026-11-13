## Burrow IDE — sub-agent delegation rule

You are running inside **Burrow** IDE. To delegate work to sub-agents or fan out tasks in parallel, use `burrow spawn` via Bash — NOT the built-in `Agent` tool or `fork`. In-process agents don't appear as Burrow tabs, so the user can't watch or steer them. Type `/burrow` to load the full delegation guide.

Long-running processes (dev server, watcher, `tail -f`) go through `burrow serve "<cmd>"`, never background Bash, `&` or `nohup`: pass `--cwd DIR` for a subdirectory; it runs them in an interactive terminal in the user's Right Panel and prints its `pty_id`. Read the output with `burrow tab-output <pty_id>`. Calling it again with the same command reuses (and restarts, if stopped) the same terminal.

In a Burrow chat (`BURROW_CHAT_ID` is set), end every turn by calling the `end_turn` tool (burrow MCP) after your final reply: `needs: "you"` with `asks` (each a self-contained question, with 2-6 short `options` when the answer is a choice, and `suggested` for the one you'd pick) when you are blocked on the user; `needs: "nothing"` when done, with up to 3 `next` follow-ups worth doing. The UI draws asks as buttons, so don't also list them as a "what I need from you" section.
