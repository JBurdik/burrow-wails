import { beforeEach, describe, expect, it } from "vitest";
import { clearClosedViews, popClosedView, pushClosedView } from "./closedViews";

describe("closedViews", () => {
  beforeEach(clearClosedViews);

  it("pops most recent first", () => {
    pushClosedView({ kind: "terminal", wsId: 1, cmd: "ls" });
    pushClosedView({ kind: "chat", wsId: 1, chatId: 7 });
    expect(popClosedView()).toEqual({ kind: "chat", wsId: 1, chatId: 7 });
    expect(popClosedView()).toEqual({ kind: "terminal", wsId: 1, cmd: "ls" });
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
