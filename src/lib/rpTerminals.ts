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
 *  served process was stopped. "" covers a daemon that reports no foreground. */
export function onlyShell(foreground: string): boolean {
  return foreground === "" || /^-?(zsh|bash|sh|fish|csh|tcsh|dash)$/.test(foreground);
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
