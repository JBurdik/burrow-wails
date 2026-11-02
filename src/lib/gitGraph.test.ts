import { describe, it, expect } from "vitest";
import { splitPatch } from "./gitGraph";

describe("splitPatch", () => {
  it("splits files and counts +/- per file", () => {
    const patch = [
      "diff --git a/a.ts b/a.ts",
      "index 1..2 100644",
      "--- a/a.ts",
      "+++ b/a.ts",
      "@@ -1,2 +1,2 @@",
      "-old",
      "+new",
      "+more",
      " same",
      "diff --git a/gone.txt b/gone.txt",
      "deleted file mode 100644",
      "--- a/gone.txt",
      "+++ /dev/null",
      "@@ -1 +0,0 @@",
      "--- dashes",
      "diff --git a/x b/y",
      "similarity index 100%",
      "rename from x",
      "rename to y",
      "",
    ].join("\n");
    const files = splitPatch(patch);
    expect(files.map((f) => [f.path, f.status, f.adds, f.dels])).toEqual([
      ["a.ts", "M", 2, 1],
      ["gone.txt", "D", 0, 1],
      ["y", "R", 0, 0],
    ]);
    expect(files[0].text.startsWith("diff --git a/a.ts")).toBe(true);
  });
});
