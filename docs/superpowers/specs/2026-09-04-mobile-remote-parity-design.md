# Mobile PWA remote parity — design

**Status:** approved for planning
**Scope:** `src/mobile/*`, `src-wails/httpserver.go`, `src-wails/remote.go`

## Problem

`src/mobile/` (the tailnet PWA) is read-only in practice even though it looks
interactive:

- `store.ts` calls `remote_create_chat` / `claude_send` / `acp_send` over the
  `/ws` JSON-RPC channel. `HTTPServer.dispatch` (`httpserver.go:298-315`) has
  no case for any of them — every call returns `unknown command "..."`.
  `App.RemoteCreateChat` (`stubs.go:67`) returns `map[string]any{}`.
- There is no permission/plan UI or response path on mobile at all. Even a
  chat that could send would deadlock the first time the agent asks
  `can_use_tool` or the ACP equivalent.
- `TabStatus` (`store.ts:11`) only knows `idle|running|waiting|permission|done`.
  Desktop's priority is `error > permission > waiting > running > review >
  done > idle` (`terminalStatus.ts`), and `review` persists until a tab/chat
  is *seen* — that's what keeps a finished-but-unwatched turn visible without
  it also cluttering the list forever. Mobile has neither `review`/`error`
  nor a "seen" concept, so every past chat just accumulates in the list.
- `BurrowWsClient.connect` (`api.ts`) is one-shot: a dropped connection (any
  phone leaving wifi) requires manually re-opening the PWA.

Everything read-only already works: `list_workspaces`, `list_terminal_tabs`,
`write_pty`, `resize_pty`, `list_pty_sessions`, `remote_list_chats`, and the
event fan-out (`pty-hook-{id}`, `pty-data-{id}`, `chat-event-{id}`) already
broadcast to WS clients via `emitAll()` (`events.go`). This design only adds
the write path and the status/list model needed to make the existing read
surface actually usable.

## Amendment (found during planning)

`remote_create_chat` supports `agentKind: "claude"` only. Building an ACP/Codex
session needs `command`/`args`/`env`/`configDir` resolved from provider config
that today only exists client-side in `AgentChat.vue`'s `acpStartPayload()` —
porting that resolution to Go is real new logic, not a thin wrapper, so it's
deferred rather than folded silently into this pass. `agentKind: "codex"`
returns an explicit error; `ChatsView.vue`'s create form drops the agent
picker accordingly.

## Non-goals

- Native OS push notifications — in-page/toast only, requires the PWA open.
- Full raw-protocol parsing on mobile (Claude `system/init`, ACP handshake
  details) — only enough of `claude-data-{id}` / `acp-req-{id}` to detect and
  answer a `can_use_tool` / `request_permission` / `ExitPlanMode` request.
- Live-syncing a mobile-created chat into the desktop Sidebar instantly — it
  becomes visible next time desktop reloads `chatSessions` from config. No
  new desktop-side event bus for this.
- Rebuilding `TerminalView.vue` — it already works (`write_pty`/`resize_pty`
  are already dispatched). It only gains the shared status colors + the
  store's reconnect handling, described below.

## Architecture

One subsystem, three phases, each independently testable/shippable in order:
Go write path → store parity → view cleanup. Phase 2 depends on Phase 1's
commands existing; Phase 3 depends on Phase 2's store fields.

### Phase 1 — Go: `/ws` write path (`src-wails/httpserver.go`, `remote.go`)

Add cases to `HTTPServer.dispatch`, each a thin wrapper over an `App` method
that already exists and is already used by the desktop bindings — no new
agent-control logic, just exposing it on the tailnet transport:

| WS command | Args | Calls |
|---|---|---|
| `claude_send` | `{id, text, sessionId}` | `App.ClaudeSend(id, text, sessionId, nil)` |
| `acp_send` | `{id, text}` | `App.AcpSend(id, text, nil)` |
| `claude_respond_control` | `{id, requestId, response}` | `App.ClaudeRespondControl(id, requestId, response)` |
| `acp_respond_permission` | `{id, rpcId, optionId}` | `App.AcpRespondPermission(id, rpcId, optionId)` |
| `remote_create_chat` | `{workspaceId, agentKind}` | new `App.RemoteCreateChat` body, below |

`wsArgs` gains the new string/number/map fields these need (`Text`,
`SessionId`, `RequestId`, `Response map[string]any`, `RpcId int64`,
`OptionId`, `AgentKind`).

**`RemoteCreateChat` real implementation** replicates what
`claudeChats.ts#create()` does client-side, since a chat's persisted shape
(`config.json` → `chatSessions` / `chatMessageHistory`, keyed by the
`chatIdCounter` config entry) has no server-side equivalent to call into:

1. Read config, bump `chatIdCounter` (mirrors `COUNTER_KEY`), build a session
   row: `{id, workspaceId, claudeSessionId: "", title: "Chat N", busy: false,
   messageCount: 0, agentKind, transport, lastActivityAt}` — `transport` is
   `"claude-cli"` for `agentKind == "claude"`, `"acp"` otherwise (mirrors
   `chatTransportFor`).
2. Append to `chatSessions`, seed `chatMessageHistory[id] = []`, write config
   back (same read-modify-write the desktop's `setConfig` does — single
   writer risk is pre-existing and out of scope here).
