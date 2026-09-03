import { describe, expect, it } from "vitest";
import { editOf, fmtDuration, mergeEdits } from "./chatTurns";

describe("editOf", () => {
  it("ignores tool calls with no edit content (Read/Grep)", () => {
    expect(editOf({ file_path: "/a/b.ts" })).toBeNull();
    expect(editOf({ path: "/a", pattern: "foo" })).toBeNull();
    expect(editOf(undefined)).toBeNull();
    expect(editOf({ content: "x" })).toBeNull(); // no path
  });

  it("counts Edit old/new lines", () => {
    expect(editOf({ file_path: "/a/b.ts", old_string: "1\n2", new_string: "1\n2\n3" }))
      .toEqual({ path: "/a/b.ts", added: 3, removed: 2 });
  });

  it("counts a Write as pure additions", () => {
    expect(editOf({ file_path: "/a/new.ts", content: "a\nb\nc" }))
      .toEqual({ path: "/a/new.ts", added: 3, removed: 0 });
  });

  it("sums MultiEdit edits", () => {
    expect(editOf({
      file_path: "/a/b.ts",
      edits: [{ old_string: "x", new_string: "y\nz" }, { old_string: "p\nq", new_string: "r" }],
    })).toEqual({ path: "/a/b.ts", added: 3, removed: 3 });
  });
});

describe("mergeEdits", () => {
  it("sums repeated edits of the same file", () => {
    expect(mergeEdits([
      { path: "/a.ts", added: 2, removed: 1 },
      { path: "/b.ts", added: 5, removed: 0 },
      { path: "/a.ts", added: 3, removed: 4 },
    ])).toEqual([
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
