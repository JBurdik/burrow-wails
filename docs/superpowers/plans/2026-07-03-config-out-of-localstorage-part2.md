# Move Remaining Config Out of localStorage (Part 2) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Finish what `docs/superpowers/plans/2026-07-03-config-out-of-localstorage.md` (Part 1) started. Part 1's Task 10 final sweep found real, non-ephemeral config and user data still in `localStorage`, in component files Part 1 never touched (only Pinia stores were in scope). This plan covers those.

**Architecture:** Same lane as Part 1 — `src/lib/config.ts`'s `configReady`/`getConfig`/`setConfig`/`migrateFromLocalStorage` (already built, do not recreate). Every task here is a mechanical port of the same pattern into `.vue` component `<script setup>` blocks instead of Pinia stores.

**Tech Stack:** Same as Part 1 — no new dependencies.

## Global Constraints

- No new npm or cargo dependencies.
- No test suite in this repo — verification is `pnpm build` plus, where possible, manual `pnpm tauri:dev` checks.
- Public component props/emits/exposed template refs must stay unchanged.
- Per-chat / per-workspace keyed values (e.g. `msgKey(chatId)`, `PERM_KEY(id)`) become per-key entries inside ONE config object for that concern (e.g. all chat message histories live under one `chatMessagesByChat: Record<string, Turn[]>` config key, not one config key per chat) — do not create unbounded numbers of top-level config keys.
- Draft-text keys (`burrow.draft.chat.*` in `TaskDetail.vue`, `ManagerBar.vue`, `Terminal.vue`, `boardTasks.ts`) and dev-only flags (`termRenderer.ts`'s `burrow.renderer`, `XTerm.vue`'s `burrow-debug`) are explicitly OUT of scope for this plan — leave them in localStorage.

---

### Task 1: Fix confirmation — verify the FloatBubble.vue regression fix

