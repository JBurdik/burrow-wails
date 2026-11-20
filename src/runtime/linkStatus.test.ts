import { describe, expect, it } from "vitest";
import { createLinkStatus, OFFLINE_AFTER_MS, SHOW_AFTER_MS, type LinkState } from "./linkStatus";

function fakeTimers() {
  let now = 0;
  let q: { at: number; fn: () => void; id: number }[] = [];
  let n = 0;
  return {
    timers: {
      set: (fn: () => void, ms: number) => { q.push({ at: now + ms, fn, id: ++n }); return n; },
      clear: (h: unknown) => { q = q.filter((t) => t.id !== h); },
    },
    advance(ms: number) {
      now += ms;
      const due = q.filter((t) => t.at <= now).sort((a, b) => a.at - b.at);
      q = q.filter((t) => t.at > now);
      for (const t of due) t.fn();
    },
  };
}

describe("createLinkStatus", () => {
  it("a short drop shows nothing", () => {
    const t = fakeTimers();
    const seen: LinkState[] = [];
    const s = createLinkStatus((x) => seen.push(x), t.timers);
    s.set(false);
    t.advance(SHOW_AFTER_MS - 1);
    s.set(true);
    t.advance(OFFLINE_AFTER_MS);
    expect(seen).toEqual(["up"]);
  });

  it("reconnecting after the debounce, offline later, up at once", () => {
    const t = fakeTimers();
    const seen: LinkState[] = [];
    const s = createLinkStatus((x) => seen.push(x), t.timers);
    s.set(false);
    t.advance(SHOW_AFTER_MS);
    expect(seen).toEqual(["reconnecting"]);
    s.set(false); // repeated down does not restart the clock
    t.advance(OFFLINE_AFTER_MS - SHOW_AFTER_MS);
    expect(seen).toEqual(["reconnecting", "offline"]);
    s.set(true);
    expect(seen[seen.length - 1]).toBe("up");
  });
});
