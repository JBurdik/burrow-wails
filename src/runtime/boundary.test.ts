import { describe, it, expect } from "vitest";
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";

// This file has top-level imports (making it a module), so a plain `declare
// const` here is scoped to this file only — it does not leak into the rest
// of the program the way a standalone .d.ts "script" file would. (The
// `node:fs`/`node:path` typings this file also needs can't follow it here:
// TypeScript only lets a module file *augment* an existing ambient module,
// not create one from scratch, so those live in ./nodeBuiltins.d.ts instead
// — see that file for why they aren't just `@types/node`.)
declare const __dirname: string;

// src/runtime is shared by the desktop shell and the PWA. If it can reach into
// components or stores, the two clients stop being able to share it — which is
// how src/mobile ended up with its own copy of the status logic.
const UI_LAYER_TARGETS = ["components", "views", "stores", "mobile"];
const FORBIDDEN = [
  // Symmetric on purpose: a bad import can spell the UI layer either as the
  // `@/`-alias or as a relative path (src/runtime is flat today, so `../x` is
  // the only relative depth that reaches out of it).
  ...UI_LAYER_TARGETS.map((t) => `@/${t}`),
  ...UI_LAYER_TARGETS.map((t) => `../${t}`),
  "xterm",
];

// Captures the quoted module specifier out of every import/export shape we
// care about, in either quote style:
//   import Foo from "x"; import type { Foo } from 'x'; export { Foo } from "x";
//   import "x";                         (side-effect import, no `from`)
// The `[^'"(]` exclusion keeps this from also swallowing a dynamic import's
// parenthesis, so it never double-matches with DYNAMIC_IMPORT_RE below.
const STATIC_IMPORT_RE = /\b(?:import|export)\b[^'"(]*?["']([^"']+)["']/g;
// import("x") / import('x')
const DYNAMIC_IMPORT_RE = /\bimport\s*\(\s*["']([^"']+)["']\s*\)/g;

function importSpecifiers(src: string): string[] {
  const specifiers: string[] = [];
  for (const re of [STATIC_IMPORT_RE, DYNAMIC_IMPORT_RE]) {
    re.lastIndex = 0;
    let match: RegExpExecArray | null;
    while ((match = re.exec(src))) specifiers.push(match[1]);
  }
  return specifiers;
}

describe("src/runtime import boundary", () => {
  it("imports nothing from the UI layer", () => {
    const dir = join(__dirname);
    for (const f of readdirSync(dir)) {
      if (!f.endsWith(".ts") || f.endsWith(".test.ts")) continue;
      const src = readFileSync(join(dir, f), "utf8");
      for (const specifier of importSpecifiers(src)) {
        for (const bad of FORBIDDEN) {
          expect(specifier.includes(bad), `${f} imports "${specifier}" (matches "${bad}")`).toBe(false);
        }
      }
    }
  });
});
