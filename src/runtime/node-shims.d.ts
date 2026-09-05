/**
 * Minimal ambient typings for the handful of Node builtins the import-boundary
 * test needs to read its own directory. Deliberately not `@types/node`: that
 * package augments global ambient types project-wide (e.g. `setTimeout`'s
 * return type), which collides with the browser typings the rest of the app
 * (and vue-tsc) relies on. This file only declares what boundary.test.ts calls.
 */

declare const __dirname: string;

declare module "node:fs" {
  export function readdirSync(path: string): string[];
  export function readFileSync(path: string, encoding: string): string;
}

declare module "node:path" {
  export function join(...paths: string[]): string;
}
