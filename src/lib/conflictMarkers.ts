/** A file with git conflict markers, split into plain text and conflict blocks (VS Code-style inline resolve). */
export type ConflictSegment =
  | { kind: "text"; lines: string[] }
  | { kind: "conflict"; ours: string[]; theirs: string[]; base: string[] | null; oursLabel: string; theirsLabel: string };

export type Resolution = "ours" | "theirs" | "both" | "both-reversed";

/** Splits on `\n`; an unterminated block (no `>>>>>>>`) is kept as plain text. */
export function parseConflicts(text: string): ConflictSegment[] {
  const lines = text.split("\n");
  const out: ConflictSegment[] = [];
  const plain = (ls: string[]) => {
    if (!ls.length) return;
    const last = out[out.length - 1];
    if (last?.kind === "text") last.lines.push(...ls);
    else out.push({ kind: "text", lines: [...ls] });
  };
  let i = 0;
  while (i < lines.length) {
    if (!lines[i].startsWith("<<<<<<<")) { plain([lines[i++]]); continue; }
    const start = i;
    const oursLabel = lines[i].slice(7).trim();
    const ours: string[] = [], theirs: string[] = [];
    let base: string[] | null = null;
    let part: string[] = ours;
    let end = -1;
    for (i++; i < lines.length; i++) {
      const l = lines[i];
      if (part === ours && l.startsWith("|||||||")) { base = []; part = base; }
      else if (part !== theirs && l.startsWith("=======")) part = theirs;
      else if (part === theirs && l.startsWith(">>>>>>>")) { end = i; break; }
      else part.push(l);
    }
    if (end < 0) { plain(lines.slice(start)); break; }
    out.push({ kind: "conflict", ours, theirs, base, oursLabel, theirsLabel: lines[end].slice(7).trim() });
    i = end + 1;
  }
  return out;
}

/** Re-joins segments, replacing conflict `index` (counted among conflicts only) by the chosen side. */
export function resolveConflict(segs: ConflictSegment[], index: number, how: Resolution): string {
  let n = -1;
  const lines = segs.flatMap((s) => {
    if (s.kind === "text") return s.lines;
    if (++n !== index) return render(s);
    return how === "ours" ? s.ours : how === "theirs" ? s.theirs : how === "both" ? [...s.ours, ...s.theirs] : [...s.theirs, ...s.ours];
  });
  return lines.join("\n");
}

function render(s: Extract<ConflictSegment, { kind: "conflict" }>): string[] {
  return [
    `<<<<<<< ${s.oursLabel}`.trimEnd(), ...s.ours,
    ...(s.base ? ["|||||||", ...s.base] : []),
    "=======", ...s.theirs,
    `>>>>>>> ${s.theirsLabel}`.trimEnd(),
  ];
}
