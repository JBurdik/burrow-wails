import type { AgentAttentionState } from "@/lib/terminalStatus";

// The phone shell (src/components/phone/) is a render of the same routes the
// desktop uses plus a few of its own. Which screen a route shows, where its
// back arrow goes and whether the bottom nav is up are pure functions of the
// route name, so they live here and are tested without mounting anything.

export type PhoneScreen = "threads" | "inbox" | "project" | "more" | "project-surface" | "thread";

/** Routes the desktop has no use for; on a wide screen they redirect to `/`. */
const PHONE_ONLY = new Set(["inbox", "project", "project-surface", "more", "new"]);

export function isPhoneOnlyRoute(name: unknown): boolean {
  return PHONE_ONLY.has(String(name));
}

export function phoneScreen(name: unknown): PhoneScreen {
  switch (name) {
    case "inbox": return "inbox";
    case "project": return "project";
    case "more": return "more";
    case "project-surface": return "project-surface";
    // `/new` is the composer, which lives in the terminal host like a thread.
    case "tab": case "workspace": case "new": return "thread";
    default: return "threads";
  }
}

const ROOTS = new Set<PhoneScreen>(["threads", "inbox", "project", "more"]);

export function showsBottomNav(name: unknown): boolean {
  return name !== "dashboard" && ROOTS.has(phoneScreen(name));
}

/** Where a detail's back arrow lands when there is no in-app history (deep link). */
export function backTarget(name: unknown): string {
  return phoneScreen(name) === "project-surface" ? "/project" : "/";
}

export interface InboxRow {
  wsId: number;
  tabId: number;
  title: string;
  state: AgentAttentionState;
  /** last activity, ms epoch */
  ts: number;
  wsName?: string;
  agentIcon?: string;
}

export interface InboxSection {
  key: "needs" | "done" | "running";
  label: string;
  rows: InboxRow[];
}

/** Needs you (input, permission, error) → done unread → running; idle and empty sections dropped. */
export function inboxSections(rows: InboxRow[]): InboxSection[] {
  const sections: InboxSection[] = [
    { key: "needs", label: "Needs you", rows: [] },
    { key: "done", label: "Done, unread", rows: [] },
    { key: "running", label: "Running", rows: [] },
  ];
  for (const r of rows) {
    if (r.state === "needs-input" || r.state === "error") sections[0].rows.push(r);
    else if (r.state === "done-unread") sections[1].rows.push(r);
    else if (r.state === "working") sections[2].rows.push(r);
  }
  for (const s of sections) s.rows.sort((a, b) => b.ts - a.ts || b.tabId - a.tabId);
  return sections.filter((s) => s.rows.length);
}
