# PROMPT — Burrow Go+Wails rewrite: restore original UI 1:1 + add 3 features

> Paste everything below the line into Claude Code, running **on branch `feat/wails-rewrite`**.

---

You are working on **Burrow**, a desktop IDE that runs AI coding agents in terminal tabs. There are TWO codebases in this repo:

- **`src/` + `src-tauri/`** (branch `main`) — the ORIGINAL, shipping Tauri v2 app. Vue 3 frontend with **bespoke scoped CSS** in each `.vue` file, **`@phosphor-icons/vue`** icons, xterm.js. This is the **visual source of truth**.
- **`burrow-wails/`** (this branch, `feat/wails-rewrite`) — a Go+Wails rewrite. Go backend is solid (~22 files, builds clean, ~60% of the Tauri command surface ported). BUT the frontend was rebuilt with **shadcn-vue + tailwind v4 + reka-ui + lucide** and now **looks completely different** from the original. That divergence is the problem.

**Your job, in order:**
1. Make the Wails frontend look and behave **1:1 identical to the original `src/` app**.
2. Then add 3 new features.

**Hard rules:**
- **Do NOT restart or rewrite the Go backend.** It works. Only touch Go when a new feature requires it (Phase 5–7). Keep the 62 bound methods in `burrow-wails/main.go` and the `burrow-wails/frontend/src/lib/invoke.ts` wrappers as the data layer.
- **Original `src/` is the design authority.** When in doubt about layout, spacing, color, icon, or behavior, open the matching original component and copy it.
- **Work phase by phase. After every phase: build, run, visually verify against the original, then `git commit`.** Do not batch phases into one giant change — that is why the previous attempt drifted. Small, verified, committed steps.
- Verify builds with: `cd burrow-wails && go build ./...` and `cd burrow-wails/frontend && npm run build` (runs `vue-tsc --noEmit && vite build`). Both must exit 0 before you commit.
- Comparison tip: `git show main:src/components/<Name>.vue` prints the original without switching branches.

---

## Phase 0 — Orient & make a parity map (no code changes)

1. List original components: `git show main:src` isn't recursive; use `git ls-tree -r main --name-only | grep '^src/components'`. Read `src/App.vue`, `src/components/Sidebar.vue`, `TitleBar.vue`, `Terminal.vue`, `ClaudeChat.vue`, `Settings.vue`, `Spotlight.vue`, `RightPanel.vue`, and `src/styles/status-dots.css`.
2. List the current Wails components under `burrow-wails/frontend/src/components/`.
3. Produce a written **parity map**: for each screen/region (title bar, activity bar, left sidebar, terminal tabs, chat, right panel, spotlight, settings, kanban, git manager, toasts, update banner, pet overlay), note: original look vs current look, and what diverged (icon set, spacing, colors, component structure, dropped panels).
4. Note **structurally dropped** pieces the rewrite removed that exist in the original — at minimum: the **RightPanel** (file tree + git panel), the **Explorer file tree**, **CodeEditor** tabs, and **FloatChat/float windows**. Flag each as "restore" or "confirm with user" — do NOT silently keep them dropped.

Output the parity map as a markdown file `burrow-wails/UI-PARITY.md` and commit it. This is your checklist for Phases 1–3.

## Phase 1 — Restore the visual foundation

The look diverged because the rewrite adopted a utility-class design system. Restore the original's foundation so components can't re-drift:

1. Port the original global styles and CSS custom properties (colors, fonts, spacing, radii, the status-dot styles in `src/styles/status-dots.css`, and any `:root` tokens / global CSS in `src/App.vue` and `src/main.ts`). Recreate them in the Wails frontend global stylesheet.
2. Switch the icon set back to **`@phosphor-icons/vue`** to match the original glyphs (the rewrite uses lucide). Add the dep, replace lucide imports as you touch each component.
3. Decide the tailwind/shadcn question and state your choice: prefer **removing tailwind + the `ui/` shadcn library** and returning to per-component scoped CSS like the original, since that is what makes it truly 1:1 and prevents future drift. If you keep any `ui/` primitive for convenience, it MUST be restyled to be visually indistinguishable from the original — no default shadcn look anywhere.
4. Build, run, confirm the app chrome (background, fonts, accent colors) now matches the original. Commit.

