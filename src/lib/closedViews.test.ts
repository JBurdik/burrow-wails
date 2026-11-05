import { beforeEach, describe, expect, it } from "vitest";
import { clearClosedViews, popClosedView, pushClosedView, pickReopen } from "./closedViews";

describe("closedViews", () => {
  beforeEach(clearClosedViews);

  it("pops most recent first", () => {
    pushClosedView({ kind: "terminal", wsId: 1, cmd: "ls" });
    pushClosedView({ kind: "chat", wsId: 1, chatId: 7 });
    expect(popClosedView()).toMatchObject({ kind: "chat", wsId: 1, chatId: 7 });
    expect(popClosedView()).toMatchObject({ kind: "terminal", wsId: 1, cmd: "ls" });
    expect(popClosedView()).toBeUndefined();
  });

  it("caps at 20, dropping the oldest", () => {
    for (let i = 0; i < 25; i++) pushClosedView({ kind: "terminal", wsId: i });
    let n = 0, last: number | undefined;
    for (let v = popClosedView(); v; v = popClosedView()) { n++; last = v.wsId; }
    expect(n).toBe(20);
    expect(last).toBe(5);
  });

  it("keeps one entry per chat, at the newest position", () => {
    pushClosedView({ kind: "chat", wsId: 1, chatId: 3 });
    pushClosedView({ kind: "terminal", wsId: 1 });
    pushClosedView({ kind: "chat", wsId: 1, chatId: 3 });
    expect(popClosedView()).toMatchObject({ chatId: 3 });
    expect(popClosedView()).toMatchObject({ kind: "terminal" });
    expect(popClosedView()).toBeUndefined();
  });
});

describe("pickReopen", () => {
  it("prefers whichever closed last: the stack or the newest archived thread", () => {
    const top = { kind: "terminal" as const, wsId: 1, at: 100 };
    expect(pickReopen(top, { id: 7, workspaceId: 2, archivedAt: 50 })).toEqual({ from: "stack" });
    expect(pickReopen(top, { id: 7, workspaceId: 2, archivedAt: 150 })).toMatchObject({ from: "archive", view: { kind: "chat", chatId: 7, wsId: 2 } });
    expect(pickReopen(undefined, { id: 7, workspaceId: 2, archivedAt: 1 })).toMatchObject({ from: "archive" });
    expect(pickReopen(undefined, undefined)).toBeNull();
  });
});
