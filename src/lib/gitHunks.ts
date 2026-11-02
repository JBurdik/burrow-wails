// Split a single-file `git diff` into standalone one-hunk patches, each one
// valid input for `git apply [--cached] [--reverse] -` (hunk staging).

export interface DiffHunk {
  header: string;
  /** file header + this hunk only */
  patch: string;
  adds: number;
  dels: number;
}

export function splitHunks(diff: string): DiffHunk[] {
  const lines = diff.split("\n");
  if (lines[lines.length - 1] === "") lines.pop();
  const first = lines.findIndex((l) => l.startsWith("@@"));
  // binary / mode-only / empty diffs have no hunks to stage separately
  if (first === -1) return [];
  const fileHeader = lines.slice(0, first).join("\n") + "\n";
  const hunks: DiffHunk[] = [];
  let cur: string[] = [];
  const flush = () => {
    if (!cur.length) return;
    const body = cur.slice(1);
    hunks.push({
      header: cur[0],
      patch: fileHeader + cur.join("\n") + "\n",
      adds: body.filter((l) => l[0] === "+").length,
      dels: body.filter((l) => l[0] === "-").length,
    });
  };
  for (const line of lines.slice(first)) {
    // a second "diff --git" means a multi-file diff; callers pass one file
    if (line.startsWith("diff --git ")) break;
    if (line.startsWith("@@")) {
      flush();
      cur = [line];
    } else cur.push(line);
  }
  flush();
  return hunks;
}
