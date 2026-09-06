/**
 * Ambient module patches for the two Node builtins boundary.test.ts imports
 * (`node:fs`, `node:path`) — nothing else.
 *
 * This is a deliberate, narrow local stand-in for `@types/node`, not an
 * oversight: installing the real package auto-includes ALL of its global
 * ambient typings project-wide (e.g. it overloads `setTimeout` to return
 * `NodeJS.Timeout`), which collides with the DOM `setTimeout` typing
 * `XTerm.vue` relies on. That is a program-wide collision regardless of
 * which file imports `@types/node` — TypeScript's global ambient
 * declarations always apply to the whole compilation, not just the
 * referencing file.
 *
 * These two `declare module` blocks have the same "applies everywhere"
 * property (an ambient module declaration patches the program's
 * module-resolution table, not a single file's), and there's no way around
 * that: TypeScript only lets a file with top-level import/export *augment*
 * an already-known ambient module, not introduce a brand-new one — so this
 * can't be inlined into boundary.test.ts. The residual risk is narrower than
 * `@types/node`'s: this only makes the literal specifiers "node:fs" and
 * "node:path" resolve (with only the functions actually called below), it
 * does not touch any global identifier like `setTimeout` or `process`.
 *
 * Keep this file's signatures as narrow as the call sites that need them —
 * do not "complete" it into a general-purpose Node shim; reach for the real
 * `@types/node` (accepting the tradeoff above, or scoping it via a separate
 * tsconfig) if more of the Node API surface is ever needed here.
 */
declare module "node:fs" {
  export function readdirSync(path: string): string[];
  export function readFileSync(path: string, encoding: string): string;
  // commandSurface.test.ts walks src/ recursively and has to tell a
  // directory from a file; this is the one method it needs for that.
  export function statSync(path: string): { isDirectory(): boolean };
}

declare module "node:path" {
  export function join(...paths: string[]): string;
}
