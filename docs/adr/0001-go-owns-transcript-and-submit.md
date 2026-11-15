# 0001. Go is the only writer of a chat's transcript and decides send vs queue

Date: 2026-11-15 · Status: accepted

## Context

A chat's transcript was folded three times: in Go (`chatfold.go`), on the desktop
(`chatProjection.ts` via `chatSession`), and reconciled again on iOS from Go's
unversioned tail splices. The desktop was also a second writer (`save_chat_messages`,
whole-transcript replace). Every client chose `chat_send` vs `chat_queue_add` from its
own copy of the phase, and the outbox / hold / offline rules existed in TS and Swift.
On 2026-11-15 this produced a run of iOS bugs: sent and steered messages vanishing
(an older splice replayed over a newer page), echoes matched by text, a
check-then-act race between send and queue.

## Decision

- **Go is the only writer of the transcript.** Clients never save it; rows they used
  to author go through Go (`turnMs` and permission receipts computed by Go, other notes
  via `publish_chat_note`). Every client applies Go's splices and nothing else.
- **Splices carry a revision**: the chat's `folded_ord`. Stream `ord` never resets
  (`chat_clear` keeps counting). Rewind and clear send an authoritative splice from the
  cut. Go coalesces a chat's splices to at most one per ~100 ms (latest wins).
- **One submit door**, `chat_submit(chatId, text, images, clientId)`: Go decides under
  the chat lock (settled → send, else queue). Steer stays a separate explicit call and
  also takes a `clientId`.
- **Client id on the user row**: the `chat-user` stream line is JSON `{text, images,
  clientId}` (plain-text lines still read), so dedupe survives a core restart and echoes
  match by id.
- Additive on the wire: `chat_send` / `chat_queue_add` stay for one release; no
  `minClientRev` raise.

## Consequences

- Ordering, staleness, dedupe and send-vs-queue are tested once, in Go, through the
  interface; client adapters are small and table-tested (vitest, XCTest).
- The desktop `chatSession` keeps listening to `chat-event` for live state only
  (pending requests, notifications, turn clock); its transcript comes from splices.
- Do not reintroduce a client-side fold or a client transcript writer to fix a display
  issue — fix it in Go's fold or in the splice adapter.
