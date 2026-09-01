import { describe, it, expect, beforeEach } from "vitest";
import { createPinia, setActivePinia } from "pinia";
import { useKeybindingsStore } from "./keybindings";
import { KEY_COMMANDS } from "../lib/keymap";

// One event factory — matchesShortcut only reads these five fields.
function ev(key: string, mods: { meta?: boolean; shift?: boolean; alt?: boolean; ctrl?: boolean } = {}) {
  return {
    key,
    code: /^[0-9]$/.test(key) ? `Digit${key}` : `Key${key.toUpperCase()}`,
    metaKey: !!mods.meta,
    shiftKey: !!mods.shift,
    altKey: !!mods.alt,
    ctrlKey: !!mods.ctrl,
  } as KeyboardEvent;
}

describe("keybindings store", () => {
  beforeEach(() => setActivePinia(createPinia()));

  it("falls back to the registry default", () => {
    const keys = useKeybindingsStore();
    expect(keys.shortcut("palette")).toBe("⌘P");
    expect(keys.matches(ev("p", { meta: true }), "palette")).toBe(true);
  });

  it("an override wins over the default", () => {
    const keys = useKeybindingsStore();
    keys.set("palette", "⌘⇧K");
    expect(keys.matches(ev("p", { meta: true }), "palette")).toBe(false);
    expect(keys.matches(ev("k", { meta: true, shift: true }), "palette")).toBe(true);
  });

  it("rebinding onto a taken combo releases the previous owner", () => {
    const keys = useKeybindingsStore();
    keys.set("manager", "⌘P"); // steals the palette's combo
    expect(keys.shortcut("palette")).toBe("");
    expect(keys.matches(ev("p", { meta: true }), "palette")).toBe(false);
    expect(keys.matches(ev("p", { meta: true }), "manager")).toBe(true);
  });

  it("clearing unbinds without matching anything", () => {
    const keys = useKeybindingsStore();
    keys.set("settings", "");
    expect(keys.matches(ev(",", { meta: true }), "settings")).toBe(false);
  });

  it("reset restores the default", () => {
    const keys = useKeybindingsStore();
    keys.set("sidebar", "⌘⌥B");
    keys.reset("sidebar");
    expect(keys.shortcut("sidebar")).toBe("⌘B");
  });

  it("ships no duplicate default bindings", () => {
    const seen = new Set<string>();
    for (const c of KEY_COMMANDS) {
      expect(seen.has(c.def), `${c.id} duplicates ${c.def}`).toBe(false);
      seen.add(c.def);
    }
  });
});
