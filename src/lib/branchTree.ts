/** Branch names grouped by their `/` segments into a collapsible tree, flattened to rows for rendering. */
export type BranchRow<T> =
  | { kind: "folder"; key: string; label: string; depth: number; count: number; open: boolean }
  | { kind: "leaf"; key: string; label: string; depth: number; item: T };

interface Node<T> { folders: Map<string, Node<T>>; leaves: { label: string; item: T }[]; count: number }

/**
 * `name(item)` is the path to split (e.g. `JB/fix/WEB-1`). Folders come before leaves at each level,
 * otherwise input order is kept. `isCollapsed` gets folder keys (`JB/fix`); `expandAll` ignores it (search).
 */
export function branchTreeRows<T>(items: T[], name: (t: T) => string, isCollapsed: (key: string) => boolean, expandAll = false): BranchRow<T>[] {
  const root: Node<T> = { folders: new Map(), leaves: [], count: 0 };
  for (const item of items) {
    const parts = name(item).split("/");
    let n = root;
    n.count++;
    for (const p of parts.slice(0, -1)) {
      if (!n.folders.has(p)) n.folders.set(p, { folders: new Map(), leaves: [], count: 0 });
      n = n.folders.get(p)!;
      n.count++;
    }
    n.leaves.push({ label: parts[parts.length - 1], item });
  }
  const rows: BranchRow<T>[] = [];
  const walk = (n: Node<T>, prefix: string, depth: number) => {
    for (const [label, child] of n.folders) {
      const key = prefix + label;
      const open = expandAll || !isCollapsed(key);
      rows.push({ kind: "folder", key, label, depth, count: child.count, open });
      if (open) walk(child, key + "/", depth + 1);
    }
    for (const l of n.leaves) rows.push({ kind: "leaf", key: prefix + l.label, label: l.label, depth, item: l.item });
  };
  walk(root, "", 0);
  return rows;
}