**Files:**
- None modified — verification only. (The fix itself was already dispatched and committed outside this plan's task sequence, as an emergency fix once discovered.)

- [ ] **Step 1: Confirm the fix commit exists and is correct**

Run: `grep -n "ws-icons\|useWorkspaceStore" src/components/FloatBubble.vue`
Expected: no `localStorage.getItem("ws-icons")` remains; the icon is read via `useWorkspaceStore().icons`.

- [ ] **Step 2: If Step 1 shows the fix is NOT yet applied, stop and alert the human** — do not proceed with the rest of this plan until it's confirmed fixed, since Task 2 below (ClaudeChat.vue) working correctly is not blocked by this, but leaving a known active regression unconfirmed is a bad state to keep building on top of.

---

### Task 2: `src/components/ClaudeChat.vue` — chat message history, per-chat ACP settings, permission mode

**Files:**
- Modify: `src/components/ClaudeChat.vue`

**Interfaces:**
- Consumes: `configReady`, `getConfig`, `setConfig`, `migrateFromLocalStorage` from `src/lib/config.ts`.
- Produces: no change to this component's props/emits.

This is the highest-value task in this plan — actual chat message content is at risk, not just settings.

- [ ] **Step 1: Read the current code**

Run: `grep -n "localStorage" src/components/ClaudeChat.vue` and read each site with surrounding context (the file is large — read function-by-function, not the whole file at once). You'll find these localStorage-backed concerns, each keyed by `props.chatId` (or a derived key function like `msgKey(chatId)`, `acpModeKey`, `acpModelKey`, `acpEffortKey`, `PROFILE_KEY`, `PERM_KEY`):
  - `msgKey(chatId)` — the full saved message/turn history for one chat (read ~1184, write ~1193, cleared ~907/2124)
  - `acpModeKey`/`acpModelKey`/`acpEffortKey` (props.chatId) — per-chat ACP session settings (write ~852/863/874, read ~1702/1706/1710)
  - `PROFILE_KEY(id)` — per-chat Claude profile selection (read ~983, write ~1008)
  - `MODEL_KEY` (global, not per-chat) — last-used model (read ~1021, write ~1051)
  - `PERM_KEY(id)` / `PERM_LAST_KEY` / `burrow.claude.dangerous.${id}` — per-chat permission mode + last-used-globally + a dangerous-mode flag (read ~1243/1246/1248, write ~2052/2053)
  - `DRAFT_KEY` — OUT OF SCOPE, leave as localStorage (ephemeral draft text, same as other draft keys across the codebase)

- [ ] **Step 2: Design the config key shape**

Use these config keys, each a `Record<string, T>` keyed by chat id (except `MODEL_KEY` which is global, not per-chat):
  - `"chatMessageHistory"`: `Record<string, unknown>` (whatever shape `msgKey` currently saves — inspect the actual `JSON.stringify(toSave)` call to get the exact shape, don't guess)
  - `"chatAcpSettings"`: `Record<string, { mode?: string; model?: string; effort?: string }>`
  - `"chatProfileSelection"`: `Record<string, string>`
  - `"chatLastUsedModel"`: `string` (global, matches old `MODEL_KEY`)
  - `"chatPermissionMode"`: `{ byChat: Record<string, string>; last?: string; dangerousByChat: Record<string, boolean> }`

- [ ] **Step 3: Migrate each concern**

For each concern, follow the same shape as Part 1's Task 9 (`src/stores/claudeChats.ts`, already merged) but adapted for per-key-inside-one-config-value instead of one whole ref: on `configReady.then()`, run `migrateFromLocalStorage` ONCE per legacy key pattern you find still present for ANY chat id (you'll need to enumerate what chat ids exist — check how `sessions`/chat ids are obtained elsewhere in this component or its parent, likely via `useClaudeChatsStore()` from the already-migrated `src/stores/claudeChats.ts`), copy each found value into the new `Record`-shaped config value under that chat's id key, then read/write through `getConfig`/`setConfig` on the whole record going forward (read the whole record, look up `[chatId]`, write the whole record back with that key updated).

This is more involved than Part 1's per-file 1:1 key swaps because multiple chats' data must collapse into one config key. Take your time getting the migration loop right — a bug here silently loses chat history, which is the exact failure mode that started this whole project.

- [ ] **Step 4: Verify**

Run: `pnpm build` — must pass with no new errors.
Then, if you have GUI access (`pnpm tauri:dev`): open an existing chat with history, confirm it still loads (proves migration worked), send a message, change model/permission mode, quit, relaunch, confirm all of it persisted and the message history is still there.

- [ ] **Step 5: Commit**

```bash
git add src/components/ClaudeChat.vue
git commit -m "refactor(ClaudeChat): move chat message history, ACP settings, profile and permission-mode selection from localStorage into config.json"
```

---

### Task 3: `src/components/ManagerBar.vue` — manager model, permission mode, panel height, worktree mode

**Files:**
- Modify: `src/components/ManagerBar.vue`

**Interfaces:**
- Consumes: same `src/lib/config.ts` exports.
- Produces: no change to props/emits.

- [ ] **Step 1: Read the current code**

Run: `grep -n "localStorage" src/components/ManagerBar.vue` and read each site. You'll find:
  - `MAP_KEY` — worktree-mode-by-repo map (read ~422, write ~425) — a `Record<string, boolean>` or similar, keyed by root repo id
  - `WT_KEY` — a single worktree-mode boolean (read ~471, write ~472) — check how this relates to `MAP_KEY`, they may be the same concern at two granularities
  - `MANAGER_MODEL_KEY` — selected model, global (read ~488, write ~500)
  - `PERM_KEY(sid)` / `PERM_LAST_KEY` — permission mode per session id + last-used (read ~533, write ~547/548)
  - `HEIGHT_KEY` — panel height, global (read ~668, write ~687)
  - `DRAFT_KEY` — OUT OF SCOPE, leave as localStorage

- [ ] **Step 2: Migrate**

Config keys: `"managerWorktreeModeByRepo"` (the map), `"managerModel"`, `"managerPermissionMode"` (`{ bySession: Record<string, string>, last?: string }`), `"managerPanelHeight"`. Follow the same `configReady.then()` + `migrateFromLocalStorage` + `getConfig`/`setConfig` pattern as Task 2. `HEIGHT_KEY` and `MANAGER_MODEL_KEY` are simple global scalars — straightforward 1:1 port like Part 1's Task 8. The worktree-mode map and permission mode need the same "enumerate existing legacy entries and fold into one Record" treatment as Task 2's per-chat concerns, but simpler (likely only a handful of repo/session ids to migrate, not full message history).

- [ ] **Step 3: Verify**

Run: `pnpm build`. Manual check if GUI available: change manager model, resize the panel, toggle worktree mode, quit, relaunch, confirm all persisted.

- [ ] **Step 4: Commit**

```bash
git add src/components/ManagerBar.vue
git commit -m "refactor(ManagerBar): move model/permission-mode/panel-height/worktree-mode from localStorage into config.json"
```

---

### Task 4: `src/components/TitleBar.vue` — usage profile, last "open in" target

**Files:**
- Modify: `src/components/TitleBar.vue`

**Interfaces:**
- Consumes: same `src/lib/config.ts` exports.
- Produces: no change to props/emits.

- [ ] **Step 1: Read the current code**

Run: `grep -n "localStorage" src/components/TitleBar.vue`. Two simple global scalars:
  - `USAGE_PROFILE_KEY` (read ~360, write ~365)
  - `"tb-last-open-in"` (read ~275, write ~628)

- [ ] **Step 2: Migrate**

Config keys: `"titlebarUsageProfileId"`, `"titlebarLastOpenIn"`. Simple 1:1 port, same pattern as Part 1's Task 8 (`update.ts`'s plain-string dismissed-version key is the closest precedent — these are also plain strings, not objects).

