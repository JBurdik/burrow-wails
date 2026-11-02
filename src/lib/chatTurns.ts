// Turn-level derivations for the chat transcript: how long a turn took and
// which files it changed. Pure so they're testable outside the component.

export interface FileEdit {
  path: string;
  added: number;
  removed: number;
  /** The edit's own before/after text, for the inline diff card. A new file is one hunk with old "". */
  hunks?: { old: string; new: string }[];
}

const str = (v: unknown) => (typeof v === "string" ? v : "");

export function countLines(s: unknown): number {
  return typeof s === "string" && s.length > 0 ? s.split("\n").length : 0;
}

// A tool call counts as a file edit only if it carries edit content — that
// keeps Read/Grep (which also have a path) out of the changed-files summary.
export function editOf(toolInput: Record<string, unknown> | undefined): FileEdit | null {
  if (!toolInput) return null;
  const raw = toolInput.file_path ?? toolInput.path;
  const path = typeof raw === "string" ? raw : "";
  if (!path) return null;
  if (Array.isArray(toolInput.edits)) {
    let added = 0, removed = 0;
    const hunks: { old: string; new: string }[] = [];
    for (const e of toolInput.edits as Record<string, unknown>[]) {
      added += countLines(e.new_string);
      removed += countLines(e.old_string);
      hunks.push({ old: str(e.old_string), new: str(e.new_string) });
    }
    return { path, added, removed, hunks };
  }
  if (typeof toolInput.old_string === "string" || typeof toolInput.new_string === "string")
    return {
      path, added: countLines(toolInput.new_string), removed: countLines(toolInput.old_string),
      hunks: [{ old: str(toolInput.old_string), new: str(toolInput.new_string) }],
    };
  if (typeof toolInput.content === "string")
    return { path, added: countLines(toolInput.content), removed: 0, hunks: [{ old: "", new: toolInput.content }] };
  return null;
}

// Sums repeated edits of the same file into one row, in first-touch order.
export function mergeEdits(edits: FileEdit[]): FileEdit[] {
  const byPath = new Map<string, FileEdit>();
  for (const e of edits) {
    const prev = byPath.get(e.path);
    if (prev) {
      prev.added += e.added;
      prev.removed += e.removed;
      prev.hunks = [...(prev.hunks ?? []), ...(e.hunks ?? [])];
    } else byPath.set(e.path, { ...e, hunks: [...(e.hunks ?? [])] });
  }
  return [...byPath.values()];
}

export function fmtDuration(ms: number): string {
  const s = Math.max(0, Math.round(ms / 1000));
  return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${s % 60}s`;
}

export interface DiffLine { kind: "add" | "del" | "sep"; text: string }

/**
 * A file's hunks as display lines, capped at `max`. Hunks come from the tool
 * input (old_string/new_string), so there are no line numbers and no context —
 * a "sep" line marks the gap between two hunks.
 */
export function hunkLines(hunks: { old: string; new: string }[] | undefined, max = 40): { lines: DiffLine[]; hidden: number } {
  const all: DiffLine[] = [];
  (hunks ?? []).forEach((h, i) => {
    if (i > 0) all.push({ kind: "sep", text: "" });
    if (h.old) for (const t of h.old.split("\n")) all.push({ kind: "del", text: t });
    if (h.new) for (const t of h.new.split("\n")) all.push({ kind: "add", text: t });
  });
  return { lines: all.slice(0, max), hidden: Math.max(0, all.length - max) };
}

/**
 * Whether an edited path belongs to the project the chat works in. The diff
 * card shows only those: an agent also writes its own memory notes
 * (~/.claude/projects/…/memory), plans and scratch files, which are not changes
 * to review. A relative path is relative to the project, so it counts.
 */
export function isProjectPath(path: string, root: string): boolean {
  if (!path.startsWith("/")) return true;
  if (!root) return false;
  const base = root.endsWith("/") ? root : `${root}/`;
  return path === root || path.startsWith(base);
}
