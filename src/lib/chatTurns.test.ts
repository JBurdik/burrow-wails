import { describe, expect, it } from "vitest";
import { editOf, fmtDuration, hunkLines, isProjectPath, mergeEdits } from "./chatTurns";

describe("editOf", () => {
  it("ignores tool calls with no edit content (Read/Grep)", () => {
    expect(editOf({ file_path: "/a/b.ts" })).toBeNull();
    expect(editOf({ path: "/a", pattern: "foo" })).toBeNull();
    expect(editOf(undefined)).toBeNull();
    expect(editOf({ content: "x" })).toBeNull(); // no path
  });

  it("counts Edit old/new lines", () => {
    expect(editOf({ file_path: "/a/b.ts", old_string: "1\n2", new_string: "1\n2\n3" }))
      .toMatchObject({ path: "/a/b.ts", added: 3, removed: 2 });
  });

  it("counts a Write as pure additions", () => {
    expect(editOf({ file_path: "/a/new.ts", content: "a\nb\nc" }))
      .toMatchObject({ path: "/a/new.ts", added: 3, removed: 0 });
  });

  it("sums MultiEdit edits", () => {
    expect(editOf({
      file_path: "/a/b.ts",
      edits: [{ old_string: "x", new_string: "y\nz" }, { old_string: "p\nq", new_string: "r" }],
    })).toMatchObject({ path: "/a/b.ts", added: 3, removed: 3 });
  });
});

describe("mergeEdits", () => {
  it("sums repeated edits of the same file", () => {
    expect(mergeEdits([
      { path: "/a.ts", added: 2, removed: 1 },
      { path: "/b.ts", added: 5, removed: 0 },
      { path: "/a.ts", added: 3, removed: 4 },
    ])).toMatchObject([
      { path: "/a.ts", added: 5, removed: 5 },
      { path: "/b.ts", added: 5, removed: 0 },
    ]);
  });
});

describe("fmtDuration", () => {
  it("formats seconds and minutes", () => {
    expect(fmtDuration(0)).toBe("0s");
    expect(fmtDuration(26_400)).toBe("26s");
    expect(fmtDuration(134_000)).toBe("2m 14s");
  });
});

describe("hunkLines", () => {
  it("keeps each edit's before/after text as del/add lines, separated, capped", () => {
    const e = mergeEdits([
      editOf({ file_path: "/a.ts", old_string: "x", new_string: "y\nz" })!,
      editOf({ file_path: "/a.ts", old_string: "p", new_string: "q" })!,
    ]);
    expect(hunkLines(e[0].hunks).lines.map((l) => `${l.kind}:${l.text}`))
      .toEqual(["del:x", "add:y", "add:z", "sep:", "del:p", "add:q"]);
    expect(hunkLines(e[0].hunks, 2)).toMatchObject({ hidden: 4 });
  });
});

describe("isProjectPath", () => {
  it("keeps files under the project and drops the agent's own notes elsewhere", () => {
    expect(isProjectPath("/repo/src/a.ts", "/repo")).toBe(true);
    expect(isProjectPath("src/a.ts", "/repo")).toBe(true);
    expect(isProjectPath("/repo-other/a.ts", "/repo")).toBe(false);
    expect(isProjectPath("/Users/me/.claude/projects/x/memory/note.md", "/repo")).toBe(false);
  });
});
