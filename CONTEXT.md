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

**Chat runtime**:
The Go-side owner of a chat's agent process, keyed by chat id: ensure started, send, restart, clear, interrupt. Works with no client attached, so `spawn`, `chat_send` and the phone need no mounted view. The chat session renders and answers; it does not start processes.
_Avoid_: chat process manager, runtime (alone)

**Launch settings**:
What a chat's process is started with — model, effort, permission mode, provider profile, appended system prompt. Stored on the chat row, shared across devices. Defaults for a *new* chat ("last used") stay per device.
_Avoid_: chat config, chat options

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

**Settled**:
A thread that needs no attention now (snoozed, settled by hand, or 3 days quiet with no open ask); Go computes when it starts (`settled_since`), a client adds only "not while in flight". Settled threads leave the active lists; archived is a separate, explicit step.
_Avoid_: done, idle

**Read receipt**:
The per-client, per-subject record of when the user last saw a PTY or chat, used to turn `done` into `review`.
_Avoid_: seen flag, unread

**Transcript**:
A chat's rendered history (user rows, replies, tool calls, thinking, notes). Go folds the agent stream into it and is its only writer (ADR 0001); clients only apply splices.
_Avoid_: messages (the DB table), chat log

**Splice**:
A change to the transcript sent to clients: `{rev, fromIndex, rows}` = "from this index on, the transcript is these rows". Applied only when its revision is newer than the client's.
_Avoid_: delta, patch

**Splice revision**:
The chat's `folded_ord`: the stream position the transcript was folded up to. Grows for the life of the chat; `chat_clear` does not reset it.
_Avoid_: version, seq (that is the shell stream's)

**Submit door**:
`chat_submit`: the one way a client sends a user message. Go decides under the chat lock: a settled chat gets it now, anything else queues it. Steer is a separate, explicit intent.
_Avoid_: send (when the choice send-vs-queue is meant)

**Client id**:
The id a client gives a user message when it is typed; reused by every retry and stored on the user row, so a retry is deduplicated and the client's echo is matched to its row.
_Avoid_: request id, nonce

**Echo**:
The client's own copy of a just-submitted message, shown until the transcript row with the same client id arrives.
_Avoid_: optimistic message, pending bubble

### Chat content

**Visual block**:
A fenced ```` ```visual ```` (or ```` ```questions ```` / ```` ```mermaid ````) block in an agent reply that the chat renders as a rich element — chart, stat tiles, table, image, page card, or question form — instead of code. Plain JSON, so it works for every provider; agents learn the format from the `burrow-visuals` skill. An image part holds a file path, not the bytes.
_Avoid_: widget, artifact, rich block
