import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createPendingDeletes } from "./pendingDeletes";

describe("createPendingDeletes", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it("hides at once and commits only after the window", async () => {
    const p = createPendingDeletes(8000);
    const commit = vi.fn();
    p.schedule([1, 2], commit);
    expect(p.isHidden(1) && p.isHidden(2)).toBe(true);
    await vi.advanceTimersByTimeAsync(7999);
    expect(commit).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1);
    expect(commit).toHaveBeenCalledOnce();
    expect(p.isHidden(1)).toBe(false);
  });

  it("undo restores the rows and never commits", async () => {
    const p = createPendingDeletes(8000);
    const commit = vi.fn();
    const undo = p.schedule([1], commit);
    undo();
    expect(p.isHidden(1)).toBe(false);
    await vi.advanceTimersByTimeAsync(10_000);
    expect(commit).not.toHaveBeenCalled();
  });

  it("keeps each pending delete independent", async () => {
    const p = createPendingDeletes(8000);
    const a = vi.fn();
    const b = vi.fn();
    p.schedule([1], a);
    await vi.advanceTimersByTimeAsync(4000);
    const undoB = p.schedule([2], b);
    await vi.advanceTimersByTimeAsync(4000);
    expect(a).toHaveBeenCalledOnce(); // on its own clock, not flushed/reset by the second
    expect(b).not.toHaveBeenCalled();
    expect(p.isHidden(2)).toBe(true);
    undoB();
    await vi.advanceTimersByTimeAsync(10_000);
    expect(b).not.toHaveBeenCalled();
  });

  it("undo after the commit started is a no-op", async () => {
    const p = createPendingDeletes(10);
    const commit = vi.fn();
    const undo = p.schedule([1], commit);
    await vi.advanceTimersByTimeAsync(10);
    undo();
    expect(commit).toHaveBeenCalledOnce();
  });
});
