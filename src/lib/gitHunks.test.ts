import { describe, expect, it } from "vitest";
import { splitHunks } from "./gitHunks";
import { layoutGraph } from "./gitGraph";

const DIFF = `diff --git a/f.txt b/f.txt
index 1111111..2222222 100644
--- a/f.txt
+++ b/f.txt
@@ -1,3 +1,3 @@
-a
+A
 b
 c
@@ -10,2 +10,3 @@ fn
 x
+y
 z
\\ No newline at end of file
`;

describe("splitHunks", () => {
  it("gives each hunk its own patch with the file header", () => {
    const h = splitHunks(DIFF);
    expect(h).toHaveLength(2);
    expect(h[0].patch.startsWith("diff --git a/f.txt b/f.txt\n")).toBe(true);
    expect(h[0].patch).toContain("@@ -1,3 +1,3 @@\n-a\n+A\n b\n c\n");
    expect(h[0].patch).not.toContain("@@ -10");
    expect(h[1].patch).toContain("+++ b/f.txt\n@@ -10,2 +10,3 @@ fn\n x\n+y\n z\n\\ No newline at end of file\n");
    expect([h[0].adds, h[0].dels, h[1].adds, h[1].dels]).toEqual([1, 1, 1, 0]);
  });

  it("returns nothing for a diff without hunks", () => {
    expect(splitHunks("")).toEqual([]);
    expect(splitHunks("diff --git a/x b/x\nBinary files a/x and b/x differ\n")).toEqual([]);
  });
});

describe("layoutGraph", () => {
  it("lays a branch + merge out on two lanes", () => {
    // m merges b into a; a and b both come from root
    const { rows, maxLanes } = layoutGraph([
      { hash: "m", parents: ["a", "b"] },
      { hash: "b", parents: ["root"] },
      { hash: "a", parents: ["root"] },
      { hash: "root", parents: [] },
    ]);
    expect(maxLanes).toBe(2);
    expect(rows.map((r) => r.lane)).toEqual([0, 1, 0, 1]);
    expect(rows[0].isMerge).toBe(true);
    // root is already tracked on b's lane, so a bends into it
    expect(rows[2].down).toContainEqual({ from: 0, to: 1, color: rows[2].color });
  });
});
