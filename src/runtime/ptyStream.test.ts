import { describe, it, expect } from "vitest";
import { createPtyStream, type PtyChunk } from "./ptyStream";

const bytes = (s: string) => Array.from(s, (c) => c.charCodeAt(0));
const text = (b: number[]) => String.fromCharCode(...b);

function setup(log: string) {
  // `log` is the PTY's whole output; fetchSince serves it from offset `ringStart`.
  let ringStart = 0;
  const out: number[][] = [];
  const lost: number[] = [];
  let fetches = 0;
  const s = createPtyStream({
    write: (b) => out.push(b),
    fetchSince: async (since) => {
      fetches++;
      const from = Math.max(since, ringStart);
      return { off: from, data: bytes(log.slice(from)) };
    },
    onLost: (n) => lost.push(n),
  });
  const chunk = (from: number, to: number): PtyChunk => ({ off: from, data: bytes(log.slice(from, to)) });
  return {
    s,
    chunk,
    rendered: () => out.map(text).join(""),
    lost,
    fetches: () => fetches,
    dropRingBefore: (n: number) => (ringStart = n),
  };
}

describe("ptyStream", () => {
  it("renders contiguous chunks once, drops duplicates and trims overlap", () => {
    const t = setup("hello world");
    t.s.push(t.chunk(0, 5));
    t.s.push(t.chunk(0, 5)); // duplicate
    t.s.push(t.chunk(3, 8)); // overlaps "lo"
    t.s.push(t.chunk(8, 11));
    expect(t.rendered()).toBe("hello world");
    expect(t.fetches()).toBe(0);
  });

  it("catchUp after a drop fills exactly the missing bytes", async () => {
    const t = setup("0123456789");
    t.s.push(t.chunk(0, 3));
    // socket down while 3..7 streamed; on reconnect:
    await t.s.catchUp();
    t.s.push(t.chunk(5, 10)); // live chunk overlapping the catch-up
    expect(t.rendered()).toBe("0123456789");
  });

  it("a hole in live offsets triggers a catch-up and holds later chunks behind it", async () => {
    const t = setup("abcdefghij");
    t.s.push(t.chunk(0, 2));
    t.s.push(t.chunk(6, 8)); // gap 2..6
    t.s.push(t.chunk(8, 10));
    await Promise.resolve();
    await Promise.resolve();
    expect(t.rendered()).toBe("abcdefghij");
    expect(t.fetches()).toBe(1);
  });

  it("reports bytes that fell out of the daemon ring and resumes after them", async () => {
    const t = setup("0123456789");
    t.s.push(t.chunk(0, 2));
    t.dropRingBefore(6);
    await t.s.catchUp();
    expect(t.lost).toEqual([4]);
    expect(t.rendered()).toBe("016789");
  });

  it("a failed catch-up accepts the hole instead of looping", async () => {
    const out: number[][] = [];
    let fetches = 0;
    const s = createPtyStream({
      write: (b) => out.push(b),
      fetchSince: async () => {
        fetches++;
        throw new Error("unknown request kind: since");
      },
    });
    s.push({ off: 0, data: bytes("ab") });
    s.push({ off: 5, data: bytes("fg") });
    await new Promise((r) => setTimeout(r, 0));
    expect(out.map(text).join("")).toBe("abfg");
    expect(fetches).toBe(1);
  });

  it("chunks without offsets (old daemon) are written as-is", () => {
    const out: number[][] = [];
    const s = createPtyStream({ write: (b) => out.push(b), fetchSince: async () => ({ off: null, data: [] }) });
    s.push({ off: null, data: bytes("a") });
    s.push({ off: null, data: bytes("a") });
    expect(out.map(text).join("")).toBe("aa");
  });
});
