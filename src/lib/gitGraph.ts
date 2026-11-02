// Commit-graph lane layout (from GitPaw). Lanes keep their column while alive
// so pass-through lines stay straight.

export interface GraphCommit {
  hash: string;
  parents: string[];
}

export interface GraphEdge { from: number; to: number; color: number }

export interface GraphRow {
  lane: number;
  color: number;
  /** top half: lanes converging into the node, or straight pass-through */
  up: GraphEdge[];
  /** bottom half: node/lane to lane at row bottom */
  down: GraphEdge[];
  width: number;
  isMerge: boolean;
}

export function layoutGraph(commits: GraphCommit[]): { rows: GraphRow[]; maxLanes: number } {
  // lanes[i] = hash expected next in that column (null = free)
  let lanes: (string | null)[] = [];
  const colors: number[] = [];
  let nextColor = 0;
  let maxLanes = 1;
  const rows: GraphRow[] = [];
  const known = new Set(commits.map((c) => c.hash));

  const alloc = (hash: string, color: number) => {
    let i = lanes.indexOf(null);
    if (i === -1) i = lanes.length;
    lanes[i] = hash;
    colors[i] = color;
    return i;
  };

  for (const c of commits) {
    const before = lanes.slice();
    const beforeColors = colors.slice();

    let lane = lanes.indexOf(c.hash);
    if (lane === -1) lane = alloc(c.hash, nextColor++);
    const color = colors[lane];
    const up: GraphEdge[] = [];
    const down: GraphEdge[] = [];

    before.forEach((h, i) => {
      if (h === c.hash) up.push({ from: i, to: lane, color: beforeColors[i] });
      else if (h !== null) up.push({ from: i, to: i, color: beforeColors[i] });
    });
    lanes = lanes.map((h, i) => (h === c.hash && i !== lane ? null : h));

    // parents outside the loaded window are dropped
    const parents = c.parents.filter((p) => known.has(p));
    if (parents.length === 0) lanes[lane] = null;
    parents.forEach((p, idx) => {
      if (idx === 0) {
        const existing = lanes.indexOf(p);
        if (existing !== -1 && existing !== lane) {
          // first parent already tracked elsewhere: bend into that lane
          lanes[lane] = null;
          down.push({ from: lane, to: existing, color });
        } else {
          lanes[lane] = p;
          down.push({ from: lane, to: lane, color });
        }
      } else {
        let j = lanes.indexOf(p);
        if (j === -1) j = alloc(p, nextColor++);
        down.push({ from: lane, to: j, color: colors[j] });
      }
    });
    lanes.forEach((h, i) => {
      if (h !== null && i !== lane && before[i] === h) down.push({ from: i, to: i, color: colors[i] });
    });

    while (lanes.length && lanes[lanes.length - 1] === null) lanes.pop();
    const width = Math.max(before.length, lanes.length, lane + 1);
    maxLanes = Math.max(maxLanes, width);
    rows.push({ lane, color, up, down, width, isMerge: c.parents.length > 1 });
  }
  return { rows, maxLanes };
}

/** One file of a multi-file `git show` patch. */
export interface PatchFile {
  path: string;
  /** A | M | D | R */
  status: string;
  adds: number;
  dels: number;
  /** this file's part of the patch, starting at its `diff --git` line */
  text: string;
}

/** Split a unified multi-file patch into per-file pieces with +/- counts. */
export function splitPatch(patch: string): PatchFile[] {
  return patch.split(/^(?=diff --git )/m).filter((p) => p.startsWith("diff --git ")).map((text) => {
    const lines = text.split("\n");
    const head = lines[0].match(/^diff --git a\/(.*) b\/(.*)$/);
    let path = head?.[2] ?? lines[0].slice(11);
    let status = "M";
    let adds = 0, dels = 0, inHunk = false;
    for (const l of lines.slice(1)) {
      if (!inHunk) {
        if (l.startsWith("new file mode")) status = "A";
        else if (l.startsWith("deleted file mode")) status = "D";
        else if (l.startsWith("rename to ")) { status = "R"; path = l.slice(10); }
        else if (l.startsWith("+++ b/")) path = l.slice(6);
        else if (l.startsWith("--- a/") && status === "D") path = l.slice(6);
        else if (l.startsWith("@@")) inHunk = true;
        continue;
      }
      if (l.startsWith("+")) adds++;
      else if (l.startsWith("-")) dels++;
    }
    return { path, status, adds, dels, text };
  });
}