## Phase 2 — Port components 1:1 (one at a time)

For each component in the parity map, in this order — TitleBar, ActivityBar/Sidebar, TerminalTabs + XTerm, ClaudeChat, Spotlight, Settings, then the rest:

1. Open the original `src/components/<Name>.vue`. Copy its **template + scoped `<style>` verbatim** as the target look.
2. Keep the Wails component's **data wiring** (calls into `lib/invoke.ts`, Wails events, stores). Only the markup + styles change to match the original.
3. Match: layout, spacing, hover/active states, status-dot rendering, empty states, keyboard shortcuts (`⌘,` settings, `⌘P` spotlight, `Ctrl+1-9` tabs, `⌘1-9` workspace switch), icons.
4. Build + run + eyeball against original. Commit per component (e.g. `fix(wails): restore TitleBar to original 1:1`).

Preserve original status-dot semantics exactly: `idle | running | waiting | permission | done | review | error` with the original colors (permission = amber pulse + bell, error = red pulse persist-until-seen, review = green pulse persist-until-seen, done = transient lime). See original `Terminal.vue` / `terminalStatus.ts` for `STATUS_PRIORITY`.

## Phase 3 — Restore dropped panels & features

From the Phase 0 flags, restore what the rewrite removed so the app matches original structure: RightPanel (file tree + git), Explorer file tree, CodeEditor tabs, FloatChat/float windows if the user wants them. For any that need a missing Go backend method (e.g. float windows, LSP, MCP, skills/config dirs — these are confirmed absent in the Go port), STOP and list them for the user rather than half-implementing. Build + commit.

---

# NEW FEATURES (do only after UI parity is done & committed)

These are ported from a competitor (`herdr`) — **reimplement cleanly from these specs, do not copy its AGPL code.**

## Phase 4 — Screen-scrape status detection via TOML manifests

Today status comes only from agent hooks (Claude/Codex `settings.json`). Add a **screen-based** detector so status works for ANY agent with zero per-agent config, and as a self-healing override when hooks miss.

**Model:** 4 states `Idle | Working | Blocked | Unknown`. "Needs attention" = `Blocked`. "Finished while away" is NOT a new state — carry a `seen` boolean; `Idle && !seen` renders as the existing `review`/`done` surfacing.

