# Thread sub-agents

An agent running in a chat thread can delegate work to sub-agents that belong to
*that thread*: they do not appear in the Sidebar as peers, they are read and
driven from the Right Panel, the parent can watch and steer them, and deleting
or archiving the parent takes them with it. Spawning one is primarily the
parent agent's own action (`burrow spawn` / the `spawn` MCP tool); the same path
is also reachable manually from the panel.

## Why

`spawn` today produces either a terminal tab or a top-level chat. Both land in
the Sidebar next to the thread that asked for them, so a thread that delegates
three tasks produces four sibling entries with no recorded relationship between
them. Nothing cascades on delete, nothing groups them, and the parent has no
way to send a follow-up to a chat sub-agent (`send_to_tab` is PTY-only) or to
wait on one (`wait_result` reads the PTY capture files a chat never writes).

## Data model

`chats` gains one column:

```sql
ALTER TABLE chats ADD COLUMN parent_chat_id INTEGER NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS chats_parent ON chats(parent_chat_id);
```

Existing rows migrate to `0`, which means top-level. The column, not a title
tag or a separate table: filtering the Sidebar, scoping the panel and cascading
a delete all read the same integer, and none of them can drift from a string.

`ListChats` returns children alongside top-level chats — one source of truth.
Filtering is the client's job: the Sidebar shows `parent_chat_id === 0`, the
Right Panel shows `parent_chat_id === activeChatId`.

**Depth is capped at one level.** A sub-agent calling `spawn` gets an error
(`sub-agent cannot spawn sub-agents`). Recursive agent trees run away in cost
and the panel is a flat list; if nesting is ever wanted, the column already
supports it and only this guard has to move.

A child is not a thread: it is absent from `chatActiveByWs` and from the route.
It exists in the Right Panel and nowhere else in the view state.

## Lifecycle

`DeleteChat(parentID)` collects `WHERE parent_chat_id = parentID`, runs the
existing per-chat teardown for each (kill the CLI process, drop `chat_stream`,
`chat_messages` and the `chat:<id>` phase row), then deletes the parent.
Archiving sets `archived_at` on children in the same statement.

Merely switching threads or unmounting the panel does **not** stop a child — a
running turn keeps streaming behind an unmounted view, which is what the chat
session registry already guarantees. Only the durable actions cascade.

## Spawn path

**Identifying the caller.** `addBurrowEnv` exports `BURROW_CHAT_ID` into a chat
agent's environment. `burrow-mcp` is a child of the CLI process and inherits it,
so the MCP door and the CLI door agree without a second mechanism. The `burrow`
CLI sends it in the POST body next to `cwd`, exactly as it already sends
`$BURROW_CWD`.

**Server.** `control.Core.spawn()` forwards `parent_chat_id` to the UI verb. A
non-zero parent forces `target=chat`: a sub-agent that belongs to a thread is a
chat, and letting it be a terminal tab would put it back in the Sidebar.

**UI (`controlBridge.spawn`).** The `chat` branch calls
`chats.create(workspaceID, { agentKind, parentChatId })` and deliberately does
**not** call `terminalTabs.openChat(...)` — that call is what puts a chat in the
Sidebar. It then appends a `kind: "subagent"` message to the *parent's*
transcript carrying `{ chatId, title, agentName }`, and the existing
`chats-changed` event repaints the panel.

**Errors.** Spawning from a child is refused (see depth, above). Spawning from a
terminal tab is unchanged — a tab has `BURROW_PTY_ID`, not a chat id, so it has
no parent and produces a top-level tab or chat as today.

**Manual spawn.** A `+` in the panel section header opens an agent picker and a
prompt box and calls the same `chats.create` path. One implementation, two
doors — the same rule the control registry already follows.

## Watching and steering

Three verbs, all `ScopeLocal`:

- **`agent_status`** — extended with `parent_chat_id` and a `children` list per
  chat. Status for chats comes from `phase-chat:{id}`, which Go already derives
  and persists (`chatPhaseEvent`) and which has had no frontend consumer until
  now: `running | waiting_input | waiting_approval | done | failed | stale`.
- **`chat_send`** (`chat_id`, `text`) — types a follow-up into a child's
  session, the same call the composer makes. This is what makes a child
  steerable mid-task rather than fire-and-forget.
- **`wait_result`** — gains a `chat_id` alternative to `token`. It blocks on the
  child's phase reaching `done`/`failed`/`stale` and returns the last assistant
  message from `chat_messages`. Resolved against `PhaseStore` server-side, so
  waiting does not require the child's view to be mounted.

`collect_results` gains the same chat path, with a `collected_at` column on
`chats` for idempotency — the role the `.done` files play for PTY captures.

The Manager primer is generated from the verb registry, so the new verbs
document themselves. `agentdocs/skills/burrow/SKILL.md` gains a paragraph: a
spawn from a chat is a sub-agent under your thread; watch it with
`agent_status`, correct it with `chat_send`, collect it with `wait_result`.

## UI

**The system message.** A `kind: "subagent"` chat message renders in
`AgentChat.vue` as one quiet row: robot icon, the task title, the agent name,
and a live status dot driven by `phase-chat:{chatId}`. Clicking it opens the
Right Panel on the `agents` surface with that child selected. The live dot in
the transcript answers the common question ("is it still running?") without
opening the panel at all.

**The `agents` surface**, two sections, both scoped to the active thread:

1. **Sub-agents** (new, interactive) — one row per child: title, agent, phase
   dot. Clicking expands that child's `AgentChat` *inside the panel*, the way
   `ManagerPanel` already mounts one; engaged children stay mounted under
   `v-show` so a busy child keeps streaming while you look at another. A back
   arrow returns to the list. `+` spawns manually.
2. **Task-tool invocations** (existing, read-only) — narrowed from
   workspace-wide to the active chat. Otherwise unchanged.

A child's `AgentChat` in the panel is a second consumer of the same keyed
`chatSession` registry, which is keyed by chat id and only evicts idle sessions,
so this holds. It must `release()` when the panel closes, or the child streams
into a view that no longer exists.

## Testing

- Go: delete and archive cascade to children; `wait_result` resolves from a
  chat's phase; `spawn` from a child is refused.
- `remoteapi.go`'s table gains the new methods — `TestRemoteSurfaceIsExhaustive`
  fails until someone decides, which is the point; `commandSurface.test.ts`
  covers the wire names from the other direction.
- Manual: spawn from a thread and see both the panel row and the transcript
  message; delete the parent and confirm the child's CLI dies.

## Out of scope

The mobile PWA (no child surface there), nesting deeper than one level, and
promoting a child to a top-level thread.
