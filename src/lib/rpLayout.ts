// Right Panel layout: two stacked panes, each with its own surface tabs.
// Pure mutators over a plain object, so RightPanel can keep it in `reactive`
// and the rules are testable without a DOM. A surface lives in at most one
// pane — opening one that is already open just focuses it where it is.

export type PaneIndex = 0 | 1;
export interface Pane { tabs: string[]; active: string | null }
export interface RpLayout {
  panes: [Pane, Pane];
  /** Top pane's share of the height while both are visible. */
  split: number;
  maximized: PaneIndex | null;
  /** The bottom pane shows while it has tabs, or right after "Split" (picker). */
  bottomOpen: boolean;
}

export const RP_LAYOUT_KEY = "burrow.rpLayout";
const MIN_SPLIT = 0.15;
const MAX_SPLIT = 0.85;

export function emptyLayout(): RpLayout {
  return { panes: [{ tabs: [], active: null }, { tabs: [], active: null }], split: 0.5, maximized: null, bottomOpen: false };
}

export function clampSplit(v: number): number {
  return Number.isFinite(v) ? Math.min(MAX_SPLIT, Math.max(MIN_SPLIT, v)) : 0.5;
}

export function paneOf(l: RpLayout, id: string): PaneIndex | -1 {
  if (l.panes[0].tabs.includes(id)) return 0;
  if (l.panes[1].tabs.includes(id)) return 1;
  return -1;
}

export function isShown(l: RpLayout, id: string): boolean {
  return l.panes[0].active === id || (l.bottomOpen && l.panes[1].active === id);
}

function detach(l: RpLayout, id: string): void {
  const from = paneOf(l, id);
  if (from === -1) return;
  const pane = l.panes[from];
  pane.tabs = pane.tabs.filter((t) => t !== id);
  if (pane.active === id) pane.active = pane.tabs[0] ?? null;
  if (from === 1 && pane.tabs.length === 0) collapseBottom(l);
}

function collapseBottom(l: RpLayout): void {
  l.bottomOpen = false;
  l.panes[1].active = null;
  if (l.maximized !== null) l.maximized = null;
}

/** Open (or focus) a surface. A new one lands in `pane`. Returns where it is. */
export function openIn(l: RpLayout, id: string, pane: PaneIndex = 0): PaneIndex {
  const at = paneOf(l, id);
  const target = at === -1 ? pane : at;
  if (at === -1) l.panes[target].tabs.push(id);
  l.panes[target].active = id;
  if (target === 1) l.bottomOpen = true;
  return target;
}

export function closeSurface(l: RpLayout, id: string): void {
  detach(l, id);
}

export function moveTo(l: RpLayout, id: string, to: PaneIndex): void {
  if (paneOf(l, id) === to) return;
  detach(l, id);
  openIn(l, id, to);
}

/** "Split": show the bottom pane (empty → it shows the surface picker). */
export function openBottom(l: RpLayout): void {
  l.bottomOpen = true;
  l.maximized = null;
}

/** Closing the bottom pane never closes its surfaces — they go back on top. */
export function closeBottom(l: RpLayout): void {
  const moved = l.panes[1].tabs;
  l.panes[1].tabs = [];
  for (const id of moved) if (!l.panes[0].tabs.includes(id)) l.panes[0].tabs.push(id);
  if (!l.panes[0].active) l.panes[0].active = moved[0] ?? null;
  collapseBottom(l);
}

export function toggleMaximize(l: RpLayout, pane: PaneIndex): void {
  l.maximized = l.maximized === pane ? null : pane;
}

/** Rebuild a layout from untrusted storage: wrong shapes become empty, a
 *  surface listed in both panes keeps only its first place, ids are kept even
 *  when unknown (extension surfaces load after this runs). */
export function normalizeLayout(raw: unknown): RpLayout {
  const l = emptyLayout();
  if (!raw || typeof raw !== "object") return l;
  const r = raw as Partial<RpLayout>;
  const seen = new Set<string>();
  ([0, 1] as const).forEach((i) => {
    const p = Array.isArray(r.panes) ? r.panes[i] : undefined;
    const tabs = Array.isArray(p?.tabs) ? p!.tabs.filter((t): t is string => typeof t === "string" && !seen.has(t)) : [];
    tabs.forEach((t) => seen.add(t));
    l.panes[i] = { tabs: [...new Set(tabs)], active: typeof p?.active === "string" && tabs.includes(p.active) ? p.active : null };
  });
  l.split = clampSplit(typeof r.split === "number" ? r.split : 0.5);
  l.bottomOpen = r.bottomOpen === true || l.panes[1].tabs.length > 0;
  l.maximized = l.bottomOpen && (r.maximized === 0 || r.maximized === 1) ? r.maximized : null;
  return l;
}

/** Per-device, per-workspace; read-modify-write so one workspace's save never
 *  drops another's. */
export function loadLayouts(storage: Pick<Storage, "getItem"> | undefined = globalThis.localStorage): Record<number, RpLayout> {
  try {
    const raw = JSON.parse(storage?.getItem(RP_LAYOUT_KEY) ?? "{}");
    const out: Record<number, RpLayout> = {};
    for (const [k, v] of Object.entries(raw ?? {})) if (Number.isInteger(Number(k))) out[Number(k)] = normalizeLayout(v);
    return out;
  } catch {
    return {};
  }
}

export function saveLayout(wsId: number, l: RpLayout, storage: Pick<Storage, "getItem" | "setItem"> | undefined = globalThis.localStorage): void {
  let all: Record<string, unknown> = {};
  try { all = JSON.parse(storage?.getItem(RP_LAYOUT_KEY) ?? "{}") ?? {}; } catch { /* corrupt → start over */ }
  try {
    all[wsId] = l;
    storage?.setItem(RP_LAYOUT_KEY, JSON.stringify(all));
  } catch {
    // Storage full/blocked: the layout just doesn't survive a restart.
  }
}
