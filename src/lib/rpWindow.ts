// The Right Panel in its own window ("Open in window" in its header).
//
// The main window unmounts its RightPanel and the window mounts one; the two
// never run at once. What crosses between them:
//  - the terminal list — through localStorage (rpTerminals.ts saveTerms/loadTerms);
//  - what the panel follows (workspace, chat) — main → window, rpFollow below;
//  - the `serve` / close-terminal control verbs, which the main window's control
//    bridge receives but only the window's panel can run — Wails JS events.
import { Events } from "@wailsio/runtime";
import { loadTerms, panelTerminals, type PanelTerminals, type ServeRequest, type ServeResult } from "@/lib/rpTerminals";

export const RP_WINDOW_KEY = "rightpanel";

// ── What the popped panel shows ───────────────────────────────────────────────
export const RP_FOLLOW_KEY = "burrow.rpFollow";
export interface RpFollow { workspaceId?: number; cwd: string; isGit: boolean; chatId: number | null }

export function publishFollow(f: RpFollow): void {
  const v = JSON.stringify(f);
  if (localStorage.getItem(RP_FOLLOW_KEY) !== v) localStorage.setItem(RP_FOLLOW_KEY, v);
}
export function readFollow(): RpFollow {
  try {
    return { cwd: "", isGit: true, chatId: null, ...(JSON.parse(localStorage.getItem(RP_FOLLOW_KEY) ?? "{}") as Partial<RpFollow>) };
  } catch {
    return { cwd: "", isGit: true, chatId: null };
  }
}

// ── Control verbs, main → window ──────────────────────────────────────────────
const OP = "rp:op";
const REPLY = "rp:reply";
const TIMEOUT_MS = 15_000;
interface Op { id: number; op: "serve" | "close"; arg: unknown }
interface Reply { id: number; result?: unknown; error?: string }

let seq = 0;
const waiting = new Map<number, { resolve: (v: unknown) => void; reject: (e: Error) => void }>();
let replyOff: (() => void) | null = null;

function call(op: Op["op"], arg: unknown): Promise<unknown> {
  replyOff ??= Events.On(REPLY, (e) => {
    const r = e.data as Reply;
    const w = waiting.get(r.id);
    if (!w) return;
    waiting.delete(r.id);
    if (r.error) w.reject(new Error(r.error));
    else w.resolve(r.result);
  });
  const id = ++seq;
  return new Promise((resolve, reject) => {
    waiting.set(id, { resolve, reject });
    setTimeout(() => {
      if (waiting.delete(id)) reject(new Error("the right panel window did not answer"));
    }, TIMEOUT_MS);
    void Events.Emit(OP, { id, op, arg } satisfies Op);
  });
}

/** Main window, while the panel is popped out: the control bridge's panel. */
export const remotePanel: PanelTerminals = {
  serve: (req: ServeRequest) => call("serve", req) as Promise<ServeResult>,
  // Synchronous by contract: answered from the shared list, the kill itself
  // goes to the window.
  close(ptyId: number) {
    const known = Object.values(loadTerms()).some((s) => s.terms.some((t) => t.id === ptyId));
    if (known) void call("close", ptyId).catch(() => {});
    return known;
  },
};

/** The panel's window: run the main window's forwarded verbs on its panel. */
export function answerPanelOps(): () => void {
  return Events.On(OP, async (e) => {
    const { id, op, arg } = e.data as Op;
    const panel = panelTerminals();
    try {
      if (!panel) throw new Error("the right panel is not mounted");
      const result = op === "serve" ? await panel.serve(arg as ServeRequest) : panel.close(arg as number);
      void Events.Emit(REPLY, { id, result } satisfies Reply);
    } catch (err) {
      void Events.Emit(REPLY, { id, error: String(err) } satisfies Reply);
    }
  });
}
