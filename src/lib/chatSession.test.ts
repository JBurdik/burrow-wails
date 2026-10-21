import { describe, it, expect, vi, beforeEach } from "vitest";
import { nextTick } from "vue";

const invoke = vi.hoisted(() => vi.fn());
vi.mock("@tauri-apps/api/core", () => ({ invoke }));
vi.mock("@tauri-apps/api/event", () => ({ listen: async () => () => {} }));

import { chatSession, dropChatSession, liveChatSessionIds, replayChatStream, type ChatSessionDeps, type ChatViewHooks } from "./chatSession";
import type { ChatHost } from "./chatHost";

/** A session wired to fakes: a listen that records handlers, a recording host. */
function fakeDeps() {
  const handlers = new Map<string, (e: { payload: unknown }) => void>();
  const host = {
    chat: vi.fn(() => undefined),
    syncChat: vi.fn(),
    recordTurn: vi.fn(),
    hasPermissionRule: vi.fn(() => false),
    addPermissionRule: vi.fn(),
    subagentStarted: vi.fn(),
    subagentCompleted: vi.fn(),
    notifyDone: vi.fn(),
    notifyPermission: vi.fn(),
  } satisfies ChatHost;
  const fakeInvoke = vi.fn(async (_cmd: string, _args?: unknown) => undefined as unknown);
  const deps = {
    invoke: fakeInvoke,
    listen: (async (name: string, cb: (e: { payload: unknown }) => void) => {
      handlers.set(name, cb);
      return () => handlers.delete(name);
    }),
    host,
  } as unknown as ChatSessionDeps;
  const emit = (name: string, payload: unknown) => handlers.get(name)?.({ payload });
  return { deps, host, invoke: fakeInvoke, emit };
}

function fakeView(over: Partial<ChatViewHooks> = {}): ChatViewHooks {
  return {
    scrollToBottom: vi.fn(), onQuestionOpened: vi.fn(), onPlanOpened: vi.fn(), onAcpSession: vi.fn(),
    restoreAcpSelections: vi.fn(), usesRpcRuntime: () => false, send: vi.fn(async () => {}),
    ...over,
  };
}

// Everything below drives the session through its public interface: intents
// in, stream lines in on the fake listeners, state out. Nothing writes a ref.
const raw = (l: object, ord = 1) => ({ ord, kind: "", line: JSON.stringify(l) });
type Fake = ReturnType<typeof fakeDeps>;
type Session = ReturnType<typeof chatSession>;

