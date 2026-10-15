import { describe, expect, it } from "vitest";
import {
  closeBottom, closeSurface, emptyLayout, isShown, loadLayouts, moveTo, normalizeLayout, openBottom, openIn, paneOf,
  RP_LAYOUT_KEY, saveLayout, toggleMaximize,
} from "./rpLayout";

function memStorage(init: Record<string, string> = {}) {
  const m = new Map(Object.entries(init));
  return { getItem: (k: string) => m.get(k) ?? null, setItem: (k: string, v: string) => void m.set(k, v) };
}

describe("rpLayout", () => {
  it("opens new surfaces into the requested pane and only focuses existing ones", () => {
    const l = emptyLayout();
    openIn(l, "browser");
    expect(openIn(l, "terminal", 1)).toBe(1);
    expect(l.bottomOpen).toBe(true);
    // Already open on top: asking for the bottom pane just focuses it on top.
    expect(openIn(l, "browser", 1)).toBe(0);
    expect(l.panes[1].tabs).toEqual(["terminal"]);
    expect(isShown(l, "browser") && isShown(l, "terminal")).toBe(true);
  });

  it("moving the last bottom surface up collapses the split", () => {
    const l = emptyLayout();
    openIn(l, "git");
    openIn(l, "terminal", 1);
    toggleMaximize(l, 1);
    moveTo(l, "terminal", 0);
    expect(paneOf(l, "terminal")).toBe(0);
    expect(l.panes[0].active).toBe("terminal");
    expect(l.bottomOpen).toBe(false);
    expect(l.maximized).toBeNull();
  });

  it("closing a surface falls back to a sibling tab", () => {
    const l = emptyLayout();
    openIn(l, "git");
    openIn(l, "diff");
    closeSurface(l, "diff");
    expect(l.panes[0]).toEqual({ tabs: ["git"], active: "git" });
  });

  it("closing the bottom pane moves its surfaces on top instead of closing them", () => {
    const l = emptyLayout();
    openBottom(l);
    expect(l.bottomOpen).toBe(true);
    openIn(l, "terminal", 1);
    openIn(l, "browser", 1);
    closeBottom(l);
    expect(l.panes[0].tabs).toEqual(["terminal", "browser"]);
    expect(l.panes[0].active).toBe("terminal");
    expect(l.bottomOpen).toBe(false);
    expect(isShown(l, "browser")).toBe(false);
  });

  it("normalizes junk and duplicates from storage", () => {
    const l = normalizeLayout({ panes: [{ tabs: ["a", "a", 3], active: "zzz" }, { tabs: ["a", "b"], active: "b" }], split: 5, maximized: 7 });
    expect(l.panes[0]).toEqual({ tabs: ["a"], active: null });
    expect(l.panes[1]).toEqual({ tabs: ["b"], active: "b" });
    expect(l.split).toBe(0.85);
    expect(l.bottomOpen).toBe(true);
    expect(l.maximized).toBeNull();
    expect(normalizeLayout("nope")).toEqual(emptyLayout());
  });

  it("save is read-modify-write per workspace and survives corrupt storage", () => {
    const s = memStorage({ [RP_LAYOUT_KEY]: "{not json" });
    const a = emptyLayout();
    openIn(a, "git");
    saveLayout(1, a, s);
    saveLayout(2, emptyLayout(), s);
    const back = loadLayouts(s);
    expect(back[1].panes[0].tabs).toEqual(["git"]);
    expect(Object.keys(back)).toEqual(["1", "2"]);
  });
});
