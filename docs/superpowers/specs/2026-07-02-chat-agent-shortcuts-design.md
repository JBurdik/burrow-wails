# Chat agent launch shortcuts

## Problem

Terminal agent presets (`agents` store, ⌘⇧1-5) already support a configurable
launch shortcut. Chat agents (`chatAgents` store — ACP + native Claude chat
sessions) have no equivalent: there's no way to bind a key combo that opens a
new chat tab with a specific chat agent.

## Design

1. `ChatAgent` (`src/stores/chatAgents.ts`) gains a `shortcut: string` field
   (default `""`, like the terminal `AgentConfig`).
2. Extract `eventToShortcut()` (currently private to `Settings.vue`) into
   `src/lib/shortcuts.ts` as a shared export, alongside the existing
   `matchesShortcut()`. Reuse it from both `Settings.vue` and the new recorder
   in `ChatAgentConfig.vue` — no duplicated key-combo-capture logic.
3. `ChatAgentConfig.vue` gets a shortcut recorder control per agent in the
   list (same click-to-record / Esc-to-cancel UX already in `Settings.vue`'s
   terminal-agent list).
4. `terminalTabs.openChat(wsId, chatId?, agentId?)` — add an optional
   `agentId` to the request payload.
5. `Terminal.vue`'s `openClaudeChat(chatId?, agentId?)` forwards `agentId` as
   `chatsStore.create(workspaceId, { agentKind: agentId ?? uiStore.defaultChatAgent })`
   — only relevant when `chatId` is absent (new session path).
6. `App.vue`'s `onKeydown`: after the existing terminal-agent shortcut loop,
   add a loop over `chatAgents.agents` checking `matchesShortcut`. On match:
   ensure a workspace is active, then `termTabs.openChat(ws.active.id, undefined, a.id)`.
   **Always creates a new chat tab** — no reuse-existing-tab check (confirmed
   with user; mirrors the "New chat" button behavior, not the terminal
   agent-toolbar click behavior).

## Out of scope

- No dedup/focus-existing-chat behavior for the shortcut path.
- No shortcut conflict detection between the `agents` and `chatAgents` stores
  — first-checked-wins (terminal agents checked first, unchanged order).
