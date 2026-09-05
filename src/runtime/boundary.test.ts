import { describe, it, expect } from "vitest";
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";

// src/runtime is shared by the desktop shell and the PWA. If it can reach into
// components or stores, the two clients stop being able to share it — which is
// how src/mobile ended up with its own copy of the status logic.
const FORBIDDEN = ["@/components", "@/views", "@/stores", "@/mobile", "../components", "../stores", "xterm"];

describe("src/runtime import boundary", () => {
  it("imports nothing from the UI layer", () => {
    const dir = join(__dirname);
    for (const f of readdirSync(dir)) {
      if (!f.endsWith(".ts") || f.endsWith(".test.ts")) continue;
      const src = readFileSync(join(dir, f), "utf8");
      for (const bad of FORBIDDEN) {
        expect(src.includes(`from "${bad}`), `${f} imports ${bad}`).toBe(false);
      }
    }
  });
});
