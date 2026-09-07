/**
 * Every `invoke("...")` in the app must name a command the backend knows —
 * and pass it argument names the backend reads.
 *
 * The second half matters more than the first. A wrong command name is loud:
 * a rejected promise the moment that code runs. A wrong ARGUMENT name is
 * silent — `callApp` fills only the parameters `remoteAllowed` names, so an
 * unrecognised key is dropped and the Go method runs on the zero value:
 * `list_terminal_tabs` for workspace 0, `write_text_file` with empty content.
 * Nothing throws, and the call looks like it worked.
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

// Nothing is skipped any more. src/mobile used to be, because it spoke the
// older /ws protocol and its own hand-written dispatch; since phase 6 it goes
// through the same transport and the same table, so it gets the same
// protection — a wire name the phone calls and the table forgot is now caught
// here rather than at 2am on a train.
const SKIP_DIRS = new Set<string>();

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

/** Whitespace and comments. Comments have to be stepped over now that the
 *  scan reads argument OBJECTS: call sites annotate keys inline. */
function skipTrivia(src: string, i: number): number {
  for (;;) {
    while (i < src.length && /\s/.test(src[i])) i++;
    if (src[i] === "/" && src[i + 1] === "/") {
      while (i < src.length && src[i] !== "\n") i++;
      continue;
    }
    if (src[i] === "/" && src[i + 1] === "*") {
      const end = src.indexOf("*/", i + 2);
      i = end === -1 ? src.length : end + 2;
      continue;
    }
    return i;
  }
}

function endOfStringLiteral(src: string, start: number): number {
  const quote = src[start];
  for (let i = start + 1; i < src.length; i++) {
    if (src[i] === "\\") i++;
    else if (src[i] === quote) return i;
  }
  return src.length;
}

/** The backtick closing the template literal at `start`, stepping over
 *  `${…}` interpolations — whose braces must not be counted as object
 *  braces by the callers below. */
function endOfTemplate(src: string, start: number): number {
  for (let i = start + 1; i < src.length; i++) {
    if (src[i] === "\\") {
      i++;
      continue;
    }
    if (src[i] === "`") return i;
    if (src[i] === "$" && src[i + 1] === "{") {
      let depth = 1;
      i += 2;
      while (i < src.length && depth > 0) {
        const ch = src[i];
        if (ch === "{") depth++;
        else if (ch === "}") depth--;
        else if (ch === '"' || ch === "'") i = endOfStringLiteral(src, i);
        else if (ch === "`") i = endOfTemplate(src, i);
        i++;
      }
      i--;
    }
  }
  return src.length;
}

/** The texts of a call's top-level arguments; `open` is its `(`. */
function callArguments(src: string, open: number): string[] {
  const args: string[] = [];
  let depth = 0;
  let start = open + 1;
  for (let i = open + 1; i < src.length; i++) {
    const ch = src[i];
    if (ch === "/" && (src[i + 1] === "/" || src[i + 1] === "*")) {
      i = skipTrivia(src, i) - 1;
      continue;
    }
    if (ch === '"' || ch === "'") {
      i = endOfStringLiteral(src, i);
      continue;
    }
    if (ch === "`") {
      i = endOfTemplate(src, i);
      continue;
    }
    if (ch === "(" || ch === "[" || ch === "{") depth++;
    else if (ch === "]" || ch === "}") depth--;
    else if (ch === ")") {
      if (depth === 0) {
        args.push(src.slice(start, i));
        return args;
      }
      depth--;
    } else if (ch === "," && depth === 0) {
      args.push(src.slice(start, i));
      start = i + 1;
    }
  }
  args.push(src.slice(start));
  return args;
}

/** One object entry ends at the next top-level `,` or at the closing `}`. */
function endOfEntry(text: string, i: number): number {
  let depth = 0;
  while (i < text.length) {
    const ch = text[i];
    if (ch === "/" && (text[i + 1] === "/" || text[i + 1] === "*")) {
      i = skipTrivia(text, i);
      continue;
    }
    if (ch === '"' || ch === "'") {
      i = endOfStringLiteral(text, i) + 1;
      continue;
    }
    if (ch === "`") {
      i = endOfTemplate(text, i) + 1;
      continue;
    }
    if (ch === "(" || ch === "[" || ch === "{") depth++;
    else if (ch === ")" || ch === "]") depth--;
    else if (ch === "}") {
      if (depth === 0) return i;
      depth--;
    } else if (ch === "," && depth === 0) return i;
    i++;
  }
  return i;
}

/**
 * The top-level keys of an object literal, or null when the argument is not
 * an object literal at all (a variable, a conditional, an omitted second
 * argument) — nothing to compare, so nothing is claimed.
 *
 * A `...spread` and a computed `[key]` contribute no name: they are skipped
 * rather than making the whole call site unreadable, so the literal keys
 * beside them are still checked.
 */
function objectLiteralKeys(text: string): string[] | null {
  let i = skipTrivia(text, 0);
  if (text[i] !== "{") return null;
  i++;
  const keys: string[] = [];
  for (;;) {
    i = skipTrivia(text, i);
    const ch = text[i];
    if (ch === undefined || ch === "}") return keys;
    if (ch === ",") {
      i++;
      continue;
    }
    if (text.startsWith("...", i) || ch === "[") {
      i = endOfEntry(text, i);
      continue;
    }
    if (ch === '"' || ch === "'") {
      const end = endOfStringLiteral(text, i);
      keys.push(text.slice(i + 1, end));
      i = endOfEntry(text, end + 1);
      continue;
    }
    const ident = /^[A-Za-z_$][\w$]*/.exec(text.slice(i));
    if (!ident) return keys;
    keys.push(ident[0]);
    i = endOfEntry(text, i + ident[0].length);
  }
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
  /** Top-level keys of the args object literal, or null when the call passes
   *  something this scan cannot read (or no second argument at all). */
  argKeys: string[] | null;
}

