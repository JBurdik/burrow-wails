import { SHELL_RE } from "@/lib/termRegistry";

// The Right Panel's Terminal surface holds several shells per workspace: the
// user's scratch shells and the long-lived processes agents start through the
// `serve` control verb. Pure helpers over a plain object (RightPanel keeps it
// reactive), plus the door the control bridge uses to reach the mounted panel.

export interface RpTerm {
  id: number;
  cwd: string;
  label: string;
  /** Set for a `serve` terminal: what it runs, and what dedup matches on. */
  cmd?: string;
}
export interface RpTerms { terms: RpTerm[]; active: number | null }

export function addTerm(s: RpTerms, t: RpTerm): void {
  s.terms.push(t);
  s.active = t.id;
}

export function removeTerm(s: RpTerms, id: number): void {
  const i = s.terms.findIndex((t) => t.id === id);
  if (i === -1) return;
  s.terms.splice(i, 1);
  if (s.active === id) s.active = (s.terms[i] ?? s.terms[i - 1])?.id ?? null;
}

/** The terminal already serving `cmd` in `cwd` — so an agent that asks for
 *  `pnpm dev` twice gets the running one, not a second server on a new port. */
export function findServed(s: RpTerms | undefined, cmd: string, cwd: string): RpTerm | undefined {
  const norm = (c: string) => c.trim().replace(/\s+/g, " ");
  return s?.terms.find((t) => t.cmd !== undefined && norm(t.cmd) === norm(cmd) && t.cwd === cwd);
}

/** True when a PTY's foreground (get_pty_foreground) is just its shell — the
 *  served process was stopped. "" is NOT that: the daemon has no such PTY
 *  (its shell exited), so there is nothing to type into. */
export function onlyShell(foreground: string): boolean {
  return SHELL_RE.test(foreground);
}

export interface ServeRequest { workspaceId: number; cmd: string; cwd: string; name: string }
export interface ServeResult { pty_id: number; workspace_id: number; reused: boolean; restarted?: boolean }
export interface PanelTerminals {
  serve(req: ServeRequest): Promise<ServeResult>;
  /** Close a panel terminal by pty id; false when no panel terminal has it. */
  close(ptyId: number): boolean;
}

// One RightPanel instance exists app-wide; it installs itself here on mount.
let panel: PanelTerminals | null = null;
export function installPanelTerminals(p: PanelTerminals | null): void {
  panel = p;
}
export function panelTerminals(): PanelTerminals | null {
  return panel;
}

// ── Persistence ──────────────────────────────────────────────────────────────
// The terminal list outlives the RightPanel instance that drew it: the panel
// moves into its own window and back (each mount reads this), and the shells
// survive an app restart in the daemon. Without it a remount lost the list and
// orphaned every running shell in the daemon.
export const RP_TERMS_KEY = "burrow.rpTerms";
type Store = Pick<Storage, "getItem" | "setItem">;

export function loadTerms(storage: Store | undefined = globalThis.localStorage): Record<number, RpTerms> {
  try {
    const raw = JSON.parse(storage?.getItem(RP_TERMS_KEY) ?? "{}") as Record<string, RpTerms>;
    const out: Record<number, RpTerms> = {};
    for (const [ws, s] of Object.entries(raw)) {
      const terms = Array.isArray(s?.terms) ? s.terms.filter((t) => typeof t?.id === "number" && typeof t.cwd === "string") : [];
      if (terms.length) out[Number(ws)] = { terms, active: terms.some((t) => t.id === s.active) ? s.active : terms[0].id };
    }
    return out;
  } catch {
    return {};
  }
}

export function saveTerms(map: Record<number, RpTerms>, storage: Store | undefined = globalThis.localStorage): void {
  try {
    storage?.setItem(RP_TERMS_KEY, JSON.stringify(map));
  } catch { /* quota: the list just won't survive a remount */ }
}

/** Only the shells the daemon still has: reattaching to a dead id would start
 *  an unrelated fresh shell under an old label. */
export function keepLive(map: Record<number, RpTerms>, live: Set<number>): Record<number, RpTerms> {
  const out: Record<number, RpTerms> = {};
  for (const [ws, s] of Object.entries(map)) {
    const terms = s.terms.filter((t) => live.has(t.id));
    if (terms.length) out[Number(ws)] = { terms, active: terms.some((t) => t.id === s.active) ? s.active : terms[0].id };
  }
  return out;
}
