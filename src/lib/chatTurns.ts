// Turn-level derivations for the chat transcript: how long a turn took and
// which files it changed. Pure so they're testable outside the component.

export interface FileEdit {
  path: string;
  added: number;
  removed: number;
}

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
    for (const e of toolInput.edits as Record<string, unknown>[]) {
      added += countLines(e.new_string);
      removed += countLines(e.old_string);
    }
    return { path, added, removed };
  }
  if (typeof toolInput.old_string === "string" || typeof toolInput.new_string === "string")
    return { path, added: countLines(toolInput.new_string), removed: countLines(toolInput.old_string) };
  if (typeof toolInput.content === "string") return { path, added: countLines(toolInput.content), removed: 0 };
  return null;
}

// Sums repeated edits of the same file into one row, in first-touch order.
export function mergeEdits(edits: FileEdit[]): FileEdit[] {
  const byPath = new Map<string, FileEdit>();
  for (const e of edits) {
    const prev = byPath.get(e.path);
    if (prev) { prev.added += e.added; prev.removed += e.removed; }
    else byPath.set(e.path, { ...e });
  }
  return [...byPath.values()];
}

export function fmtDuration(ms: number): string {
  const s = Math.max(0, Math.round(ms / 1000));
  return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${s % 60}s`;
}
