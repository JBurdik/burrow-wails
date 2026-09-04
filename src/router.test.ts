import { describe, it, expect } from "vitest";
import { createRouter, createMemoryHistory } from "vue-router";
import { routes, routeId, tabsOrWelcome, workspaceRoute } from "./router";

function testRouter() {
  return createRouter({ history: createMemoryHistory(), routes });
}

describe("view-state routes", () => {
  it("addresses a workspace and a tab", async () => {
    const r = testRouter();
    await r.push("/ws/7");
    expect(r.currentRoute.value.name).toBe("workspace");
    expect(routeId(r.currentRoute.value.params.wsId)).toBe(7);

    await r.push("/ws/7/tab/12");
    expect(r.currentRoute.value.name).toBe("tab");
    expect(routeId(r.currentRoute.value.params.tabId)).toBe(12);
  });

  it("sends anything unrecognised to the composer, not a blank shell", async () => {
    const r = testRouter();
    await r.push("/nope/1/2/3");
    expect(r.currentRoute.value.name).toBe("welcome");
  });

  it("does not match a non-numeric workspace id as a workspace", async () => {
    const r = testRouter();
    await r.push("/ws/abc");
    expect(r.currentRoute.value.name).toBe("welcome");
  });

  it("routeId rejects what is not a usable id", () => {
    expect(routeId("7")).toBe(7);
    expect(routeId(["7"])).toBe(7);
    expect(routeId("")).toBeNull();
    expect(routeId(undefined)).toBeNull();
    expect(routeId("abc")).toBeNull();
  });
});

// Was resolveWelcomeVisible() in stores/ui.ts, back when "is the composer up"
// was a tri-state ref. Same rule, now expressed as where to navigate.
describe("where 'show me the tabs' lands", () => {
  it("goes to the composer when the workspace has no live work", () => {
    // Restore reopens EVERY saved thread, so "has tabs" is not "has live work":
    // a startup with only settled threads must still land on the composer.
    expect(tabsOrWelcome(7, 0)).toBe("/");
    expect(tabsOrWelcome(7, 1)).toBe("/ws/7");
  });

  it("goes to the composer when there is no workspace at all", () => {
    expect(tabsOrWelcome(null, 3)).toBe("/");
    expect(workspaceRoute(undefined)).toBe("/");
    expect(workspaceRoute(7)).toBe("/ws/7");
  });
});
