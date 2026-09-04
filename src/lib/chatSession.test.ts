import { describe, it, expect, vi, beforeEach } from "vitest";

const invoke = vi.hoisted(() => vi.fn());
vi.mock("@tauri-apps/api/core", () => ({ invoke }));
vi.mock("@tauri-apps/api/event", () => ({ listen: async () => () => {} }));

import { chatSession, dropChatSession, liveChatSessionIds, replayChatStream } from "./chatSession";

// The eviction rule is the whole point of the registry: an idle chat may be
// forgotten when nobody is looking at it, a busy one may not — that is what
// keeps a turn alive behind an unmounted view.
describe("chat session eviction", () => {
  it("forgets an idle chat once the last view releases it", () => {
    const s = chatSession(1);
    s.retain();
    s.release();
    expect(liveChatSessionIds()).not.toContain(1);
    // A later mount gets a fresh session, not the evicted one.
    expect(chatSession(1)).not.toBe(s);
    dropChatSession(1);
  });

  it("keeps a busy chat after release, and the same state is there on remount", () => {
    const s = chatSession(2);
    s.retain();
    s.busy.value = true;
    s.messages.value.push({ id: 0, role: "assistant", text: "mid-turn" });
    s.release();

    expect(liveChatSessionIds()).toContain(2);
    const again = chatSession(2);
    expect(again).toBe(s);
    expect(again.messages.value).toHaveLength(1);
    dropChatSession(2);
  });

  it("keeps a chat that is blocked on the user, even when idle", () => {
    const s = chatSession(3);
    s.retain();
    s.pendingQuestion.value = { requestId: "r1", toolName: "AskUserQuestion", input: {}, suggestions: [] };
    s.release();
    expect(liveChatSessionIds()).toContain(3);

    // Answering it makes the session evictable again.
    s.pendingQuestion.value = null;
    s.retain();
    s.release();
    expect(liveChatSessionIds()).not.toContain(3);
  });

  it("release is not driven negative by extra unmounts", () => {
    const s = chatSession(4);
    s.retain();
    s.retain();
    s.release();
    expect(liveChatSessionIds()).toContain(4); // one view still holds it
    s.release();
    expect(liveChatSessionIds()).not.toContain(4);
  });
});

describe("replay after restart", () => {
  beforeEach(() => invoke.mockReset());

  it("replays domain events, not raw lines", () => {
    // Raw would also re-open permission requests that were answered before the
    // restart; a replay should rebuild the transcript and nothing else.
    const s = chatSession(10);
    const seen: string[] = [];
    s.setHandlers({ onEvents: (b) => seen.push(...b.events.map((e) => e.type)) });
    invoke.mockImplementation(async (cmd: string) => {
      if (cmd === "chat_folded_ord") return 4;
      return [
        { ord: 4, events: [{ type: "text.delta", messageId: "c1", text: "a" }] },
        { ord: 6, events: [{ type: "tool.started", toolCallId: "t1", name: "Bash" }, { type: "turn.completed" }] },
      ];
    });

    return replayChatStream(10).then(async () => {
      expect(seen).toEqual(["text.delta", "tool.started", "turn.completed"]);
      expect(invoke).toHaveBeenCalledWith("load_chat_events_since", { chatId: 10, since: 4 });
      // The replayed batches count as folded on the next save.
      expect(s.lastOrd).toBe(6);

      // Idempotent: a second mount must not double-feed.
      await replayChatStream(10);
      expect(seen).toHaveLength(3);
      dropChatSession(10);
    });
  });

  it("does nothing for a chat with no folded mark", async () => {
    const s = chatSession(11);
    const seen: string[] = [];
    s.setHandlers({ onEvents: (b) => seen.push(...b.events.map((e) => e.type)) });
    // folded_ord 0 means "nothing folded" — replaying from 0 would duplicate a
    // history that chat_messages already holds.
    invoke.mockImplementation(async () => 0);
    await replayChatStream(11);
    expect(seen).toEqual([]);
    dropChatSession(11);
  });
});

describe("who counts as watching", () => {
  it("is nobody once the last view releases a busy chat", () => {
    // The component cannot answer this from its own props: an unmounted
    // component's props are frozen, so a turn finishing behind a closed view
    // would report itself watched and settle to a transient `done` that clears
    // itself — losing the "agent finished while you were away" dot.
    const s = chatSession(20);
    s.retain();
    expect(s.isWatched()).toBe(true);
    s.busy.value = true; // keeps the session alive past release()
    s.release();
    expect(s.isWatched()).toBe(false);
    dropChatSession(20);
  });

  it("stays watched while a second view still holds it", () => {
    const s = chatSession(21);
    s.retain();
    s.retain();
    s.release();
    expect(s.isWatched()).toBe(true);
    s.release();
    dropChatSession(21);
  });
});
