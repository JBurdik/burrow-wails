import { describe, expect, it } from "vitest";
import {
  cacheClear, cacheRead, cacheWrite, drainOutbox, isOfflineError, outboxAdd, outboxRead, outboxRemove, outboxWrite,
  type HeldMessage,
} from "./remoteCache";

function mem() {
  const m = new Map<string, string>();
  return {
    getItem: (k: string) => m.get(k) ?? null,
    setItem: (k: string, v: string) => void m.set(k, v),
    removeItem: (k: string) => void m.delete(k),
    key: (i: number) => [...m.keys()][i] ?? null,
    get length() { return m.size; },
    map: m,
  };
}

const msg = (id: string, chatId = 1): HeldMessage => ({ id, chatId, text: id, at: 0 });

describe("read cache", () => {
  it("is keyed by environment and only for the cached reads", () => {
    const s = mem();
    cacheWrite("A", "shell_snapshot", { seq: 5 }, s);
    cacheWrite("B", "shell_snapshot", { seq: 9 }, s);
    cacheWrite("A", "chat_send", { no: 1 }, s);
    expect(cacheRead("A", "shell_snapshot", s)).toEqual({ seq: 5 });
    expect(cacheRead("B", "shell_snapshot", s)).toEqual({ seq: 9 });
    expect(cacheRead("A", "chat_send", s)).toBeNull();
  });

  it("clear drops one environment and leaves its outbox", () => {
    const s = mem();
    cacheWrite("A", "list_chats", [1], s);
    cacheWrite("A", "list_workspaces", [2], s);
    cacheWrite("B", "list_chats", [3], s);
    outboxWrite("A", [msg("x")], s);
    cacheClear("A", s);
    expect(cacheRead("A", "list_chats", s)).toBeNull();
    expect(cacheRead("A", "list_workspaces", s)).toBeNull();
    expect(cacheRead("B", "list_chats", s)).toEqual([3]);
    expect(outboxRead("A", s)).toHaveLength(1);
  });
});

describe("outbox", () => {
  it("adds in order, dedupes by id, removes by id", () => {
    let l = outboxAdd([], msg("a"));
    l = outboxAdd(l, msg("b"));
    l = outboxAdd(l, msg("a"));
    expect(l.map((m) => m.id)).toEqual(["a", "b"]);
    expect(outboxRemove(l, "a").map((m) => m.id)).toEqual(["b"]);
  });

  it("write reports a full storage", () => {
    const s = { ...mem(), setItem: () => { throw new Error("quota"); } };
    expect(outboxWrite("A", [msg("a")], s)).toBe(false);
  });

  it("classifies offline errors", () => {
    expect(isOfflineError(new Error("disconnected"))).toBe(true);
    expect(isOfflineError(new Error("transport unreachable: x"))).toBe(true);
    expect(isOfflineError(new Error("chat 4 not found"))).toBe(false);
  });
});

function io(items: HeldMessage[], fail: (m: HeldMessage) => Error | null) {
  let list = [...items];
  const sent: string[] = [];
  const failed: string[] = [];
  return {
    sent, failed,
    get list() { return list; },
    read: () => list,
    send: async (m: HeldMessage) => { const e = fail(m); if (e) throw e; sent.push(m.id); },
    remove: (id: string) => { list = list.filter((m) => m.id !== id); },
    fail: (id: string) => { failed.push(id); },
  };
}

describe("drainOutbox", () => {
  it("delivers oldest first and removes only after success", async () => {
    const x = io([msg("a"), msg("b", 2), msg("c")], () => null);
    expect(await drainOutbox(x)).toBe(3);
    expect(x.sent).toEqual(["a", "b", "c"]);
    expect(x.list).toEqual([]);
  });

  it("stops on an offline error and keeps the rest", async () => {
    const x = io([msg("a"), msg("b"), msg("c")], (m) => (m.id === "b" ? new Error("disconnected") : null));
    expect(await drainOutbox(x)).toBe(1);
    expect(x.list.map((m) => m.id)).toEqual(["b", "c"]);
  });

  it("a refusal blocks only that chat, never reordering it", async () => {
    const x = io([msg("a", 1), msg("b", 2), msg("c", 1)], (m) => (m.id === "a" ? new Error("chat gone") : null));
    await drainOutbox(x);
    expect(x.sent).toEqual(["b"]);
    expect(x.failed).toEqual(["a"]);
    expect(x.list.map((m) => m.id)).toEqual(["a", "c"]);
  });
});
