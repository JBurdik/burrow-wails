import { describe, expect, it } from "vitest";
import { linkifyFileRefs, looksLikeMdPath, resolveLocalLink } from "./localLink";

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

describe("line ranges", () => {
  const base = "/repo";
  it("parses #L5-L20, #L12, :12, :12-20", () => {
    expect(resolveLocalLink("src/foo.ts#L5-L20", base)).toEqual({ path: "/repo/src/foo.ts", line: 5, lineEnd: 20, markdown: false });
    expect(resolveLocalLink("src/foo.ts#L12", base)).toEqual({ path: "/repo/src/foo.ts", line: 12, markdown: false });
    expect(resolveLocalLink("src/foo.ts:12", base)).toEqual({ path: "/repo/src/foo.ts", line: 12, markdown: false });
    expect(resolveLocalLink("src/foo.ts:12-20", base)).toEqual({ path: "/repo/src/foo.ts", line: 12, lineEnd: 20, markdown: false });
    expect(resolveLocalLink("src/foo.ts:12:3", base)?.lineEnd).toBeUndefined();
    expect(resolveLocalLink("src/foo.ts#L20-L5", base)).toMatchObject({ line: 5, lineEnd: 20 });
  });
});

describe("linkifyFileRefs", () => {
  const ref = (s: string) => [...s.matchAll(/data-file-ref="([^"]+)"/g)].map((m) => m[1]);
  it("wraps every supported format in plain text", () => {
    const html = "<p>see src/a.ts#L5-L20, src/b.ts#L12, src/c.ts:12 and ./d/e.go:12-20.</p>";
    expect(ref(linkifyFileRefs(html))).toEqual(["src/a.ts#L5-L20", "src/b.ts#L12", "src/c.ts:12", "./d/e.go:12-20"]);
  });
  it("leaves trailing punctuation outside the link", () => {
    expect(linkifyFileRefs("<p>(src/a.ts:12)</p>")).toContain('>src/a.ts:12</a>)');
  });
  it("wraps inline code, skips code blocks and existing links", () => {
    expect(ref(linkifyFileRefs("<p><code>src/a.ts:7</code></p>"))).toEqual(["src/a.ts:7"]);
    expect(linkifyFileRefs("<pre><code>src/a.ts:7</code></pre>")).not.toContain("data-file-ref");
    expect(linkifyFileRefs('<a href="src/a.ts#L5">src/a.ts:7</a>')).not.toContain("data-file-ref");
  });
  it("ignores URLs, bare names and non-paths", () => {
    expect(linkifyFileRefs("<p>https://github.com/x/y/a.ts#L5 and http://localhost:3000 and src/a.ts and v1.2.3:4 and 12:30</p>")).not.toContain("data-file-ref");
  });
});
