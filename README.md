<p align="center">
  <img src="assets/logo-text.png" alt="Burrow — Run AI coding agents side-by-side" width="640">
</p>

<p align="center">
  <strong>A macOS-first desktop IDE that runs AI coding agents side-by-side in real terminal tabs.</strong>
</p>

<p align="center">
  <img alt="version" src="https://img.shields.io/badge/version-2.7.0-orange">
  <img alt="platform" src="https://img.shields.io/badge/platform-macOS-black">
  <img alt="stack" src="https://img.shields.io/badge/stack-Wails%20v2%20%C2%B7%20Vue%203%20%C2%B7%20Go-1a1a1e">
</p>

---

## What is Burrow

**Burrow** wraps real PTYs in a multi-workspace IDE shell, built to run AI coding agents — Claude Code, Codex, Aider, Copilot CLI — together in terminal tabs. Live status dots tell you which agent is working, waiting, blocked on permission, or done. Git worktrees give each branch its own isolated workspace. Agents can spawn sub-agents into new tabs and collect their results.

Subscription-safe: agents launch **interactively**, never via headless `-p` / Agent SDK.

<p align="center">
  <img src="assets/infographic.png" alt="Burrow feature overview" width="720">
</p>

## Features

- **Multi-agent terminals** — run several AI agents in parallel tabs, each in its own workspace.
- **Live status dots** — blue = waiting, amber = needs permission, green = done/review, red = error. Driven by global, env-aware status hooks, so status works for any agent session (button-launched, hand-typed, or reattached after restart).
- **Git worktrees** — isolated workspace per branch, nested under its repo in the sidebar.
- **Spawn sub-agents** — delegate work to fresh tabs via the `burrow` CLI; collect results back.
- **Native & fast** — Go/Wails v2 core, SQLite persistence, and a separate PTY daemon that keeps sessions alive across app restarts.
- **Manager** — a persistent, per-repository orchestration chat for delegating work, creating worktrees, and handling pull-request workflow.
- **Auto-update** — signed updates via GitHub Releases.

## Stack

Vue 3 + Pinia + xterm.js frontend · Go/Wails v2 backend · SQLite persistence.

## Development

```bash
# Native development window (hot reload)
just dev

# Frontend only (browser; no Wails backend)
just web

# Type-check frontend and Go backend; run tests
just check

# Full unsigned macOS build (desktop app + daemon + mobile bundle)
just build
```

`just` is the project task runner (`brew install just`). You can also start the
native app directly with `cd src-wails && wails dev`.

## Documentation

Standalone HTML reference (no build step — open in a browser):

| File | Covers |
|------|--------|
| `docs/context.html` | Whole-project overview: architecture, features, key files, Wails bindings, shortcuts |
| `docs/burrow.html` | The `burrow` CLI: spawn/wait/capture, agent-docs install |

See [`CLAUDE.md`](CLAUDE.md) for full architecture notes.
