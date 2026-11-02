import type { ChatMessage } from "@/lib/chatTypes";

// The files a chat wrote, derived from its own transcript's tool calls — the
// Right Panel's Outputs surface. Unlike the Diff surface (the whole workspace),
// this is only what THIS chat touched. Pure so it is testable without a session.

export type OutputOp = "created" | "edited" | "deleted";

export interface ChatOutputFile {
  path: string;
  /** The op that best describes the file overall: created wins over a later edit. */
  op: OutputOp;
  /** How many tool calls touched it. */
  edits: number;
}

const CLAUDE_EDIT_TOOLS = new Set(["Edit", "MultiEdit", "NotebookEdit"]);

function str(v: unknown): string {
  return typeof v === "string" ? v : "";
}

/** One tool row → the (path, op) pairs it wrote. Failed calls wrote nothing. */
function opsOf(m: ChatMessage): { path: string; op: OutputOp; from?: string }[] {
  if (m.role !== "tool" || m.toolFailed) return [];
  const input = m.toolInput ?? {};
  // Claude: the name is the raw tool identifier.
  if (m.toolRawName && m.text === "Write") {
    const path = str(input.file_path);
    return path ? [{ path, op: "created" }] : [];
  }
  if (m.toolRawName && CLAUDE_EDIT_TOOLS.has(m.text)) {
    const path = str(input.file_path) || str(input.notebook_path);
    return path ? [{ path, op: "edited" }] : [];
  }
  // Codex `fileChange` (acp.go codexToolCall, app-server v2 FileUpdateChange):
  // {changes: [{path, diff, kind: {type: "add"} | {type: "delete"} |
  // {type: "update", move_path: string | null}}]}. A non-null move_path is a
  // rename: the file now lives there. A bare-string kind is tolerated.
  if (Array.isArray(input.changes)) {
    const out: { path: string; op: OutputOp; from?: string }[] = [];
    for (const c of input.changes as Record<string, unknown>[]) {
      const path = str(c?.path);
      if (!path) continue;
      const kindObj = c.kind && typeof c.kind === "object" ? (c.kind as Record<string, unknown>) : undefined;
      const kind = str(c.kind) || str(kindObj?.type);
      if (kind === "add") out.push({ path, op: "created" });
      else if (kind === "delete") out.push({ path, op: "deleted" });
      else {
        const moved = str(kindObj?.move_path);
        out.push(moved && moved !== path ? { path: moved, op: "edited", from: path } : { path, op: "edited" });
      }
    }
    return out;
  }
  // Generic ACP: only a rawInput shaped like an edit counts — a path alone is
  // also what a read carries.
  if (!m.toolRawName) {
    const path = str(input.file_path) || str(input.path);
    if (!path) return [];
    if (typeof input.old_string === "string" || typeof input.new_string === "string" || Array.isArray(input.edits))
      return [{ path, op: "edited" }];
    if (typeof input.content === "string") return [{ path, op: "created" }];
  }
  return [];
}

/** Dedupe by path, newest touch first. */
export function chatOutputs(messages: readonly ChatMessage[]): ChatOutputFile[] {
  const byPath = new Map<string, ChatOutputFile>();
  for (const m of messages) {
    for (const { path, op, from } of opsOf(m)) {
      // A rename carries the old path's history to the new one.
      let prev = byPath.get(path);
      if (from && byPath.has(from)) {
        prev = byPath.get(from);
        byPath.delete(from);
      }
      // Re-insert so Map order tracks the last touch.
      byPath.delete(path);
      byPath.set(path, {
        path,
        op: op === "deleted" ? "deleted" : prev?.op === "created" ? "created" : op,
        edits: (prev?.edits ?? 0) + 1,
      });
    }
  }
  return [...byPath.values()].reverse();
}
