# 008 — Git window looks like GitPaw

The pop-out Git window (`#/window/git`, `WindowApp.vue`) gets GitPaw's layout and look.
Reference app (React, read-only): `~/code/fun/ai-apps/GitPaw` — `src/mainview/components/{Sidebar,Toolbar,History,Changes,DiffView,ui}.tsx`,
`src/mainview/index.css`, `gitpaw_brand_guidelines.md`. Port the **look and layout** faithfully, in Vue 3 `<script setup lang="ts">`.

The Right Panel keeps the narrow `GitPanel.vue` (size `sm`), only restyled.

## Layout (window)

```
┌ GitSidebar (≈260px) ┬ tabs [Historie | Změny n] ─────── GitToolbar: Fetch Pull Push | Stash Branch ┐
│ project header      │ GitHistory  (graph list  │  commit detail: message, author, hash, files, diff) │
│ Branches (n)  +     │   or                                                                          │
│ Worktrees (n)       │ GitChanges  (staged/unstaged + hunks, stacked diffs, commit composer)        │
│ Stashes / Tags      │                                                                               │
└─────────────────────┴───────────────────────────────────────────────────────────────────────────────┘
```

## File ownership (one owner per file — do not edit files you don't own)

| Owner | Files |
|-------|-------|
| shell | `src/components/git/GitWindow.vue`, `GitSidebar.vue`, `GitToolbar.vue`; the one-line swap in `src/WindowApp.vue` |
| history | `src/components/git/GitHistory.vue`, `GitCommitDetail.vue`; additive exports in `src/lib/gitGraph.ts` |
| changes | `src/components/git/GitChanges.vue`, `src/components/git/GitDiff.vue` (shared diff renderer, also used by history) |
| rp | `src/components/GitPanel.vue` (restyle only) |

`src/stores/git.ts` is shared: additive changes only (new functions/refs), never rename or change existing behaviour.

## Contract

- `GitWindow.vue` props `{ initialWsId?: number }`; owns workspace selection (same mechanism as `GitPanel.vue` today —
  `useGitStore()` holds one active `cwd`), the active tab (`"history" | "changes"`), and renders:
  - `<GitHistory :cwd="cwd" @open-changes="tab = 'changes'" />`
  - `<GitChanges :cwd="cwd" />`
- Children read repo state from `useGitStore()` and may call `run_git` / `run_git_input` themselves for what the store lacks.
- `GitDiff.vue` props `{ text: string; path?: string }` — unified diff text → GitPaw-style rendering (old/new line numbers, +/- tint).
  Optional hunk actions via props `{ hunkActions?: boolean }` + emits `stage-hunk(i)`, `unstage-hunk(i)`, `discard-hunk(i)`.
- Styling: Burrow theme tokens only (CSS vars already used in `GitPanel.vue`: `--text-muted`, `--border`, `--yellow`, Tailwind
  `text-foreground`, `bg-accent`, …) so every theme (light too) works. Map GitPaw's palette onto them; lane colours may stay as in
  `GitPanel.vue`'s `LANE_COLORS`. Czech UI strings as in GitPaw are NOT wanted — keep English labels.
- No new npm dependencies. Don't commit; the coordinator integrates and commits.
- Check: `pnpm exec vue-tsc --noEmit` clean for your files; `pnpm test` still green.