3. Call `App.ClaudeStart(...)` (claude-cli) or the ACP session-start path
   (acp.go) with the workspace's cwd, exactly as `AgentChat.vue` does on
   mount for a freshly created session.
4. Return the same shape `RemoteListChats` returns for one chat, so
   `store.ts`'s `createChat` can push it straight into `chats.value`.

Router only — every state mutation still happens through the existing
`ClaudeStart`/`ClaudeSend`/`ClaudeRespondControl`/`AcpSend`/
`AcpRespondPermission` functions the desktop already exercises, so this
phase carries little new risk beyond wiring and the session-row bootstrap in
step 1-2.

### Phase 2 — `src/mobile/store.ts` parity

- Extend `TabStatus` to `idle|running|waiting|permission|review|done|error`
  and add the same priority order as `terminalStatus.ts` for anywhere
  multiple statuses need collapsing to one (list sorting, dashboard summary).
- `watchTabStatus`/chat equivalents: a turn that completes while the chat/tab
  isn't the one currently open goes to `review` instead of transient `done`;
  opening it calls a new `markSeen(id)` that drops it back to `idle`. This is
  the mechanism that actually declutters the list — "settled" stops meaning
  "still shown forever" and starts meaning "collapsed until you look".
- For each open `RemoteChat`, also subscribe to `claude-data-{id}` (Claude)
  or `acp-req-{id}` (ACP) — the raw channels already broadcast by
  `emitChatLine`/`emitAll`. Parse just enough to recognize a control/permission
  request (`can_use_tool`, `ExitPlanMode`, ACP `session/request_permission`)
  and store it as `chat.pendingPermission`. This is a deliberately narrow
  reader: unrecognized lines are ignored, not stored, matching the "only
  enough to unblock the turn" non-goal above.
- `respondPermission(chat, allow)` on the store calls
  `claude_respond_control` or `acp_respond_permission` depending on
  `chat.transport`, then clears `chat.pendingPermission`.
- `BurrowWsClient`/store reconnect: on `onClose`, retry `connect()` with
  capped exponential backoff (1s → 2s → 4s… max 30s) while `view` isn't
  `connect`, surfaced as a `reconnecting` ref the views can show a banner
  for. Stops retrying on an explicit `disconnect()`.

### Phase 3 — Views cleanup

- `DashboardView`/`ChatsView`/`SessionsView`: sort by status priority (from
  Phase 2), and collapse anything at `idle`/`done`-and-seen into the existing
  "Hotovo/Nedávno hotovo" collapsed section pattern `DashboardView` already
  has for tabs — extended to chats too. This is the direct fix for "spousta
  chatů i těch settled".
- `ChatView.vue`: render `chat.pendingPermission` as a fixed banner above the
  composer with Allow/Deny (and Allow-always where the request supports it,
  mirroring `AgentChat.vue`'s `respondPermission` options) — simplified to
  two/three buttons, no dropdown.
- `TerminalView`/status dot everywhere `s-dot` is used: add `.review`/`.error`
  classes (colors already defined as CSS vars in `App.vue`, just unused by
  mobile markup today).
- Show the `reconnecting` state from Phase 2 as a small persistent banner
  (not a full-screen block) so an in-progress chat read stays visible while
  the socket recovers.

## Data flow (chat send → permission → resume)

1. User types in `ChatView` → `store.sendChat` → WS `claude_send`/`acp_send`
   → `App.ClaudeSend`/`AcpSend` (unchanged desktop code path).
2. Agent needs a tool decision → Go's existing control/ACP handling writes
   `claude-data-{id}`/`acp-req-{id}` → `emitAll` → WS broadcast (already
   works today, just unconsumed by mobile).
3. Store's raw-channel subscriber (Phase 2) recognizes the request → sets
   `chat.pendingPermission` → `ChatView` shows the banner (Phase 3).
4. User taps Allow/Deny → store calls `claude_respond_control` /
   `acp_respond_permission` (Phase 1) → same Go path the desktop uses →
   agent resumes → `chat-event-{id}` deltas keep streaming as today.

## Error handling

- Every new WS command follows the existing `{id, error}` reply shape
  (`httpserver.go:256-269`) — no new error format.
- `RemoteCreateChat` failing (bad workspace id, `ClaudeStart` error) returns
  the error to the WS caller instead of a partial session; `store.ts`'s
  `createChat` already surfaces `catch` into `createError` in `ChatsView`.
- Reconnect backoff (Phase 2) is capped and stops on explicit disconnect, so
  it can't spin forever against an intentionally-closed tunnel.
- A malformed/unrecognized raw-channel line (Phase 2 parser) is dropped
  silently — it's the same posture `applyEvent`'s `default` case already
  takes for `chat-event-{id}`.

## Testing

- Go: extend `remote_test.go` for `RemoteCreateChat`'s config read-modify-write
  (id allocation, session shape) using the existing pure-function-over-JSON
  pattern (`remoteChatsFromConfig`'s test style) — no real process spawn in
  the unit test; `ClaudeStart`/ACP start calls get mocked/skipped the way
  existing Go tests avoid spawning real CLIs.
- Manual verification (no automated UI test harness for `src/mobile/`
  today): pair a phone/browser tab against a local dev build, then walk
  send → permission prompt → allow → resume, create a new chat remotely,
  force a network drop mid-chat and confirm reconnect, and confirm a
  finished-but-unwatched chat shows `review` and collapses into "Hotovo"
  after being opened.
