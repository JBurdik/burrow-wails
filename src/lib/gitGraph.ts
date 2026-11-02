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