/** A native can_use_tool request, as the CLI sends it. */
async function nativeRequest(s: Session, f: Fake, toolName: string, input: Record<string, unknown> = {}) {
  await s.listenClaude();
  f.emit(`claude-data-${s.chatId}`, raw({ type: "control_request", request_id: `r-${toolName}`, request: { subtype: "can_use_tool", tool_name: toolName, input } }));
}
/** A request on the ACP / Codex request channel. */
async function acpRequest(s: Session, f: Fake, msg: object) {
  await s.listenAcp();
  f.emit(`acp-req-${s.chatId}`, raw(msg));
}
const codexApproval = (id: number) => ({ id, method: "item/commandExecution/requestApproval", params: { command: "go test", itemId: "i" } });
const codexInput = (id: number) => ({ id, method: "item/tool/requestUserInput", params: { questions: [{ id: "q", question: "Which?", options: [{ label: "A" }] }] } });
/** The turn ends the way Go reports it. */
async function endTurn(s: Session, f: Fake) {
  await s.listenEvents();
  f.emit(`chat-event-${s.chatId}`, { ord: 0, events: [{ type: "turn.completed" }] });
}
/** foldedOrd the next save hands to Go — lastOrd + 1, the trim-safety mark. */
function savedFoldedOrd(s: Session, f: Fake): number {
  s.save();
  const calls = f.invoke.mock.calls as unknown as Array<[string, { foldedOrd: number }]>;
  return calls.filter(([cmd]) => cmd === "save_chat_messages").pop()![1].foldedOrd;
}

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
    const s = chatSession(2, fakeDeps().deps);
    s.retain();
    s.startTurn();
    s.notice("mid-turn");
    s.release();

    expect(liveChatSessionIds()).toContain(2);
    const again = chatSession(2);
    expect(again).toBe(s);
    expect(again.messages.value).toHaveLength(1);
    dropChatSession(2);
  });

  it("keeps a chat that is blocked on the user, even when idle", async () => {
    const f = fakeDeps();
    const s = chatSession(3, f.deps);
    s.retain();
    await nativeRequest(s, f, "AskUserQuestion", { questions: [] });
    s.release();
    expect(liveChatSessionIds()).toContain(3);

    // Answering it makes the session evictable again.
    await s.respond({ kind: "question", answers: {} });
    s.retain();
    s.release();
    expect(liveChatSessionIds()).not.toContain(3);
  });

  // A Codex user-input prompt that arrives while the chat is unmounted used to
  // land in the dead view's own ref: the remount showed nothing, and the
  // session could be evicted while the agent sat blocked on an answer.
  it("keeps a chat blocked on a Codex user-input prompt, and the prompt survives a remount", async () => {
    const f = fakeDeps();
    const s = chatSession(4, f.deps);
    s.retain();
    s.release(); // the view is gone before the request arrives
    const again = chatSession(4, f.deps);
    await acpRequest(again, f, codexInput(9));
    again.retain();
    again.release();
    expect(liveChatSessionIds()).toContain(4);
    expect(chatSession(4).codexUserInput.value?.rpcId).toBe(9);
    dropChatSession(4);
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
    const f = fakeDeps();
    const { deps, invoke: fake } = f;
    const s = chatSession(10, deps);
    fake.mockImplementation((async (cmd: string) => {
      if (cmd === "chat_folded_ord") return 4;
      if (cmd === "load_chat_events_since") {
        return [
          { ord: 4, events: [{ type: "text.delta", messageId: "c1", text: "a" }] },
          { ord: 6, events: [{ type: "tool.started", toolCallId: "t1", name: "Bash" }, { type: "turn.completed" }] },
        ];
      }
      return undefined;
    }) as never);

    return replayChatStream(10).then(async () => {
      expect(s.messages.value.map((m) => m.role)).toEqual(["assistant", "tool"]);
      expect(fake).toHaveBeenCalledWith("load_chat_events_since", { chatId: 10, since: 4 });
      // The replayed batches count as folded on the next save.
      expect(savedFoldedOrd(s, f)).toBe(7);

      // Idempotent: a second mount must not double-feed.
      await replayChatStream(10);
      expect(s.messages.value).toHaveLength(2);
      dropChatSession(10);
    });
  });

  it("does nothing for a chat with no folded mark", async () => {
    const { deps, invoke: fake } = fakeDeps();
    const s = chatSession(11, deps);
    // folded_ord 0 means "nothing folded" — replaying from 0 would duplicate a
    // history that chat_messages already holds.
    fake.mockImplementation((async () => 0) as never);
    await replayChatStream(11);
    expect(s.messages.value).toEqual([]);
    expect(fake).not.toHaveBeenCalledWith("load_chat_events_since", expect.anything());
    dropChatSession(11);
  });
});

