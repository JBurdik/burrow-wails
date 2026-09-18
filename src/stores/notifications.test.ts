/**
 * Unit tests for the notifications store: dedup, persistence, corrupt-storage
 * resilience. No DOM env — localStorage is stubbed in-memory per test.
 */

import { describe, it, expect, beforeEach, vi } from "vitest";
import { setActivePinia, createPinia } from "pinia";
import { useNotificationsStore } from "./notifications";

function stubLocalStorage() {
  const backing: Record<string, string> = {};
  (globalThis as unknown as { localStorage: Storage }).localStorage = {
    getItem: (k: string) => (k in backing ? backing[k] : null),
    setItem: (k: string, v: string) => {
      backing[k] = v;
    },
    removeItem: (k: string) => {
      delete backing[k];
    },
    clear: () => {
      for (const k of Object.keys(backing)) delete backing[k];
    },
    key: () => null,
    length: 0,
  };
}

beforeEach(() => {
  stubLocalStorage();
  setActivePinia(createPinia());
});

describe("notifications store", () => {
  it("dedup increments count instead of adding a row", () => {
    const s = useNotificationsStore();
    s.push({ title: "Build finished", type: "done", source: "git" });
    s.push({ title: "Build finished", type: "done", source: "git" });
    expect(s.history.length).toBe(1);
    expect(s.history[0].count).toBe(2);
  });

  it("a 61s gap does NOT dedup", () => {
    vi.useFakeTimers();
    const s = useNotificationsStore();
    s.push({ title: "X", type: "info" });
    vi.advanceTimersByTime(61_000);
    s.push({ title: "X", type: "info" });
    expect(s.history.length).toBe(2);
    vi.useRealTimers();
  });

  it("a different title does NOT dedup", () => {
    const s = useNotificationsStore();
    s.push({ title: "A", type: "info" });
    s.push({ title: "B", type: "info" });
    expect(s.history.length).toBe(2);
  });

  it("history and readTs round-trip through localStorage", () => {
    const s1 = useNotificationsStore();
    s1.push({ title: "Persisted", type: "done" });
    s1.markAllRead();

    setActivePinia(createPinia());
    const s2 = useNotificationsStore();
    expect(s2.history.length).toBe(1);
    expect(s2.history[0].title).toBe("Persisted");
    expect(s2.readTs).toBe(s1.readTs);
  });

  // The id counter is module state, so a fresh pinia is not a fresh process —
  // only re-importing the module reproduces what a restart actually does.
  it("a restored history never hands a new toast an id it already holds", async () => {
    const s1 = useNotificationsStore();
    s1.push({ title: "Before restart", type: "done" });
    const restoredId = s1.history[0].id;

    vi.resetModules();
    const { useNotificationsStore: freshStore } = await import("./notifications");
    setActivePinia(createPinia());
    const s2 = freshStore();
    s2.push({ title: "After restart", type: "done" });

    expect(s2.history.length).toBe(2);
    expect(s2.history[0].id).toBeGreaterThan(restoredId);
    expect(new Set(s2.history.map((i) => i.id)).size).toBe(2);
  });

  it("a corrupt localStorage value leaves the store constructible with an empty history", () => {
    localStorage.setItem("burrow.notifications", "{not json");
    const s = useNotificationsStore();
    expect(s.history).toEqual([]);
  });
});
