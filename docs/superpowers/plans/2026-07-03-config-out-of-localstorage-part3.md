# Move Remaining Config Out of localStorage (Part 3) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Two gaps found by Part 2's final sweep (Task 7), not fixed at the time — pick them up here.

**Architecture:** Same `src/lib/config.ts` mechanism as Parts 1-2 (`configReady`/`getConfig`/`setConfig`/`migrateFromLocalStorage`).

## Global Constraints

- No new npm or cargo dependencies.
- No test suite — verify with `pnpm build` + manual `pnpm tauri:dev` where possible.
- Component props/emits must stay unchanged.
- `src/components/TaskDetail.vue` and `src/stores/boardTasks.ts` may carry unrelated human in-progress edits at execution time — do not touch them regardless of task scope.

---

### Task 1: `src/components/FloatChat.vue` — worktree mode + worktree map

**Files:**
- Modify: `src/components/FloatChat.vue`

- [ ] **Step 1: Read the current code**

Run: `grep -n "localStorage\|MAP_KEY\|WT_KEY" src/components/FloatChat.vue`. This is the same concern already migrated in `src/components/ManagerBar.vue` (Part 2 Task 3, commit 65fa557) — `MAP_KEY` (worktree-session map keyed by root repo id) and `WT_KEY` (global worktree-mode boolean). Read ManagerBar.vue's current code for the already-migrated pattern (config keys used there, likely `"managerWorktreeSessionMap"`-equivalent and `"managerWorktreeMode"` — check the actual names in ManagerBar.vue at HEAD) to decide: is FloatChat.vue's `MAP_KEY`/`WT_KEY` the SAME localStorage key string as ManagerBar's (in which case reuse the same config key, like Part 2 Task 3 did for permission mode), or a distinct one (in which case give it its own config key, e.g. `"floatChatWorktreeMode"` / `"floatChatWorktreeSessionMap"`)? Verify by comparing the actual key string constants, don't assume.

- [ ] **Step 2: Migrate accordingly**, following the same pattern as ManagerBar.vue's Task 3.

- [ ] **Step 3: Verify**

Run: `pnpm build`. Manual check if available.

- [ ] **Step 4: Commit**

```bash
git add src/components/FloatChat.vue
git commit -m "refactor(FloatChat): move worktree mode/session-map from localStorage into config.json"
```

---

### Task 2: `src/components/Terminal.vue` — CHAT_TABS_KEY (open chat tabs per workspace)

**Files:**
- Modify: `src/components/Terminal.vue`

- [ ] **Step 1: Read the current code**

Run: `grep -n "CHAT_TABS_KEY\|localStorage" src/components/Terminal.vue`. Note: this file also has an explicitly-out-of-scope draft key (`burrow.draft.chat.${session.id}`, ~line 1673) — leave that one alone, only migrate `CHAT_TABS_KEY` (read ~1547, write ~1388), which tracks which chat ids are open as tabs per workspace — real UI state, not a draft.

- [ ] **Step 2: Migrate**

`CHAT_TABS_KEY(workspaceId)` is a per-workspace-id keyed family — fold into one config key `"openChatTabsByWorkspace"`: `Record<string, string[]>` (workspace id → array of chat ids), same per-id-family-to-Record pattern used in Part 2 Task 2 (ClaudeChat.vue) and Task 3 (ManagerBar.vue).

- [ ] **Step 3: Verify**

Run: `pnpm build`. Manual check if available: open a chat tab in a workspace, quit, relaunch, confirm the tab is still there.

- [ ] **Step 4: Commit**

```bash
git add src/components/Terminal.vue
git commit -m "refactor(Terminal): move open-chat-tabs-per-workspace from localStorage into config.json"
```

---

### Task 3: Final sweep round 3

- [ ] **Step 1:** `grep -rn "localStorage" src/ --include="*.vue" --include="*.ts"` — expected remaining hits: only explicitly out-of-scope draft keys, dev-only flags, legacy-migration code inside already-migrated files, and `src/lib/config.ts` itself.
- [ ] **Step 2:** Fresh-profile smoke test: `rm -rf ~/Library/WebKit/com.agenticide.app ~/Library/Caches/com.agenticide.app` then `pnpm tauri:dev`, confirm nothing regresses.