describe("who counts as watching", () => {
  it("is nobody once the last view releases a busy chat", () => {
    // The component cannot answer this from its own props: an unmounted
    // component's props are frozen, so a turn finishing behind a closed view
    // would report itself watched and settle to a transient `done` that clears
    // itself — losing the "agent finished while you were away" dot.
    const s = chatSession(20, fakeDeps().deps);
    s.retain();
    expect(s.isWatched()).toBe(true);
    s.startTurn(); // keeps the session alive past release()
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

describe("eviction after a turn ends behind a closed view", () => {
  it("keeps a busy session on release, then drops it once it settles", async () => {
    // release() cannot be the only decision point: a busy session is kept on
    // purpose, and if nothing looks again it sits in the registry with its
    // listeners and transcript until the chat itself is closed.
    vi.useFakeTimers();
    const f = fakeDeps();
    const s = chatSession(30, f.deps);
    s.retain();
    s.startTurn();
    s.release();
    expect(liveChatSessionIds()).toContain(30);

    await endTurn(s, f);
    s.maybeEvict();
    expect(liveChatSessionIds()).toContain(30); // deferred, not immediate
    vi.runAllTimers();
    expect(liveChatSessionIds()).not.toContain(30);
    vi.useRealTimers();
  });

  it("does not evict while a queued message is still waiting to be sent", async () => {
    // finishTurn flushes the queue a nextTick later; evicting in between would
    // hand that send a detached session while the next mount built a fresh one.
    vi.useFakeTimers();
    const f = fakeDeps();
    const s = chatSession(31, f.deps);
    s.retain();
    s.release();
    // release() already dropped it (idle, unwatched) — take a fresh one.
    const s2 = chatSession(31, f.deps);
    s2.retain();
    s2.startTurn();
    s2.release();
    await endTurn(s2, f);
    s2.enqueueMessage("next one");
    s2.maybeEvict();
    vi.runAllTimers();
    expect(liveChatSessionIds()).toContain(31);
    vi.useRealTimers();
    dropChatSession(31);
  });

  it("keeps a session someone is still watching", () => {
    vi.useFakeTimers();
    const s = chatSession(32);
    s.retain();
    s.maybeEvict();
    vi.runAllTimers();
    expect(liveChatSessionIds()).toContain(32);
    vi.useRealTimers();
    dropChatSession(32);
  });
});

describe("queued follow-ups", () => {
  it("keeps duplicate prompts distinct and preserves their images", () => {
    const s = chatSession(40);
    const first = s.enqueueMessage("run tests", ["data:image/png;base64,first"]);
    const second = s.enqueueMessage("run tests", ["data:image/png;base64,second"]);

    s.removeQueuedMessage(first.id);
    expect(s.messageQueue.value).toEqual([second]);
    expect(s.messages.value).toEqual([{ id: second.id, role: "queued", text: "run tests", images: second.images }]);
    dropChatSession(40);
  });

  it("reads persisted queue markers back in FIFO order with no restore step", () => {
    const s = chatSession(41);
    s.hydrate({ messages: [
      { id: 4, role: "queued", text: "first" },
      { id: 5, role: "assistant", text: "working" },
      { id: 6, role: "queued", text: "second", images: ["data:image/png;base64,x"] },
    ] });

    expect(s.messageQueue.value).toEqual([
      { id: 4, text: "first" },
      { id: 6, text: "second", images: ["data:image/png;base64,x"] },
    ]);
    // New rows continue past the loaded ids instead of colliding on `:key`.
    expect(s.enqueueMessage("third").id).toBe(7);
    dropChatSession(41);
  });

  it("sends the next follow-up when a turn ends, even with nobody watching", async () => {
    // The chat leaf is unmounted while the user looks at another tab, so the
    // drain cannot live in a component watcher — the turn that releases the
    // queue usually finishes right there.
    const f = fakeDeps();
    const s = chatSession(43, f.deps);
    const view = fakeView();
    s.attachView(view);
    s.retain();
    s.startTurn();
    await nextTick(); // let the flag settle, as a real turn's start does
    s.enqueueMessage("after this one");
    s.release(); // view unmounted mid-turn
    await endTurn(s, f);
    await nextTick();
    await nextTick();
    expect(view.send).toHaveBeenCalledWith("after this one", undefined);
    expect(s.messageQueue.value).toEqual([]);
    dropChatSession(43);
  });

  it("moves a message to the head of the queue in the transcript too", () => {
    const s = chatSession(42, fakeDeps().deps);
    const first = s.enqueueMessage("first");
    s.notice("working");
    const working = s.messages.value[s.messages.value.length - 1].id;
    const second = s.enqueueMessage("second");

    s.moveQueuedMessage(second.id, 0);
    expect(s.messageQueue.value.map((e) => e.id)).toEqual([second.id, first.id]);
    expect(s.messages.value.map((m) => m.id)).toEqual([second.id, first.id, working]);
    expect(s.messageQueue.value[0]).toEqual({ id: second.id, text: "second" });
    dropChatSession(42);
  });

  it("moves a queued message down (drag reorder)", () => {
    const s = chatSession(42, fakeDeps().deps);
    const [a, b, c] = ["a", "b", "c"].map((t) => s.enqueueMessage(t));
    s.moveQueuedMessage(a.id, 2);
    expect(s.messageQueue.value.map((e) => e.text)).toEqual(["b", "c", "a"]);
    s.moveQueuedMessage(a.id, 1);
    expect(s.messageQueue.value.map((e) => e.id)).toEqual([b.id, a.id, c.id]);
    dropChatSession(42);
  });
});

// The reducers, driven through the session's own seam: lines in on the
// listeners, state out. This is the surface AgentChat.vue used to hide.
describe("stream reducers", () => {
  const line = (l: object) => ({ ord: 1, kind: "", line: JSON.stringify(l) });

  it("opens a native permission request with a marker and a notification, and a cancel withdraws it", async () => {
    const { deps, host, emit } = fakeDeps();
    const s = chatSession(60, deps);
    s.attachView(fakeView());
    await s.listenClaude();
    emit("claude-data-60", line({ type: "control_request", request_id: "r1", request: { subtype: "can_use_tool", tool_name: "Bash", input: { command: "ls" } } }));
    expect(s.pendingPermission.value?.requestId).toBe("r1");
    expect(s.messages.value[s.messages.value.length - 1]?.text).toContain("Bash wants permission");
    expect(host.notifyPermission).toHaveBeenCalledOnce();

    emit("claude-data-60", line({ type: "control_cancel_request", request_id: "r1" }));
    expect(s.pendingPermission.value).toBeNull();
    expect(s.messages.value).toEqual([]);
    // A replay of the withdrawn request must not reopen it.
    emit("claude-data-60", line({ type: "control_request", request_id: "r1", request: { subtype: "can_use_tool", tool_name: "Bash", input: {} } }));
    expect(s.pendingPermission.value).toBeNull();
    dropChatSession(60);
  });

  it("answers an always-allowed tool itself, with no prompt", async () => {
    const { deps, host, invoke: fake, emit } = fakeDeps();
    host.hasPermissionRule.mockReturnValue(true);
    const s = chatSession(61, deps);
    await s.listenClaude();
    emit("claude-data-61", line({ type: "control_request", request_id: "r2", request: { subtype: "can_use_tool", tool_name: "Bash", input: { command: "git status" } } }));
    expect(host.hasPermissionRule).toHaveBeenCalledWith(["Bash", "Bash:git"]);
    expect(fake).toHaveBeenCalledWith("claude_respond_control", { id: 61, requestId: "r2", response: { behavior: "allow", updatedInput: { command: "git status" } } });
    expect(s.pendingPermission.value).toBeNull();
    dropChatSession(61);
  });

  it("keeps a Codex user-input request that arrives with no view mounted", async () => {
    const { deps, emit } = fakeDeps();
    const s = chatSession(62, deps);
    await s.listenAcp();
    s.retain();
    s.release(); // the view closes before the request arrives
    const again = chatSession(62, deps);
    await again.listenAcp();
    emit("acp-req-62", line({ id: 5, method: "item/tool/requestUserInput", params: { questions: [{ id: "q", question: "Which?", options: [{ label: "A" }] }] } }));
    expect(again.codexUserInput.value).toEqual({ rpcId: 5, questions: [{ id: "q", header: "Question", question: "Which?", isOther: false, isSecret: false, options: [{ label: "A" }] }] });
    again.retain();
    again.release();
    expect(liveChatSessionIds()).toContain(62);
    dropChatSession(62);
  });

  it("drops its own prompt's echo, and settles a finished turn with a notification", async () => {
    const f = fakeDeps();
    const { deps, host, emit } = f;
    const s = chatSession(63, deps);
    s.attachView(fakeView());
    await s.listenEvents();
    s.postUserMessage({ text: "hi" });
    s.startTurn();
    emit("chat-event-63", { ord: 2, events: [
      { type: "user.delta", messageId: "acp:user:2", text: "hi" },
      { type: "text.delta", messageId: "m", text: "hello" },
      { type: "tool.started", toolCallId: "t", name: "Bash" },
      { type: "turn.completed", inputTokens: 3, outputTokens: 4 },
    ] });
    expect(s.messages.value.map((m) => m.role)).toEqual(["user", "assistant", "tool"]);
    expect(s.busy.value).toBe(false);
    // A tool row with no completion is settled, not left spinning.
    expect(s.messages.value[2].toolOutput).toBe("");
    expect(host.recordTurn).toHaveBeenCalledWith(3, 4);
    expect(host.notifyDone).toHaveBeenCalledWith(63, false);
    expect(savedFoldedOrd(s, f)).toBe(3);
    // The turn clock stamped the bubble that opened the turn.
    expect(s.messages.value[0].turnMs).toBeGreaterThanOrEqual(0);
    dropChatSession(63);
  });

  it("does not toast a turn ended by our own restart", async () => {
    const { deps, host, emit } = fakeDeps();
    const s = chatSession(64, deps);
    await s.listenEvents();
    s.startTurn();
    s.suppressDoneToast();
    emit("chat-event-64", { ord: 1, events: [{ type: "turn.completed" }] });
    expect(host.notifyDone).not.toHaveBeenCalled();
    // Once only: the next real turn end toasts again.
    s.startTurn();
    emit("chat-event-64", { ord: 2, events: [{ type: "turn.completed" }] });
    expect(host.notifyDone).toHaveBeenCalledOnce();
    dropChatSession(64);
  });

  it("leaves an ACP turn to Go's boundary and applies any selector reply", async () => {
    const view = fakeView({ usesRpcRuntime: () => true });
    const { deps, emit } = fakeDeps();
    const s = chatSession(65, deps);
    s.attachView(view);
    await s.listenAcp();
    await s.listenEvents();
    s.startTurn();
    // A bare JSON-RPC response is not a turn end — whoever's prompt it answers.
    emit("acp-data-65", line({ id: 7, result: { stopReason: "end_turn" } }));
    expect(s.busy.value).toBe(true);
    // A selector reply to a push Go made (the row's settings after a start)
    // still updates the pickers, without a restore round.
    const configOptions = [{ id: "model", name: "Model", currentValue: "m2", options: [] }];
    emit("acp-data-65", line({ id: 8, result: { configOptions } }));
    expect(s.acpConfigOptions.value).toEqual(configOptions);
    expect(view.restoreAcpSelections).not.toHaveBeenCalled();
    // Our own pick's reply does restore the rest.
    s.trackAcpControl(9);
    emit("acp-data-65", line({ id: 9, result: { configOptions } }));
    expect(view.restoreAcpSelections).toHaveBeenCalledOnce();
    emit("chat-event-65", { ord: 2, events: [{ type: "turn.completed" }] });
    expect(s.busy.value).toBe(false);
    dropChatSession(65);
  });

  it("clears a Codex approval only on its serverRequest/resolved", async () => {
    const { deps, emit } = fakeDeps();
    const s = chatSession(66, deps);
    await s.listenAcp();
    emit("acp-req-66", line(codexApproval(9)));
    expect(s.acpPermRpcId.value).toBe(9);
    expect(s.acpPermReq.value).not.toBeNull();
    emit("acp-data-66", line({ method: "serverRequest/resolved", params: { requestId: 8 } }));
    expect(s.acpPermReq.value).not.toBeNull();
    emit("acp-data-66", line({ method: "serverRequest/resolved", params: { requestId: 9 } }));
    expect(s.acpPermReq.value).toBeNull();
    expect(s.messages.value).toEqual([]);
    dropChatSession(66);
  });

  it("settles a turn whose process died mid-turn, marking stuck tools failed", async () => {
    const { deps, emit } = fakeDeps();
    const s = chatSession(67, deps);
    await s.listenEvents();
    s.startTurn();
    emit("chat-event-67", { ord: 1, events: [{ type: "tool.started", toolCallId: "t", name: "Bash" }, { type: "session.exited" }] });
    expect(s.busy.value).toBe(false);
    expect(s.messages.value[0].toolFailed).toBe(true);
    dropChatSession(67);
  });

  it("a runtime reset drops every pending request, closes the partial and goes idle", async () => {
    const f = fakeDeps();
    const { deps, emit } = f;
    const s = chatSession(68, deps);
    await s.listenEvents();
    s.startTurn();
    emit("chat-event-68", { ord: 1, events: [{ type: "text.delta", messageId: "m", text: "half" }] });
    await nativeRequest(s, f, "Bash", { command: "ls" });
    await acpRequest(s, f, codexApproval(4));
    expect(s.pendingRequests.value.length).toBe(2);
    expect(s.messages.value.some((m) => m.partial)).toBe(true);

    emit("chat-event-68", { ord: 3, events: [{ type: "session.reset" }] });
    expect(s.pendingRequests.value).toEqual([]);
    expect(s.busy.value).toBe(false);
    expect(s.messages.value.some((m) => m.partial)).toBe(false);
    expect(s.messages.value.some((m) => m.text === "half")).toBe(true);
    dropChatSession(68);
  });

  it("a cleared reset also empties the transcript and restarts the ord", async () => {
    const f = fakeDeps();
    const { deps, emit } = f;
    const s = chatSession(69, deps);
    await s.listenEvents();
    emit("chat-event-69", { ord: 40, events: [{ type: "text.delta", messageId: "m", text: "old" }, { type: "turn.completed" }] });
    s.hydrate({ sessionId: "sess" });
    emit("chat-event-69", { ord: 0, events: [{ type: "session.reset", cleared: true }] });
    expect(s.messages.value).toEqual([]);
    expect(s.sessionId.value).toBe("");
    expect(savedFoldedOrd(s, f)).toBe(1);
    dropChatSession(69);
  });
});

// The process intents forward to the Go chat runtime, whatever the transport.
describe("process intents", () => {
  it("sends through chat_send: bubble, busy, the wire call", async () => {
    const f = fakeDeps();
    const s = chatSession(80, f.deps);
    const id = await s.send({ text: "go", images: ["data:x"] });
    expect(id).not.toBeNull();
    expect(s.busy.value).toBe(true);
    expect(s.messages.value.map((m) => [m.role, m.text])).toEqual([["user", "go"]]);
    expect(f.invoke).toHaveBeenCalledWith("chat_send", { chatId: 80, text: "go", images: ["data:x"] });
    dropChatSession(80);
  });

  it("a failed send reports itself and leaves the turn", async () => {
    const f = fakeDeps();
    f.invoke.mockImplementation(async (cmd: string) => {
      if (cmd === "chat_send") throw "pipe closed";
      return undefined;
    });
    const s = chatSession(81, f.deps);
    expect(await s.send({ text: "go", displayText: "go!" })).toBeNull();
    expect(s.busy.value).toBe(false);
    expect(s.messages.value.map((m) => [m.role, m.text])).toEqual([["user", "go"], ["assistant", "Error: pipe closed"]]);
    expect(s.messages.value[0].displayText).toBe("go!");
    expect(s.recallLastUserMessage()).toBe("go");
    expect(s.messages.value.map((m) => m.role)).toEqual(["assistant"]);
    dropChatSession(81);
  });

  it("restart / interrupt / clear forward to Go; an interrupt's turn end does not toast", async () => {
    const f = fakeDeps();
    const s = chatSession(82, f.deps);
    await s.restart();
    await s.clear();
    s.startTurn();
    await s.interrupt();
    expect(f.invoke.mock.calls.map(([cmd]) => cmd)).toEqual(["chat_restart", "chat_clear", "chat_interrupt"]);
    f.emit("chat-event-82", { ord: 1, events: [{ type: "turn.completed" }] });
    expect(f.host.notifyDone).not.toHaveBeenCalled();
    dropChatSession(82);
  });
});

// One door for every answer. The prompt-clearing rules differ per protocol and
// used to be spread over six functions in the view.
describe("answering a pending request", () => {
  it("allows a native permission, remembers an always-rule, and leaves a receipt", async () => {
    const f = fakeDeps();
    const { deps, host, invoke: fake } = f;
    const s = chatSession(70, deps);
    await nativeRequest(s, f, "Bash", { command: "git push" });
    expect(s.pendingRequests.value.map((r) => r.kind)).toEqual(["permission"]);
    expect(await s.respond({ kind: "permission", allow: true, always: true })).toBe(true);
    expect(fake).toHaveBeenCalledWith("claude_respond_control", { id: 70, requestId: "r-Bash", response: { behavior: "allow", updatedInput: { command: "git push" } } });
    expect(host.addPermissionRule).toHaveBeenCalledWith("Bash:git");
    expect(s.pendingRequests.value).toEqual([]);
    expect(s.messages.value[s.messages.value.length - 1]?.text).toBe("✓ Always allowed: Bash — git push");
    dropChatSession(70);
  });

  it("drops a native prompt whose answer could not be written, and says so", async () => {
    const f = fakeDeps();
    const s = chatSession(71, f.deps);
    await nativeRequest(s, f, "ExitPlanMode");
    f.invoke.mockRejectedValue(new Error("pipe closed"));
    expect(await s.respond({ kind: "plan", approve: false, feedback: "more detail" })).toBe(false);
    expect(s.pendingPlan.value).toBeNull();
    expect(s.messages.value.some((m) => m.text.includes("pipe closed"))).toBe(true);
    expect(s.nativeControlResponsePending.value).toBe(false);
    dropChatSession(71);
  });

  it("sends question answers in the tool's own input, {} to dismiss", async () => {
    const f = fakeDeps();
    const { deps, invoke: fake } = f;
    const s = chatSession(72, deps);
    await nativeRequest(s, f, "AskUserQuestion", { questions: [] });
    await s.respond({ kind: "question", answers: { "Which?": ["A", "B"] } });
    expect(fake).toHaveBeenCalledWith("claude_respond_control", expect.objectContaining({
      response: { behavior: "allow", updatedInput: { questions: [], answers: { "Which?": ["A", "B"] } } },
    }));
    expect(s.pendingQuestion.value).toBeNull();
    dropChatSession(72);
  });

  it("closes a generic ACP permission on the write, but waits for Codex to resolve its own", async () => {
    const acp = fakeDeps();
    acp.host.chat.mockReturnValue({ transport: "acp" } as never);
    const s = chatSession(73, acp.deps);
    await acpRequest(s, acp, { id: 4, method: "session/request_permission", params: { sessionId: "x", toolCall: { toolCallId: "t", title: "Edit", kind: "edit" }, options: [] } });
    expect(await s.respond({ kind: "acpOption", optionId: "allow_once", label: "Allow", reject: false })).toBe(true);
    expect(acp.invoke).toHaveBeenCalledWith("acp_respond_permission", { id: 73, rpcId: 4, optionId: "allow_once" });
    expect(s.acpPermReq.value).toBeNull();
    dropChatSession(73);

    const codex = fakeDeps();
    codex.host.chat.mockReturnValue({ transport: "codex-app-server" } as never);
    const c = chatSession(74, codex.deps);
    await acpRequest(c, codex, codexApproval(5));
    await c.respond({ kind: "acpOption", optionId: "codex:accept", label: "Accept", reject: false });
    expect(c.acpPermReq.value).not.toBeNull(); // serverRequest/resolved clears it
    expect(c.permissionResponsePending.value).toBe(true);
    dropChatSession(74);
  });

  it("keeps an ACP prompt retryable when its answer fails", async () => {
    const f = fakeDeps();
    const s = chatSession(75, f.deps);
    await acpRequest(s, f, codexApproval(6));
    f.invoke.mockRejectedValue(new Error("adapter gone"));
    expect(await s.respond({ kind: "acpOption", optionId: "x", label: "Allow", reject: false })).toBe(false);
    expect(s.acpPermReq.value).not.toBeNull();
    expect(s.permissionResponsePending.value).toBe(false);
    dropChatSession(75);
  });

  it("answers a Codex user-input request by question id", async () => {
    const f = fakeDeps();
    const { deps, invoke: fake } = f;
    const s = chatSession(76, deps);
    await acpRequest(s, f, codexInput(8));
    expect(await s.respond({ kind: "codexInput", answers: { q: ["A"] } })).toBe(true);
    expect(fake).toHaveBeenCalledWith("acp_respond_user_input", { id: 76, rpcId: 8, answers: { q: ["A"] } });
    expect(s.codexUserInput.value).toBeNull();
    dropChatSession(76);
  });
});

// A long chat is opened from its tail. The window must never be written back as
// the whole transcript — save_chat_messages replaces the chat's stored rows.
describe("a windowed transcript", () => {
  const msg = (id: number, role: "user" | "assistant" = "user") => ({ id, role, text: `m${id}` });

  it("does not save while earlier messages are not loaded, and saves once they are", async () => {
    const f = fakeDeps();
    const s = chatSession(90, f.deps);
    s.hydrate({ messages: [msg(8), msg(9)], hiddenBefore: 8 });
    expect(s.hiddenBefore.value).toBe(8);

    s.save();
    expect(f.invoke.mock.calls.some(([cmd]) => cmd === "save_chat_messages")).toBe(false);

    f.invoke.mockResolvedValueOnce({ messages: Array.from({ length: 8 }, (_, i) => msg(i)), start: 0, total: 10 });
    await s.loadEarlier();
    expect(f.invoke).toHaveBeenCalledWith("load_chat_messages_page", { chatId: 90, before: 8, limit: 60 });
    expect(s.messages.value.map((m) => m.id)).toEqual([0, 1, 2, 3, 4, 5, 6, 7, 8, 9]);
    expect(s.hiddenBefore.value).toBe(0);

    s.save();
    expect(f.invoke.mock.calls.some(([cmd]) => cmd === "save_chat_messages")).toBe(true);
    dropChatSession(90);
  });

  it("offsets the rewind index by what is not loaded", async () => {
    const f = fakeDeps();
    const s = chatSession(91, f.deps);
    s.hydrate({ messages: [msg(8), msg(9, "assistant"), msg(10)], hiddenBefore: 8 });
    await s.rewind(s.messages.value[2] as never);
    expect(f.invoke).toHaveBeenCalledWith("chat_rewind", { chatId: 91, index: 10, text: "m10" });
    dropChatSession(91);
  });
});
