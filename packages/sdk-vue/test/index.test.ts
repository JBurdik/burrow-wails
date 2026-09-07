import { describe, expect, it } from "vitest";
import { Action, ActionPanel, Detail, List, ListItem, defineSurface } from "../src/index.js";

describe("@burrow/sdk-vue", () => {
  it("builds a serialisable native surface", () => {
    const surface = defineSurface({
      id: "deployments",
      title: "Deployments",
      root: List({
        title: "Recent deployments",
        children: [ListItem({ id: "api", title: "API", subtitle: "Healthy", actions: ActionPanel({ children: [Action({ id: "open", title: "Open logs" })] }) })],
      }),
    });
    expect(JSON.parse(JSON.stringify(surface))).toMatchObject({ id: "deployments", root: { type: "list", children: [{ type: "list-item" }] } });
  });

  it("rejects callbacks across the host boundary", () => {
    const unsafeActionPanel = {
      type: "action-panel" as const,
      children: [{ type: "action" as const, id: "x", title: "X", handler: () => undefined }],
    };
    expect(() => defineSurface({
      id: "x",
      title: "X",
      root: Detail({ title: "X", markdown: "Hi", actions: unsafeActionPanel as never }),
    })).toThrow("callbacks");
  });
});