- [ ] **Step 3: Verify**

Run: `pnpm build`. Manual check if available: change usage profile and "open in" target, quit, relaunch, confirm both persisted.

- [ ] **Step 4: Commit**

```bash
git add src/components/TitleBar.vue
git commit -m "refactor(TitleBar): move usage profile and last open-in target from localStorage into config.json"
```

---

### Task 5: `src/components/Sidebar.vue` — collapse state

**Files:**
- Modify: `src/components/Sidebar.vue`

**Interfaces:**
- Consumes: same `src/lib/config.ts` exports.
- Produces: no change to props/emits.

- [ ] **Step 1: Read the current code**

Run: `grep -n "COLLAPSE_KEY\|localStorage" src/components/Sidebar.vue` (read ~440, write ~449) — a `Record<string, boolean>` of collapsed section/workspace ids.

- [ ] **Step 2: Migrate**

Config key: `"sidebarCollapseState"`. Simple 1:1 port (the whole record moves as one value, same as Part 1's Task 5 pattern — no per-key folding needed here since it's already a single Record).

- [ ] **Step 3: Verify**

Run: `pnpm build`. Manual check if available: collapse a section, quit, relaunch, confirm it's still collapsed.

- [ ] **Step 4: Commit**

```bash
git add src/components/Sidebar.vue
git commit -m "refactor(Sidebar): move collapse state from localStorage into config.json"
```

---

### Task 6: `src/composables/useAutoRefresh.ts` — refresh interval

**Files:**
- Modify: `src/composables/useAutoRefresh.ts`

**Interfaces:**
- Consumes: same `src/lib/config.ts` exports.
- Produces: no change to this composable's exported function signature.

- [ ] **Step 1: Read the current code**

Run: `cat src/composables/useAutoRefresh.ts` (it's short, ~40 lines per the earlier grep). Note: `storageKey` is a parameter to this composable (not a fixed constant) — check every call site (`grep -rn "useAutoRefresh(" src/`) to see what keys are actually in use across the app, since each caller may pass a different key.

- [ ] **Step 2: Migrate**

This composable is generic (parameterized by `storageKey`), so its config-backed replacement should be too: prefix the config key with a fixed namespace, e.g. `` `autoRefreshInterval.${storageKey}` ``, so different callers don't collide. Follow the same `configReady`/`getConfig`/`setConfig`/`migrateFromLocalStorage` pattern, adapted for a composable rather than a store (the composable itself calls these directly; there's no Pinia `watch` here, just wire the read at setup and the write at the same call site the old `localStorage.setItem` was).

- [ ] **Step 3: Verify**

Run: `pnpm build`. Manual check if available: change a refresh interval somewhere that uses this composable, quit, relaunch, confirm it persisted.

- [ ] **Step 4: Commit**

```bash
git add src/composables/useAutoRefresh.ts
git commit -m "refactor(useAutoRefresh): move refresh interval from localStorage into config.json"
```

---

### Task 7: Final sweep round 2

**Files:**
- None modified — verification only.

- [ ] **Step 1: Grep for remaining localStorage usage**

Run: `grep -rn "localStorage" src/ --include="*.vue" --include="*.ts"`
Expected remaining hits: only the explicitly-out-of-scope draft-text keys (`TaskDetail.vue`, `ManagerBar.vue`'s `DRAFT_KEY`, `Terminal.vue`'s `burrow.draft.chat.*`, `boardTasks.ts`) and dev-only flags (`termRenderer.ts`, `XTerm.vue`'s `burrow-debug`), plus the `src/lib/config.ts` helper itself (which legitimately reads/writes localStorage as its migration mechanism) and any comment-only mentions.

- [ ] **Step 2: Fresh-profile smoke test**

Run: `rm -rf ~/Library/WebKit/com.agenticide.app ~/Library/Caches/com.agenticide.app` then `pnpm tauri:dev` — confirms the original failure mode (wiping the WebKit cache dir) no longer loses chat history, manager settings, titlebar prefs, or sidebar collapse state, on top of what Part 1 already verified.

- [ ] **Step 3: Commit** (only if Step 1 turned up a real gap)

```bash
git add -A
git commit -m "chore: sweep remaining localStorage config usage (part 2)"
```
