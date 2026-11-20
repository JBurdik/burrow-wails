import { describe, expect, it } from "vitest";
import type { ChatMessage } from "./chatTypes";
import {
  applySplice, applySubscribe, applyTail, mergeLocal, prependPage, windowFromPage, type TranscriptWindow,
} from "./transcriptSplice";

const row = (text: string, role: ChatMessage["role"] = "assistant", extra: Partial<ChatMessage> = {}): ChatMessage =>
  ({ id: 999, role, text, ...extra });
const texts = (w: TranscriptWindow) => w.rows.map((r) => r.text);
const win = (texts: string[], start = 0, rev = 10): TranscriptWindow =>
  windowFromPage({ messages: texts.map((t) => row(t)), start, rev });

describe("windowFromPage", () => {
  it("ids rows by transcript index and takes the page rev", () => {
    const w = win(["a", "b"], 5, 7);
    expect(w.rows.map((r) => r.id)).toEqual([5, 6]);
    expect(w.rev).toBe(7);
  });
});

describe("applySplice", () => {
  const cases: Array<{ name: string; w: TranscriptWindow; sp: Parameters<typeof applySplice>[1]; result: string; texts: string[]; start: number; rev: number }> = [
    { name: "chained append", w: win(["a", "b"]), sp: { baseRev: 10, rev: 11, fromIndex: 2, messages: [row("c")] },
      result: "applied", texts: ["a", "b", "c"], start: 0, rev: 11 },
    { name: "chained replace of the tail row", w: win(["a", "b"]), sp: { baseRev: 10, rev: 11, fromIndex: 1, messages: [row("B"), row("c")] },
      result: "applied", texts: ["a", "B", "c"], start: 0, rev: 11 },
    { name: "gap: baseRev ahead of the window", w: win(["a"]), sp: { baseRev: 12, rev: 13, fromIndex: 1, messages: [row("x")] },
      result: "gap", texts: ["a"], start: 0, rev: 10 },
    { name: "duplicate is stale", w: win(["a"]), sp: { baseRev: 9, rev: 10, fromIndex: 0, messages: [row("old")] },
      result: "stale", texts: ["a"], start: 0, rev: 10 },
    { name: "authoritative rewind inside the window", w: win(["a", "b", "c"]), sp: { baseRev: 3, rev: 12, fromIndex: 1, messages: [], authoritative: true },
      result: "applied", texts: ["a"], start: 0, rev: 12 },
    { name: "authoritative clear above the window", w: win(["x", "y"], 60), sp: { baseRev: 10, rev: 11, fromIndex: 0, messages: [], authoritative: true },
      result: "applied", texts: [], start: 0, rev: 11 },
    { name: "authoritative older than held is stale", w: win(["a"]), sp: { baseRev: 0, rev: 9, fromIndex: 0, messages: [], authoritative: true },
      result: "stale", texts: ["a"], start: 0, rev: 10 },
    { name: "chained splice reaching above the window keeps its start", w: win(["y"], 5), sp: { baseRev: 10, rev: 11, fromIndex: 4, messages: [row("x"), row("Y")] },
      result: "applied", texts: ["Y"], start: 5, rev: 11 },
    { name: "splice past the window's end restarts it", w: win(["a"]), sp: { baseRev: 10, rev: 11, fromIndex: 3, messages: [row("d")] },
      result: "applied", texts: ["d"], start: 3, rev: 11 },
  ];
  for (const c of cases) {
    it(c.name, () => {
      expect(applySplice(c.w, c.sp)).toBe(c.result);
      expect(texts(c.w)).toEqual(c.texts);
      expect(c.w.start).toBe(c.start);
      expect(c.w.rev).toBe(c.rev);
      c.w.rows.forEach((r, i) => expect(r.id).toBe(c.w.start + i));
    });
  }

  it("ignores everything until a page is loaded", () => {
    const w: TranscriptWindow = { start: 0, rows: [], rev: -1 };
    expect(applySplice(w, { baseRev: 0, rev: 1, fromIndex: 0, messages: [row("a")], authoritative: true })).toBe("stale");
    expect(w.rows).toEqual([]);
  });
});

describe("applyTail", () => {
  it("appends at the offset and a repeat is idempotent", () => {
    const w = win(["Hel"]);
    const t = { rev: 10, index: 0, offset: 3, append: "lo", kind: "text" as const };
    expect(applyTail(w, t)).toBe(true);
    expect(applyTail(w, t)).toBe(true);
    expect(texts(w)).toEqual(["Hello"]);
  });
  it("counts the offset in UTF-16 units", () => {
    const w = win(["😀"]);
    applyTail(w, { rev: 10, index: 0, offset: 2, append: "!", kind: "text" });
    expect(texts(w)).toEqual(["😀!"]);
  });
  it("ignores a stale rev, a missing row, and a row shorter than the offset", () => {
    const w = win(["ab"]);
    expect(applyTail(w, { rev: 9, index: 0, offset: 2, append: "X", kind: "text" })).toBe(false);
    expect(applyTail(w, { rev: 10, index: 4, offset: 0, append: "X", kind: "text" })).toBe(false);
    expect(applyTail(w, { rev: 10, index: 0, offset: 5, append: "X", kind: "text" })).toBe(false);
    expect(texts(w)).toEqual(["ab"]);
  });
});

describe("applySubscribe", () => {
  it("applies the chain, skips splices already applied live, then sets inProgress", () => {
    const w = win(["a"]);
    applySplice(w, { baseRev: 10, rev: 11, fromIndex: 1, messages: [row("b")] }); // arrived live before the reply
    const res = applySubscribe(w, {
      rev: 12,
      splices: [
        { baseRev: 10, rev: 11, fromIndex: 1, messages: [row("b")] },
        { baseRev: 11, rev: 12, fromIndex: 2, messages: [row("c", "assistant", { partial: true })] },
      ],
      inProgress: { index: 2, text: "c so far" },
    });
    expect(res).toBe("applied");
    expect(texts(w)).toEqual(["a", "b", "c so far"]);
    expect(w.rev).toBe(12);
  });
  it("takes an authoritative newest page after a ring miss", () => {
    const w = win(["a"]);
    applySubscribe(w, { rev: 40, splices: [{ baseRev: 0, rev: 40, fromIndex: 100, messages: [row("z")], authoritative: true }], inProgress: null });
    expect(w.start).toBe(100);
    expect(texts(w)).toEqual(["z"]);
  });
  it("reports a gap inside the reply", () => {
    const w = win(["a"]);
    expect(applySubscribe(w, { rev: 14, splices: [{ baseRev: 13, rev: 14, fromIndex: 1, messages: [] }], inProgress: null })).toBe("gap");
  });
});

describe("prependPage", () => {
  it("puts older rows in front without duplicating the window", () => {
    const w = win(["c", "d"], 2);
    prependPage(w, { messages: [row("a"), row("b"), row("c")], start: 0 });
    expect(texts(w)).toEqual(["a", "b", "c", "d"]);
    expect(w.start).toBe(0);
  });
});

describe("mergeLocal", () => {
  it("interleaves client rows at their anchors", () => {
    const w = win(["a", "b"], 3);
    const local = [{ row: row("echo", "user"), after: 5 }, { row: row("marker", "system-info"), after: 4 }];
    expect(mergeLocal(w, local).map((r) => r.text)).toEqual(["a", "marker", "b", "echo"]);
  });
});
