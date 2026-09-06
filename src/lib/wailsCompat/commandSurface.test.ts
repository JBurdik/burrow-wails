/**
 * Every `invoke("...")` in the app must name a command the backend knows.
 *
 * `TestRemoteSurfaceIsExhaustive` (src-wails/remoteapi_test.go) already runs
 * the other way — from Go's `App` methods to the table — so no method sneaks
 * onto the wire by accident. Nothing ran from the frontend's call sites *to*
 * the table, and that is the direction the switch-to-socket flip can break: a
 * wire name the old 130-case switch handled but `remoteAllowed` never learned
 * is now a rejected promise at runtime, in whatever corner of the UI happens
 * to call it.
 *
 * This test lives on the TypeScript side rather than in Go because the two
 * things it compares are both TypeScript: the call sites, and core.ts's
 * `CLIENT_SIDE_COMMANDS` (the commands invoke() answers itself). A Go test
 * would have to keep its own copy of that exception list, which is exactly
 * the kind of second source of truth this whole task is deleting. Reading
 * remoteapi.go from here is one-directional and cheap — only the table's
 * keys, which are plain string literals.
 *
 * Known limit: this sees string literals. A command name assembled at
 * runtime (`invoke(\`${prefix}_stop\`)`) is invisible to it — so the scan
 * fails loudly on a template literal with an interpolation rather than
 * quietly skipping one. Conditional call sites
 * (`invoke(a ? "acp_stop" : "codex_stop")`) are fine: both branches are
 * literals and both get checked.
 */
import { describe, it, expect } from "vitest";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import { CLIENT_SIDE_COMMANDS } from "./core";

declare const __dirname: string;

const ROOT = join(__dirname, "..", "..", "..");
const SRC = join(ROOT, "src");
const REMOTE_API = join(ROOT, "src-wails", "remoteapi.go");

// src/mobile is the PWA, which talks to the older /ws protocol and its own
// hand-written dispatch (httpserver.go) — a later phase moves it over.
const SKIP_DIRS = new Set(["mobile"]);

/**
 * Wire names no backend implements and none is planned to, so they are not
 * gaps this test should report. Keep the reason with the entry — an entry
 * here is a decision, not a TODO.
 */
const KNOWN_UNIMPLEMENTED: Record<string, string> = {
  // Leftover from the Rust backend: there is no `BurrowMcpFlag` method in
  // src-wails at all, and there never was on this branch. XTerm.vue's call
  // site is wrapped in try/catch and launches the agent without the MCP flag
  // when it fails, which is what has been happening all along — the old
  // switch threw "no Go binding yet" for it, the socket now answers
  // "unknown_command". Same outcome; listed so this test reports the real
  // gaps instead of this long-standing one.
  burrow_mcp_flag: "no Go method exists; XTerm.vue catches the failure and launches without MCP tools",
};

function walk(dir: string, out: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) {
      if (!SKIP_DIRS.has(entry)) walk(full, out);
      continue;
    }
    // Tests are not shipped code, and src/runtime/transport.test.ts calls
    // `t.invoke("answer")` on a fake transport — a command name that is not
    // meant to exist.
    if (/\.(test|spec)\.ts$/.test(entry)) continue;
    if (/\.(ts|tsx|vue)$/.test(entry)) out.push(full);
  }
  return out;
}

function skipSpace(src: string, i: number): number {
  while (i < src.length && /\s/.test(src[i])) i++;
  return i;
}

/** Step over `<Record<string, string>>` / `<Omit<T, "checkedAt">>` so their
 *  own commas and string literals are never mistaken for arguments. */
function skipTypeArgs(src: string, start: number): number {
  if (src[start] !== "<") return start;
  let depth = 0;
  for (let i = start; i < src.length; i++) {
    const ch = src[i];
    if (ch === "<") depth++;
    else if (ch === ">") {
      depth--;
      if (depth === 0) return i + 1;
    } else if (ch === "(" || ch === ";" || ch === "\n") {
      return start; // a comparison, not a type-argument list
    }
  }
  return start;
}

/** The text of the call's first argument: from just after `(` to the first
 *  top-level comma or the closing paren. */
