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
  const fakeInvoke = vi.fn(async () => undefined as unknown);
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

  // A Codex user-input prompt that arrives while the chat is unmounted used to
  // land in the dead view's own ref: the remount showed nothing, and the
  // session could be evicted while the agent sat blocked on an answer.
  it("keeps a chat blocked on a Codex user-input prompt, and the prompt survives a remount", () => {
    const s = chatSession(4);
    s.retain();
    s.release(); // the view is gone before the request arrives
    const again = chatSession(4);
    again.codexUserInput.value = { rpcId: 9, questions: [{ id: "q", header: "Q", question: "Which?", options: [] }] };
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
    const { deps, invoke: fake } = fakeDeps();
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
      expect(s.lastOrd).toBe(6);

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

describe("eviction after a turn ends behind a closed view", () => {
  it("keeps a busy session on release, then drops it once it settles", () => {
    // release() cannot be the only decision point: a busy session is kept on
    // purpose, and if nothing looks again it sits in the registry with its
    // listeners and transcript until the chat itself is closed.
    vi.useFakeTimers();
    const s = chatSession(30);
    s.retain();
    s.busy.value = true;
    s.release();
    expect(liveChatSessionIds()).toContain(30);

    s.busy.value = false;
    s.maybeEvict();
    expect(liveChatSessionIds()).toContain(30); // deferred, not immediate
    vi.runAllTimers();
    expect(liveChatSessionIds()).not.toContain(30);
    vi.useRealTimers();
  });

  it("does not evict while a queued message is still waiting to be sent", () => {
    // finishTurn flushes the queue a nextTick later; evicting in between would
    // hand that send a detached session while the next mount built a fresh one.
    vi.useFakeTimers();
    const s = chatSession(31);
    s.retain();
    s.release();
    // release() already dropped it (idle, unwatched) — take a fresh one.
    const s2 = chatSession(31);
    s2.retain();
    s2.busy.value = true;
    s2.release();
    s2.busy.value = false;
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
    s.messages.value = [
      { id: 4, role: "queued", text: "first" },
      { id: 5, role: "assistant", text: "working" },
      { id: 6, role: "queued", text: "second", images: ["data:image/png;base64,x"] },
    ];

    expect(s.takeNextQueuedMessage()).toEqual({ id: 4, text: "first" });
    expect(s.takeNextQueuedMessage()).toEqual({ id: 6, text: "second", images: ["data:image/png;base64,x"] });
    dropChatSession(41);
  });

  it("sends the next follow-up when a turn ends, even with nobody watching", async () => {
    // The chat leaf is unmounted while the user looks at another tab, so the
    // drain cannot live in a component watcher — the turn that releases the
    // queue usually finishes right there.
    const { deps } = fakeDeps();
    const s = chatSession(43, deps);
    const view = fakeView();
    s.attachView(view);
    s.retain();
    s.busy.value = true;
    await nextTick(); // let the flag settle, as a real turn's start does
    s.enqueueMessage("after this one");
    s.release(); // view unmounted mid-turn
    s.busy.value = false;
    await nextTick();
    await nextTick();
    expect(view.send).toHaveBeenCalledWith("after this one", undefined);
    expect(s.messageQueue.value).toEqual([]);
    dropChatSession(43);
  });

  it("moves a message to the head of the queue in the transcript too", () => {
    const s = chatSession(42);
    const first = s.enqueueMessage("first");
    s.messages.value.push({ id: 900, role: "assistant", text: "working" });
    const second = s.enqueueMessage("second");

    s.moveQueuedMessageNext(second.id);
    expect(s.messageQueue.value.map((e) => e.id)).toEqual([second.id, first.id]);
    expect(s.messages.value.map((m) => m.id)).toEqual([second.id, first.id, 900]);
    expect(s.takeNextQueuedMessage()).toEqual({ id: second.id, text: "second" });
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
    const { deps, host, emit } = fakeDeps();
    const s = chatSession(63, deps);
    s.attachView(fakeView());
    await s.listenEvents();
    s.messages.value.push({ id: s.nextMsgId++, role: "user", text: "hi" });
    s.expectEcho("hi");
    s.busy.value = true;
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
    expect(s.lastOrd).toBe(2);
    dropChatSession(63);
  });

  it("does not toast a turn ended by our own restart", async () => {
    const { deps, host, emit } = fakeDeps();
    const s = chatSession(64, deps);
    await s.listenEvents();
    s.busy.value = true;
    s.suppressNextDone.value = true;
    emit("chat-event-64", { ord: 1, events: [{ type: "turn.completed" }] });
    expect(host.notifyDone).not.toHaveBeenCalled();
    expect(s.suppressNextDone.value).toBe(false);
    dropChatSession(64);
  });

  it("settles an ACP turn on the response to its own session/prompt, not another", async () => {
    const { deps, emit } = fakeDeps();
    const s = chatSession(65, deps);
    s.attachView(fakeView({ usesRpcRuntime: () => true }));
    await s.listenAcp();
    s.busy.value = true;
    s.acpPromptRpcId.value = 7;
    emit("acp-data-65", line({ id: 3, result: {} }));
    expect(s.busy.value).toBe(true);
    emit("acp-data-65", line({ id: 7, result: { stopReason: "end_turn" } }));
    expect(s.busy.value).toBe(false);
    expect(s.acpPromptRpcId.value).toBeNull();
    dropChatSession(65);
  });

  it("clears a Codex approval only on its serverRequest/resolved", async () => {
    const { deps, emit } = fakeDeps();
    const s = chatSession(66, deps);
    await s.listenAcp();
    emit("acp-req-66", line({ id: 9, method: "item/commandExecution/requestApproval", params: { command: "go test", itemId: "i" } }));
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
    s.busy.value = true;
    s.runtimeStarted.value = true;
    emit("chat-event-67", { ord: 1, events: [{ type: "tool.started", toolCallId: "t", name: "Bash" }, { type: "session.exited" }] });
    expect(s.busy.value).toBe(false);
    expect(s.runtimeStarted.value).toBe(false);
    expect(s.messages.value[0].toolFailed).toBe(true);
    dropChatSession(67);
  });
});

// One door for every answer. The prompt-clearing rules differ per protocol and
// used to be spread over six functions in the view.
describe("answering a pending request", () => {
  const nativeRequest = (toolName: string, input: Record<string, unknown> = {}) =>
    ({ requestId: `r-${toolName}`, toolName, input, suggestions: [] });

  it("allows a native permission, remembers an always-rule, and leaves a receipt", async () => {
    const { deps, host, invoke: fake } = fakeDeps();
    const s = chatSession(70, deps);
    s.pendingPermission.value = nativeRequest("Bash", { command: "git push" });
    expect(s.pendingRequests.value.map((r) => r.kind)).toEqual(["permission"]);
    expect(await s.respond({ kind: "permission", allow: true, always: true })).toBe(true);
    expect(fake).toHaveBeenCalledWith("claude_respond_control", { id: 70, requestId: "r-Bash", response: { behavior: "allow", updatedInput: { command: "git push" } } });
    expect(host.addPermissionRule).toHaveBeenCalledWith("Bash:git");
    expect(s.pendingRequests.value).toEqual([]);
    expect(s.messages.value[s.messages.value.length - 1]?.text).toBe("✓ Always allowed: Bash — git push");
    dropChatSession(70);
  });

  it("drops a native prompt whose answer could not be written, and says so", async () => {
    const { deps, invoke: fake } = fakeDeps();
    fake.mockRejectedValue(new Error("pipe closed"));
    const s = chatSession(71, deps);
    s.pendingPlan.value = nativeRequest("ExitPlanMode");
    expect(await s.respond({ kind: "plan", approve: false, feedback: "more detail" })).toBe(false);
    expect(s.pendingPlan.value).toBeNull();
    expect(s.messages.value[0].text).toContain("pipe closed");
    expect(s.nativeControlResponsePending.value).toBe(false);
    dropChatSession(71);
  });

  it("sends question answers in the tool's own input, {} to dismiss", async () => {
    const { deps, invoke: fake } = fakeDeps();
    const s = chatSession(72, deps);
    s.pendingQuestion.value = nativeRequest("AskUserQuestion", { questions: [] });
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
    s.acpPermReq.value = { rpcId: 4, toolCallId: "t", title: "Edit", kind: "edit", options: [], rawInput: {} };
    s.acpPermRpcId.value = 4;
    expect(await s.respond({ kind: "acpOption", optionId: "allow_once", label: "Allow", reject: false })).toBe(true);
    expect(acp.invoke).toHaveBeenCalledWith("acp_respond_permission", { id: 73, rpcId: 4, optionId: "allow_once" });
    expect(s.acpPermReq.value).toBeNull();
    dropChatSession(73);

    const codex = fakeDeps();
    codex.host.chat.mockReturnValue({ transport: "codex-app-server" } as never);
    const c = chatSession(74, codex.deps);
    c.acpPermReq.value = { rpcId: 5, toolCallId: "t", title: "Run", kind: "execute", options: [], rawInput: {} };
    await c.respond({ kind: "acpOption", optionId: "codex:accept", label: "Accept", reject: false });
    expect(c.acpPermReq.value).not.toBeNull(); // serverRequest/resolved clears it
    expect(c.permissionResponsePending.value).toBe(true);
    dropChatSession(74);
  });

  it("keeps an ACP prompt retryable when its answer fails", async () => {
    const { deps, invoke: fake } = fakeDeps();
    fake.mockRejectedValue(new Error("adapter gone"));
    const s = chatSession(75, deps);
    s.acpPermReq.value = { rpcId: 6, toolCallId: "t", title: "Edit", kind: "edit", options: [], rawInput: {} };
    expect(await s.respond({ kind: "acpOption", optionId: "x", label: "Allow", reject: false })).toBe(false);
    expect(s.acpPermReq.value).not.toBeNull();
    expect(s.permissionResponsePending.value).toBe(false);
    dropChatSession(75);
  });

  it("answers a Codex user-input request by question id", async () => {
    const { deps, invoke: fake } = fakeDeps();
    const s = chatSession(76, deps);
    s.codexUserInput.value = { rpcId: 8, questions: [{ id: "q", header: "Q", question: "Which?", options: [] }] };
    expect(await s.respond({ kind: "codexInput", answers: { q: ["A"] } })).toBe(true);
    expect(fake).toHaveBeenCalledWith("acp_respond_user_input", { id: 76, rpcId: 8, answers: { q: ["A"] } });
    expect(s.codexUserInput.value).toBeNull();
    dropChatSession(76);
  });
});
