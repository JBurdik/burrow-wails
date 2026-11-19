// Activity log + hang watch (Go: src-wails/activity.go, verb `activity`).
// Opt-in (uiPrefs.activityLog): the main window notes clicks (by the clicked
// control's aria-label / title / label + the surface it is in), route changes,
// shortcuts (keymap id), message sends and request answers — never typed text
// or message content. Hangs (UI stalls ≥ 1 s) are noted always: they carry no
// user content. Batched here, flushed every 5 s through `activity_append`.
import type { Router } from "vue-router";

export interface ActivityEntry {
  at?: number;
  kind: "click" | "route" | "shortcut" | "send" | "answer" | "hang";
  label?: string;
  surface?: string;
  route?: string;
  ms?: number;
  images?: number;
  mode?: string;
  last?: string;
}

interface Deps {
  invoke: (cmd: string, args: Record<string, unknown>) => Promise<unknown>;
  enabled: () => boolean;
  route: () => string;
  now?: () => number;
}

const HANG_MIN_MS = 1000;
const HANG_GAP_MS = 10_000;

export function createActivityLog(deps: Deps) {
  const now = deps.now ?? Date.now;
  let queue: ActivityEntry[] = [];
  let last: ActivityEntry | null = null;
  let lastHangAt = -Infinity;
  return {
    note(e: ActivityEntry) {
      if (!deps.enabled()) return;
      last = { ...e, at: now() };
      queue.push(last);
    },
    hang(ms: number) {
      const t = now();
      if (ms < HANG_MIN_MS || t - lastHangAt < HANG_GAP_MS) return;
      lastHangAt = t;
      const prev = deps.enabled() && last ? [last.kind, last.label, last.surface].filter(Boolean).join(" ") : undefined;
      queue.push({ at: t, kind: "hang", ms: Math.round(ms), route: deps.route(), last: prev });
    },
    flush() {
      if (!queue.length) return;
      const entries = queue;
      queue = [];
      deps.invoke("activity_append", { entries }).catch(() => {});
    },
  };
}

let active: ReturnType<typeof createActivityLog> | null = null;

/** Notes one entry while the log is on; a no-op before `startActivityLog`. */
export function noteActivity(e: ActivityEntry) {
  active?.note(e);
}

const SURFACES: [string, string][] = [
  [".settings-root", "settings"],
  [".s-modal", "palette"],
  ["[role=dialog]", "dialog"],
  ["[role=menu]", "menu"],
  [".claude-chat", "chat"],
  [".terminal-pane", "terminal"],
  ["aside.bg-panel", "right panel"],
  ["aside", "sidebar"],
];

function surfaceOf(el: Element): string | undefined {
  return SURFACES.find(([sel]) => el.closest(sel))?.[1];
}

function labelOf(el: Element): string | undefined {
  const raw = el.getAttribute("aria-label") || el.getAttribute("title") || (el as HTMLElement).innerText || "";
  const label = raw.replace(/\s+/g, " ").trim().slice(0, 80);
  return label || undefined;
}

const CLICKABLE = "button, a, [role=button], [role=menuitem], [role=tab], [role=option], [role=switch], summary, label";

/** Installs the listeners on the main window. Returns a stop function. */
export function startActivityLog(router: Router, deps: Omit<Deps, "route">): () => void {
  const log = createActivityLog({ ...deps, route: () => router.currentRoute.value.fullPath });
  active = log;
  const onClick = (ev: MouseEvent) => {
    const el = (ev.target as Element | null)?.closest?.(CLICKABLE);
    // ponytail: only controls are noted; text fields and blank space never are.
    if (!el) return;
    log.note({ kind: "click", label: labelOf(el), surface: surfaceOf(el) });
  };
  document.addEventListener("click", onClick, true);
  const offRoute = router.afterEach((to) => log.note({ kind: "route", route: to.fullPath }));
  const flushTimer = setInterval(() => log.flush(), 5000);

  // Hang watch: longtask where the engine has it (not WKWebView), else a
  // timer that measures how late it fires. A hidden window's timers are
  // throttled, and a gap over 5 min is a sleep, not a hang.
  let observer: PerformanceObserver | null = null;
  let stallTimer: ReturnType<typeof setInterval> | undefined;
  if (PerformanceObserver.supportedEntryTypes?.includes("longtask")) {
    observer = new PerformanceObserver((list) => list.getEntries().forEach((e) => log.hang(e.duration)));
    observer.observe({ type: "longtask" });
  } else {
    const TICK = 250;
    let prev = performance.now();
    let wasVisible = true;
    stallTimer = setInterval(() => {
      const t = performance.now();
      const late = t - prev - TICK;
      const visible = document.visibilityState === "visible";
      prev = t;
      if (visible && wasVisible && late < 300_000) log.hang(late);
      wasVisible = visible;
    }, TICK);
  }
  return () => {
    log.flush();
    document.removeEventListener("click", onClick, true);
    offRoute();
    clearInterval(flushTimer);
    clearInterval(stallTimer);
    observer?.disconnect();
    if (active === log) active = null;
  };
}