function firstArgument(src: string, start: number): string {
  let depth = 0;
  for (let i = start; i < src.length; i++) {
    const ch = src[i];
    if (ch === '"' || ch === "'" || ch === "`") {
      i = endOfStringLiteral(src, i);
      continue;
    }
    if (ch === "(" || ch === "[" || ch === "{") depth++;
    else if (ch === "]" || ch === "}") depth--;
    else if (ch === ")") {
      if (depth === 0) return src.slice(start, i);
      depth--;
    } else if (ch === "," && depth === 0) return src.slice(start, i);
  }
  return src.slice(start);
}

function endOfStringLiteral(src: string, start: number): number {
  const quote = src[start];
  for (let i = start + 1; i < src.length; i++) {
    if (src[i] === "\\") i++;
    else if (src[i] === quote) return i;
  }
  return src.length;
}

// Every key in `remoteAllowed` has this shape, and the "parsed the Go
// command table" case below asserts the table is read with the same pattern.
// It is what lets the scan take a conditional call site
// (`invoke(s.transport === "claude-cli" ? "claude_stop" : "acp_stop")`)
// without also reporting the operand it compared against — a command name
// never contains a hyphen. The cost is that a hyphenated typo would be
// skipped rather than reported; a snake_case one still is.
const WIRE_NAME = /^[a-z0-9_]+$/;

function stringLiterals(text: string): string[] {
  const out: string[] = [];
  for (let i = 0; i < text.length; i++) {
    const ch = text[i];
    if (ch !== '"' && ch !== "'" && ch !== "`") continue;
    const end = endOfStringLiteral(text, i);
    const literal = text.slice(i + 1, end);
    // Interpolations are kept so the "built at runtime" case can report them.
    if (WIRE_NAME.test(literal) || literal.includes("${")) out.push(literal);
    i = end;
  }
  return out;
}

interface CallSite {
  file: string;
  name: string;
}

function invokeCallSites(file: string, src: string): CallSite[] {
  const sites: CallSite[] = [];
  const re = /\binvoke\b/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(src))) {
    let i = skipSpace(src, m.index + "invoke".length);
    i = skipSpace(src, skipTypeArgs(src, i));
    if (src[i] !== "(") continue;
    for (const name of stringLiterals(firstArgument(src, i + 1))) {
      sites.push({ file, name });
    }
  }
  return sites;
}

/** The keys of Go's `remoteAllowed`. Entries look like
 *  `"create_pty": {Method: "CreatePty", ...}`; `remoteDenied`'s entries are
 *  `"CreatePty": "reason"` and never match. */
function remoteAllowedNames(): Set<string> {
  const src = readFileSync(REMOTE_API, "utf8");
  const names = new Set<string>();
  const re = /^\s*"([a-z0-9_]+)":\s*\{Method:/gm;
  let m: RegExpExecArray | null;
  while ((m = re.exec(src))) names.add(m[1]);
  return names;
}

describe("invoke() command surface", () => {
  const allowed = remoteAllowedNames();
  const sites = walk(SRC).flatMap((f) => invokeCallSites(f.slice(ROOT.length + 1), readFileSync(f, "utf8")));

  it("parsed the Go command table", () => {
    // If the table's formatting ever changes shape, the regex above would
    // match nothing and every assertion below would pass vacuously.
    expect(allowed.size).toBeGreaterThan(50);
    expect(allowed.has("create_pty")).toBe(true);
    expect(allowed.has("list_workspaces")).toBe(true);
  });

  it("found the call sites", () => {
    expect(sites.length).toBeGreaterThan(50);
  });

  it("names no command built at runtime", () => {
    const dynamic = sites.filter((s) => s.name.includes("${"));
    expect(dynamic.map((s) => `${s.file}: ${s.name}`)).toEqual([]);
  });

  it("only calls commands the backend knows", () => {
    const missing = sites
      .filter((s) => !allowed.has(s.name))
      .filter((s) => !CLIENT_SIDE_COMMANDS.has(s.name))
      .filter((s) => !(s.name in KNOWN_UNIMPLEMENTED))
      .map((s) => `${s.file}: invoke("${s.name}") — add it to remoteAllowed in src-wails/remoteapi.go`);
    expect([...new Set(missing)]).toEqual([]);
  });

  it("keeps the client-side exceptions off the wire", () => {
    // A command answered here AND named in the table would mean two
    // implementations of one wire name, with only core.ts's switch order
    // deciding which one runs.
    for (const name of CLIENT_SIDE_COMMANDS) {
      expect(allowed.has(name), `${name} is both client-side and in remoteAllowed`).toBe(false);
    }
  });
});
