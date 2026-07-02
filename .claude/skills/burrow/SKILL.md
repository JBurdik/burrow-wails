---
name: burrow
description: Delegate work to sub-agents by spawning new terminal tabs from inside the Burrow IDE. Use when the user asks to run work in parallel, hand a task to another agent, or when you want to fan out independent subtasks and collect their results without blocking.
---

# Delegating with `burrow`

You are running inside **Burrow**. You can delegate work to sub-agents in new tabs. The model is **fire-and-forget + collect**: spawn agents, keep doing your own work, then pull their results when you want — never sit blocked waiting on them.

## Prefer the Burrow MCP tools
If you have MCP tools whose names are `spawn`, `create_worktree`, `list_workspaces`, `collect_results`, `send_to_tab`, `wait_result`, `pr_create`/`pr_list`/`pr_view`/`pr_merge`, `focus_tab`, `focus_workspace`, `new_tab`, `worktree_remove` (served by the **burrow** MCP server), **use those** — they are typed, validated, and return structured results. `spawn` with `wait: true` blocks and returns the sub-agent's captured result directly (no separate collect step); omit `wait` to fire-and-forget and pick results up later with `collect_results`. The `burrow` shell CLI below is the **fallback** for shells and non-MCP contexts, and every CLI subcommand maps to an equivalent tool.

## Spawn sub-agents (fire-and-forget)
```
burrow spawn claude "write unit tests for src/foo"
burrow spawn codex "refactor the auth module"
```
Opens a new tab in the **current project** and runs the command interactively. Returns immediately.

## Fan out with tokens, then collect (non-blocking)
```
burrow spawn --token a claude "audit src/auth for bugs"
burrow spawn --token b claude "audit src/api for bugs"
# ...go do your own work in the meantime...
burrow collect a b      # prints results of whichever have FINISHED, and only those
burrow collect          # or: collect every finished sub-agent, no token list
```
`burrow collect` never blocks: it prints the final message of each finished token and **consumes** it, so a later `collect` returns only newly-finished ones. Tokens still running are reported as pending. Loop back and `collect` again later to pick them up — do useful work between calls, don't poll in a tight loop.

## Recap pattern
Spawn N agents up front → continue your task → near the end, `burrow collect` (optionally a few times as stragglers finish) → summarize what each returned for the user. You drive the recap; the sub-agents just drop their results for you.

## Worktrees
Create a git worktree off this repo on a new or existing branch — it shows up in the Sidebar nested under the repo, ready to open and run agents in.
```
burrow worktree feat/login                 # new branch off HEAD (or check out existing)
burrow worktree hotfix --base-ref main      # new branch based on main
burrow worktree feat/x --path ~/wt/x        # override the on-disk location
```
Use this to isolate a sub-task on its own branch/checkout instead of sharing the working tree. The worktree's disk path defaults to Burrow's configured worktrees dir.

## Status labels, progress & logs
```
burrow set-status "running tests..."   # show a label next to this tab's status dot
burrow set-status                        # clear the label
burrow trigger-flash                     # briefly flash this tab (visual ping)
burrow set-progress 0.4 --label "Building"  # show a progress bar in the tab (0.0–1.0)
burrow set-progress                      # clear the progress bar
burrow log -- "Compiled 12 files"       # append a timestamped log line below the tab bar
burrow log --level warn -- "Tests slow" # levels: info (default), warn, error
```
Use these to communicate progress to the user without printing to the terminal.

## Inspect what changed this turn
```
burrow diff --last-turn                  # git diff from HEAD at start of this turn
```
Shows exactly what files changed since the user submitted the prompt. Good for a quick sanity-check before reporting done.

## Monitor all terminals
```
burrow top                               # table of all live Burrow PTY sessions
```

## Inspect / other dir
```
burrow sessions            # list live sub-agent tabs (--count for just the number)
burrow spawn --cwd /path/to/other/project claude "..."
```

