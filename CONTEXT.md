# Burrow

A desktop IDE shell that runs AI coding agents side by side, as terminal tabs (PTYs) and as structured chats, reachable from the desktop and from a paired phone.

## Language

### Agents and their state

**Chat**:
A structured conversation with one agent CLI (Claude, an ACP agent, Codex), rendered as a transcript rather than a terminal.
_Avoid_: thread (UI word only), conversation

**Sub-agent**:
A chat that belongs to a parent chat; depth is capped at one level.
_Avoid_: child agent, worker

**Chat session**:
The per-chat owner of a chat's live stream: transcript, turn, queued follow-ups and every pending request. It lives independently of whether the chat is on screen.
_Avoid_: chat controller, chat store

**Turn**:
One prompt from the user and the agent's work up to its result, failure or interrupt.

**Pending request**:
Something the agent is blocked on until the user answers: a permission, a question, a plan approval, a diff approval, or a Codex user-input prompt.
_Avoid_: prompt (overloaded), dialog

**Phase**:
The server-derived state of a PTY or chat (`idle`, `running`, `waiting_input`, `waiting_approval`, `done`, `failed`, `stale`). It is the single source of truth for agent state and exists with no client attached.
_Avoid_: status (reserved for the displayed status)

**Displayed status**:
What a dot shows, derived per client from the phase, the read receipt and whether the user is watching; `review` exists only here.
_Avoid_: phase

**Read receipt**:
The per-client, per-subject record of when the user last saw a PTY or chat, used to turn `done` into `review`.
_Avoid_: seen flag, unread
