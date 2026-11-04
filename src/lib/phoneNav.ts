// The phone shell (src/components/phone/) is a render of the same routes the
// desktop uses plus a few of its own. Which screen a route shows, where its
// back arrow goes and whether the bottom nav is up are pure functions of the
// route name, so they live here and are tested without mounting anything.

export type PhoneScreen = "threads" | "project" | "more" | "project-surface" | "thread";

/** Routes the desktop has no use for; on a wide screen they redirect to `/`. */
const PHONE_ONLY = new Set(["project", "project-surface", "more", "new"]);

export function isPhoneOnlyRoute(name: unknown): boolean {
  return PHONE_ONLY.has(String(name));
}

export function phoneScreen(name: unknown): PhoneScreen {
  switch (name) {
    case "project": return "project";
    case "more": return "more";
    case "project-surface": return "project-surface";
    // `/new` is the composer, which lives in the terminal host like a thread.
    case "tab": case "workspace": case "new": return "thread";
    default: return "threads";
  }
}

const ROOTS = new Set<PhoneScreen>(["threads", "project", "more"]);

export function showsBottomNav(name: unknown): boolean {
  return name !== "dashboard" && ROOTS.has(phoneScreen(name));
}

/** Where a detail's back arrow lands when there is no in-app history (deep link). */
export function backTarget(name: unknown): string {
  return phoneScreen(name) === "project-surface" ? "/project" : "/";
}
