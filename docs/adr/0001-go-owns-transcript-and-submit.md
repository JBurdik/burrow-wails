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
  cut. Streaming text goes as transient tail appends (below), coalesced to ~100 ms.
- **One submit door**, `chat_submit(chatId, text, images, clientId)`: Go decides under
  the chat lock (settled → send, else queue). Steer stays a separate explicit call and
  also takes a `clientId`.
- **Client id on the user row**: the `chat-user` stream line is JSON `{text, images,
  clientId}` (plain-text lines still read), so dedupe survives a core restart and echoes
  match by id.
- Additive on the wire: `chat_send` / `chat_queue_add` stay for one release; no
  `minClientRev` raise.

## Wire shape (phase B, after comparing with threadbear)

Threadbear (a similar product) keeps a per-thread gap-free `seq` and replays from the
last one a client saw; it has no message identity (matches echoes by text + time, which
is its weak spot). Taken from it:

- **Gap check**: every splice is `{baseRev, rev, fromIndex, rows}`. A client applies it
  only when `baseRev == its rev` (or the splice is authoritative: `fromIndex <= its
  window start`, e.g. rewind / clear); otherwise it drops it and catches up.
- **`chat_subscribe(chatId, sinceRev)`** → the splices since `sinceRev` (or one
  authoritative splice when they are gone) **plus `inProgress`**: the text streamed so far
  for the unfinished tail row, so a reconnect mid-stream loses nothing.
- **Streaming tail as an append**: while a row streams, Go sends transient
  `chat-tail-<id>` `{rev, index, append}` events (text appended to the row at `index`),
  coalesced to ≤ 1 per ~100 ms per chat, which never bump `rev`; the next rev'd splice
  replaces whatever they built. Under backpressure tail events are dropped first.
- **Images by reference**: no base64 in splices, `chat-user` lines or pages: a row
  carries `burrow-image://<index>/<n>` refs (or a stored-attachment id) and clients fetch
  each once (`load_chat_image`).

## Consequences

- Ordering, staleness, dedupe and send-vs-queue are tested once, in Go, through the
  interface; client adapters are small and table-tested (vitest, XCTest).
- The desktop `chatSession` keeps listening to `chat-event` for live state only
  (pending requests, notifications, turn clock); its transcript comes from splices.
- Do not reintroduce a client-side fold or a client transcript writer to fix a display
  issue — fix it in Go's fold or in the splice adapter.