function invokeCallSites(file: string, src: string): CallSite[] {
  const sites: CallSite[] = [];
  const re = /\binvoke\b/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(src))) {
    let i = skipSpace(src, m.index + "invoke".length);
    i = skipSpace(src, skipTypeArgs(src, i));
    if (src[i] !== "(") continue;
    const args = callArguments(src, i);
    const argKeys = args.length > 1 ? objectLiteralKeys(args[1]) : null;
    for (const name of stringLiterals(args[0] ?? "")) {
      sites.push({ file, name, argKeys });
    }
  }
  return sites;
}

/** Go's `remoteAllowed`, as wire name -> the argument names it declares.
 *  Entries look like
 *  `"create_pty": {Method: "CreatePty", Args: []string{"id", …}, …}`;
 *  `remoteDenied`'s entries are `"CreatePty": "reason"` and never match.
 *  `[\s\S]*?` because a long Args list (claude_start) wraps across lines. */
function remoteAllowedTable(): Map<string, string[]> {
  const src = readFileSync(REMOTE_API, "utf8");
  const table = new Map<string, string[]>();
  const re = /"([a-z0-9_]+)":\s*\{Method:\s*"\w+",\s*Args:\s*(?:nil|\[\]string\{([\s\S]*?)\})/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(src))) {
    // Not stringLiterals(): argument names are camelCase (`chatId`,
    // `resumeSessionId`), which WIRE_NAME deliberately rejects.
    table.set(m[1], m[2] ? [...m[2].matchAll(/"([A-Za-z0-9_]+)"/g)].map((a) => a[1]) : []);
  }
  return table;
}

/**
 * Argument names the check must not report. A key is either a whole command
 * (its call sites are reshaped before dispatch, so comparing them means
 * nothing) or one `command.argument` pair. Keep the reason with the entry.
 */
const ARGUMENT_SHAPE_EXCEPTIONS: Record<string, string> = {
  // Reshaped in core.ts before dispatch: call sites pass the AcpStartOpts
  // fields flat, and core.ts packs them into the single `opts` key the table
  // names (one Go struct parameter cannot be spread across table entries).
  acp_start: "core.ts packs the flat call-site fields into `opts`",
  // Also reshaped in core.ts, which supplies the `foldedOrd` sentinel the
  // call sites may omit.
  save_chat_messages: "core.ts fills in foldedOrd",
  // Harmless leftover, and the one case where dropping the value is right:
  // CreateWorktree (git.go) looks the parent workspace up from repoPath
  // itself, so it has no parent-id parameter for this to land in.
  "create_worktree.parentId": "CreateWorktree derives the parent from repoPath",
};

describe("invoke() command surface", () => {
  const allowed = remoteAllowedTable();
  const sites = walk(SRC).flatMap((f) => invokeCallSites(f.slice(ROOT.length + 1), readFileSync(f, "utf8")));

  it("parsed the Go command table", () => {
    // If the table's formatting ever changes shape, the regex above would
    // match nothing and every assertion below would pass vacuously.
    expect(allowed.size).toBeGreaterThan(50);
    expect(allowed.has("create_pty")).toBe(true);
    expect(allowed.has("list_workspaces")).toBe(true);
    // Same for the ARGUMENT names: an Args list that stopped parsing would
    // read as "this command takes nothing", and the argument check below
    // would report every call site instead of passing vacuously — but a
    // command with a genuinely empty list must still read as empty.
    expect(allowed.get("create_pty")).toEqual(["id", "cwd", "cols", "rows"]);
    expect(allowed.get("list_workspaces")).toEqual([]);
    // The one entry whose Args list wraps across several lines.
    expect(allowed.get("claude_start")).toContain("appendSystemPrompt");
  });

  it("read the argument objects at the call sites", () => {
    // Same guard from the other side: if the object scan broke, every site
    // would read as "no args object" and the check below would pass while
    // seeing nothing.
    expect(sites.filter((s) => s.argKeys !== null).length).toBeGreaterThan(100);
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

  /**
   * A wrong command NAME is loud — a rejected promise the moment that corner
   * of the UI runs. A wrong ARGUMENT name is silent: callApp only fills the
   * parameters the table names, so an unrecognised key is dropped and the Go
   * method gets the zero value. `list_terminal_tabs` for workspace 0,
   * `write_text_file` with empty content, `create_pty` in the wrong cwd —
   * all of them "work", and none of them do what the call site asked.
   *
   * Only keys present at the call site and absent from the table are
   * reported. An OMITTED argument is legitimate and common (core.ts's
   * `args.cwd ?? ""` behaviour is preserved by design: absent means the zero
   * value), so a missing key is not an error here.
   */
  it("passes only argument names the backend reads", () => {
    const wrong = sites
      .filter((s) => s.argKeys !== null && !(s.name in ARGUMENT_SHAPE_EXCEPTIONS))
      .flatMap((s) => {
        const declared = allowed.get(s.name);
        if (!declared) return []; // an unknown command; reported above
        return s.argKeys!
          .filter((key) => !declared.includes(key))
          .filter((key) => !(`${s.name}.${key}` in ARGUMENT_SHAPE_EXCEPTIONS))
          .map(
            (key) =>
              `${s.file}: ${s.name} is passed "${key}", which remoteapi.go does not name ` +
              `(it names: ${declared.join(", ") || "no arguments"}) — the value is silently dropped`,
          );
      });
    expect([...new Set(wrong)]).toEqual([]);
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