## Read & control the Burrow UI
Inspect and drive the app itself from the terminal — list workspaces/tabs, switch focus, open tabs.
```
burrow list-workspaces             # print every workspace: <id>\t<name>\t<path>
burrow list-tabs                   # print this workspace's tabs: <pty-id>\t<title>
burrow list-tabs --ws 3            # tabs of workspace 3
burrow focus-workspace 3           # switch Burrow to (and open) workspace 3
burrow focus-tab 42                # activate the tab with pty id 42 (switches workspace if needed)
burrow new-tab                     # open an empty terminal tab in this workspace
burrow new-tab --cmd "npm test"   # open a tab running a command
burrow new-tab --ws 3 --cmd htop   # open a tab in another workspace
```
`list-workspaces` / `list-tabs` are READ commands: they print tab-separated rows to stdout (parse the ids for the focus/new-tab commands). `new-tab` differs from `spawn`: `spawn` is for delegating sub-agents in the current project, `new-tab` is a plain UI action that can target any workspace by id.

## Limits & notes
- **Soft concurrency limit** (per workspace, default 3, set in Burrow Settings): `burrow spawn` prints the current cap. Respect it — don't exceed it. It is advisory, not enforced, so it's on you.
- Sub-agents run **interactively on the subscription**. Never pass `-p`/`--print`; never use the Agent SDK.
- Result capture works for `claude` sub-agents (via its Stop hook). Other agents spawn fine but only return a collectable result once they emit a done signal.
- `burrow wait <token>` still exists (blocks until one finishes) but prefer `collect` so you stay productive instead of blocked.

## Rules — when to use sidebar feedback

These rules apply to every task you run inside Burrow. Follow them unless the user explicitly says otherwise.

**Status label (`burrow set-status`):**
- Call `burrow set-status "<phase>"` at the start of any meaningful work phase (e.g. `"analyzing"`, `"running tests"`, `"applying fixes"`).
- Update the label when the phase changes (e.g. switch from `"analyzing"` to `"running tests"`).
- Clear it with `burrow set-status` (no arg) when your turn ends so the tab header returns to the agent status dot.
- Keep labels short — one or two words. The user reads them at a glance.

**Visual flash (`burrow trigger-flash`):**
- Call `burrow trigger-flash` once, at the very end of a turn, when you have finished a significant task and want to draw the user's attention to this tab (e.g. tests passed, a long refactor completed).
- Do NOT flash mid-turn or on trivial steps — it is a "done" signal, not a progress ping.
- Do NOT flash if the turn ended in an error or requires immediate user action (the status dot already signals that).

**Diff check (`burrow diff --last-turn`):**
- Before reporting a multi-file change as complete, run `burrow diff --last-turn` internally as a sanity check to confirm the expected files changed.
- You may skip this for single-file edits or when the user's request was purely read-only.

**Progress bar (`burrow set-progress`):**
- Use `burrow set-progress <0.0-1.0> --label "<phase>"` for tasks with measurable progress (running many tests, compiling many files, processing a list).
- Clear with `burrow set-progress` (no arg) when the task ends.
- Do NOT use for tasks where progress is not measurable — `set-status` suffices.

**Log strip (`burrow log`):**
- Use `burrow log -- "message"` to record key milestones that are worth keeping visible (e.g. "Compiled 12 files", "3 tests failed", "Wrote auth.ts").
- Use `--level warn` or `--level error` for problems.
- Do NOT log every step — only events the user would want to scroll back and read. Aim for 3-8 log lines per turn max.

**Example turn lifecycle:**
```
burrow set-status "analyzing"
burrow log -- "Reading 8 files"
# ...read files, understand the problem...
burrow set-status "fixing"
burrow set-progress 0.0 --label "Editing"
# ...make edits, update progress as files done...
burrow set-progress 1.0 --label "Editing"
burrow set-status "testing"
burrow set-progress 0.0 --label "Tests"
# ...run tests...
burrow log -- "All tests passed"
burrow set-progress          # clear
burrow diff --last-turn      # quick sanity check
burrow set-status            # clear — turn done
burrow trigger-flash         # ping user: "this tab finished"
```

## Generating diagrams
When asked to visualize architecture, flows, or data models, use Mermaid syntax wrapped in a `burrow diagram` call:
```sh
burrow diagram 'flowchart LR
A --> B
B --> C'
```
This renders an interactive SVG diagram in the Burrow UI. Pass the entire Mermaid source as a single argument (single-quoted). Any valid Mermaid diagram type works: flowchart, sequenceDiagram, classDiagram, erDiagram, gantt, etc.