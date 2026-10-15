import { describe, expect, it } from "vitest";
import { looksLikeMdPath, resolveLocalLink } from "./localLink";

describe("resolveLocalLink", () => {
  const base = "/repo";
  it("resolves relative markdown links against the base dir", () => {
    expect(resolveLocalLink("docs/plans/x.md", base)).toEqual({ path: "/repo/docs/plans/x.md", line: undefined, markdown: true });
    expect(resolveLocalLink("./a/../b.md", base)?.path).toBe("/repo/b.md");
  });
  it("keeps absolute and file:// paths, parses lines", () => {
    expect(resolveLocalLink("/abs/x.ts:12", base)).toEqual({ path: "/abs/x.ts", line: 12, markdown: false });
    expect(resolveLocalLink("file:///abs/My%20Plan.md#L3", base)).toEqual({ path: "/abs/My Plan.md", line: 3, markdown: true });
  });
  it("ignores external urls and anchors", () => {
    expect(resolveLocalLink("https://example.com/x.md", base)).toBeNull();
    expect(resolveLocalLink("mailto:a@b.c", base)).toBeNull();
    expect(resolveLocalLink("#section", base)).toBeNull();
  });
  it("detects md paths in inline code", () => {
    expect(looksLikeMdPath("docs/plans/003-view-state-routes.md")).toBe(true);
    expect(looksLikeMdPath("pnpm build && cat x.md")).toBe(false);
  });
});
