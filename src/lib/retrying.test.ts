import { describe, expect, it, vi } from "vitest";
import { retrying } from "./retrying";

describe("retrying", () => {
  it("retries a failing call until it succeeds", async () => {
    vi.useFakeTimers();
    let n = 0;
    const p = retrying(async () => { if (++n < 3) throw new Error("not yet"); return "ok"; }, [10, 10]);
    await vi.advanceTimersByTimeAsync(25);
    await expect(p).resolves.toBe("ok");
    expect(n).toBe(3);
    vi.useRealTimers();
  });

  it("rethrows the last error once the delays are used up", async () => {
    vi.useFakeTimers();
    const p = retrying(async () => { throw new Error("down"); }, [10]);
    const settled = expect(p).rejects.toThrow("down");
    await vi.advanceTimersByTimeAsync(20);
    await settled;
    vi.useRealTimers();
  });
});
