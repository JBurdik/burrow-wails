import { createRouter, createMemoryHistory, createWebHashHistory, type RouteRecordRaw } from "vue-router";

// The URL is the view state (fáze 4 of docs/plans/003-view-state-routes.md).
// Before this, "what is the user looking at" was a pair of refs in the ui store
// (`mode` + tri-state `welcomeOpen`) that every new piece of code had to
// remember to consult — and one that forgot is exactly how a tab behind the
// welcome composer came out as "watched".
//
// ponytail: there is deliberately no <router-view>. The shell in App.vue reads
// the route and shows the surface itself, because the terminal host must stay
// mounted across every route — a routed component would be unmounted on
// navigation, and re-attaching a PTY corrupts its scrollback (fáze 5). Routing
// here buys the single readable state, deep links and back/forward; it is not
// an excuse to restructure the DOM.
//
// Hash history: the app is loaded from the Wails asset scheme, where a path
// URL has no server to rewrite it back to index.html.

/** Routes render nothing themselves — App.vue owns the layout. */
const NONE = { render: () => null };

export const routes: RouteRecordRaw[] = [
  // The welcome composer ("What should we build in X?").
  { path: "/", name: "welcome", component: NONE },
  // A workspace's tabs. `tab` is the same surface with one tab selected, so
  // the two share everything except which tab the terminal host activates.
  { path: "/ws/:wsId(\\d+)", name: "workspace", component: NONE },
  { path: "/ws/:wsId(\\d+)/tab/:tabId(\\d+)", name: "tab", component: NONE },
  { path: "/dashboard", name: "dashboard", component: NONE },
  // Anything unrecognised is the composer, not a blank shell.
  { path: "/:pathMatch(.*)*", redirect: "/" },
];

export const router = createRouter({
  // Memory history outside a browser: the store tests import this module and
  // have no `location`, and a router that throws on import would make every one
  // of them fail for a reason unrelated to what they test.
  history: typeof window === "undefined" ? createMemoryHistory() : createWebHashHistory(),
  routes,
});

/** Numeric route param, or null when absent/malformed. */
export function routeId(value: unknown): number | null {
  const raw = Array.isArray(value) ? value[0] : value;
  const n = Number(raw);
  return Number.isFinite(n) && String(raw) !== "" ? n : null;
}

export default router;

/** Where a workspace's tabs live, or the composer when there is no workspace. */
export function workspaceRoute(activeWsId: number | null | undefined): string {
  return activeWsId == null ? "/" : `/ws/${activeWsId}`;
}

/**
 * Where "show me the tabs" should actually land. A workspace with nothing live
 * has no tabs to show, so the composer is the honest destination — this is the
 * auto branch of the old tri-state `welcomeOpen`, as a navigation decision
 * rather than a stored one.
 */
export function tabsOrWelcome(activeWsId: number | null | undefined, liveTabCount: number): string {
  return liveTabCount > 0 ? workspaceRoute(activeWsId) : "/";
}
