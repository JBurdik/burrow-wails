// Single source of truth for every rebindable app shortcut. The keybindings
// store (src/stores/keybindings.ts) layers user overrides from config.json on
// top; App.vue / Terminal.vue match keydowns against it; Settings and the ⌘/
// cheatsheet render straight from it, so a rebind shows up everywhere at once.
//
// ponytail: flat list, no per-scope registry object. Scope is a string field
// the two listeners filter on — that's all the routing this needs.

export type KeyScope = "app" | "terminal";

export interface KeyCommand {
  id: string;
  label: string;
  group: string;
  /** Default binding as a shortcut string ("⌘⇧O"), matched by lib/shortcuts.ts. */
  def: string;
  scope: KeyScope;
}

export const KEY_COMMANDS: KeyCommand[] = [
  // ── Global ──
  { id: "palette", label: "Command palette", group: "Global", def: "⌘P", scope: "app" },
  { id: "settings", label: "Settings", group: "Global", def: "⌘,", scope: "app" },
  { id: "cheatsheet", label: "Keyboard shortcuts cheatsheet", group: "Global", def: "⌘/", scope: "app" },
  { id: "sidebar", label: "Toggle sidebar", group: "Global", def: "⌘B", scope: "app" },
  { id: "manager", label: "Toggle Manager", group: "Global", def: "⌘⇧J", scope: "app" },
  { id: "unread", label: "Jump to first unread tab", group: "Global", def: "⌘⇧U", scope: "app" },

  // ── Projects ──
  { id: "newProject", label: "New project (browse / create folder)", group: "Projects", def: "⌘⇧N", scope: "app" },
  { id: "pickProject", label: "Pick project → new thread", group: "Projects", def: "⌘⇧O", scope: "app" },
  { id: "switchProvider", label: "Switch agent provider (Welcome)", group: "Projects", def: "⌘⇧A", scope: "app" },

  // ── Tabs & panes ──
  { id: "newTab", label: "New tab", group: "Tabs & panes", def: "⌘T", scope: "terminal" },
  { id: "closePane", label: "Close pane", group: "Tabs & panes", def: "⌘W", scope: "terminal" },
  { id: "splitH", label: "Split pane horizontally", group: "Tabs & panes", def: "⌘D", scope: "terminal" },
  { id: "splitV", label: "Split pane vertically", group: "Tabs & panes", def: "⌘⇧D", scope: "terminal" },
  { id: "bottomPanel", label: "Toggle bottom terminal panel", group: "Tabs & panes", def: "⌘J", scope: "terminal" },
  { id: "repaint", label: "Repaint terminals (un-scramble)", group: "Tabs & panes", def: "⌘⇧R", scope: "app" },
];

// Ranges and per-agent bindings can't be expressed as one shortcut string, so
// they stay hardcoded — listed here only so the cheatsheet stays honest.
export const FIXED_SHORTCUTS: { keys: string; desc: string; group: string }[] = [
  { keys: "⌘ 1-9", desc: "Switch project", group: "Projects" },
  { keys: "⌃ 1-9", desc: "Switch tab", group: "Tabs & panes" },
  { keys: "⌘ ⇧ 1-5", desc: "Launch agent (Settings → Agents)", group: "Agents" },
  { keys: "⇧ ↵", desc: "Insert newline (agent multiline input)", group: "Terminal" },
  { keys: "Esc", desc: "Close overlay", group: "Global" },
];