**Mechanism:**
1. **Per-agent TOML manifests** describing ordered detection rules. Each rule: a screen `region`, a `priority`, the `state` it emits, optional flags (`visible_blocker`, `visible_idle`, `visible_working`, `skip_state_update`), and a gate of matchers (`contains` / `regex` / `line_regex`, composable with `all`/`any`/`not`). Highest-priority matching rule wins. Ship manifests as files (embed defaults via Go `embed`, allow user override in the config dir, allow versioned remote update later). Sandbox limits: max rules, gate depth, matcher count/length.
2. **Semantic screen regions**, not "last N lines": `whole_recent`, `bottom_non_empty_lines(n)`, `after_last_horizontal_rule`, `prompt_box_body`, `above_prompt_box`, `osc_title`, `osc_progress`. The prompt-box regions find the boxed input by locating the 2nd-from-bottom `───` rule. Capture OSC 0/2 (title) and OSC 9 (progress) from the PTY stream and expose them as `osc_title` / `osc_progress`.
3. **Arbitration:** `live visible blocker > lifecycle hook > screen-scrape`. A `visible_blocker` rule matching on screen overrides even a hook that says "working". This kills stuck dots.
4. **Debounce:** hold a Working→plain-Idle transition until N confirmations within ~700ms (bypass if it's a `visible_idle` live prompt box). A 3s startup grace after an agent is first detected. Skip re-scan when the bottom-buffer content hash is unchanged.

**Verified Claude Code manifest facts (confirmed empirically against real Claude, July 2026):** Claude sets the **terminal title** (OSC) to a **braille spinner char** while working and **✳ (U+2733)** when idle. So:
- working rule: `region = osc_title`, `regex = ['^[\x{2800}-\x{28FF}] ']`, `priority` highest, `visible_working = true`.
- idle rule: `region = osc_title`, `regex = ['^\x{2733} ']`.
- blocked rules: match the permission chrome on screen — `contains = ["do you want to proceed?"]` with a Yes/No line (`^\s*❯?\s*yes\b`, `^\s*2\.\s*no\b`), and selection prompts (`"enter to select"` + `"esc to cancel"` + navigation hints), `visible_blocker = true`.
- viewer screens (`"showing detailed transcript"`, model picker) → `skip_state_update = true`.
- The on-screen star cycle `✢ ✳ ✶ ✻ ✽` ("Moseying…"/"Hatching…") is visual noise — read the TITLE, not those.

Wire the detector's output into the existing status pipeline alongside the hook server (`hookserver.go` / `statushooks.go`), respecting the arbitration order. Build + verify status dots still work for a live Claude tab (hook path) AND for an agent with no hook (screen path). Commit.

## Phase 5 — Agent auto-resume across app restart

**Prerequisite / context:** the rewrite runs PTYs **in-process** (`spawnpoll.go:167` — no daemon), so PTYs die when the app closes. Full session survival (live-handoff of PTY fds) is a bigger effort; the pragmatic, high-ROI win is **conversation resume**: re-launch each agent with its native `--resume` on next start.

1. Capture each agent's native session id from its hook reports (Claude/Codex already emit `session_id` / session refs via the hook events the app receives). Persist per-tab: `{agent_kind, session_id, cwd}` into SQLite alongside the tab row.
2. On app start, when restoring a tab that had an agent, spawn the shell and type the resume command for that agent kind: `claude --resume <id>`, `codex resume <id>`, `copilot --resume=<id>`, `cursor-agent --resume`, etc. Keep a small kind→command table. **Treat the id as data — never shell-interpolate it unsafely** (pass as an argv arg, quote it).
3. Guard: only resume once per session id (dedupe), and only if `resume_agents_on_restore` pref is on (default on). Wait for the terminal to be ready before typing.
4. Also persist + replay **scrollback** so a restored tab shows its prior transcript instead of a blank screen (save rendered ANSI history per pane to SQLite, repaint on restore).

Build + verify: start an agent, quit the app, reopen → the agent tab relaunches and resumes its conversation. Commit.

## Phase 6 — Orphaned-worktree recovery (fixes a known bug)

Burrow's known bug: a raw `git worktree remove` can leave a stale DB row / orphaned checkout dir. Make removal robust in `worktree.go`:

1. Before removing, **shut down the worktree's PTYs** so the checkout isn't held open.
2. Try `git worktree remove` **without `--force` first**. Only if git errors with "contains modified or untracked files" (dirty), surface an explicit second confirmation to the user, then retry with `--force`. No blanket force.
3. **Orphan recovery:** if git says "not a working tree" but the dir still exists, read the checkout's `.git` file, resolve the `gitdir:` pointer, and `rm -rf` the dir **only if** that gitdir points back into THIS repo's worktree admin dir (`.git/worktrees/...`). Never nuke a dir whose gitdir points elsewhere.
4. Always delete the matching DB row after a successful/ recovered removal so no stale entry remains.

Build + verify: create a worktree, corrupt/orphan it, run remove → clean recovery, no stale row. Commit.

## Phase 7 — Wrap up

Update `burrow-wails/UI-PARITY.md` to check off completed items, note any still-missing backend subsystems (LSP, MCP mgmt, float windows, skills/config, daemon-backed PTY persistence) as follow-ups, and summarize what was done. Final build (`go build ./...` + `npm run build`, both exit 0). Commit.

---

**Remember:** UI parity first and fully committed before any feature work. Small verified commits. Original `src/` is the truth. Don't touch the Go backend except where Phases 5–6 require. Reimplement herdr's ideas from spec, not its code.
