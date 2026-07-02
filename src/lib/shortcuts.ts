// Match a KeyboardEvent against a human-written shortcut string like "⌘⇧1".
// Modifiers: ⌘ meta, ⌥ alt, ⌃ ctrl, ⇧ shift. The trailing char is the key.
// Digits are matched via e.code (Digit1…) so Shift/Option key remapping on
// macOS (Shift+1 → "!", Option+1 → "¡") doesn't break the binding.
const MODS = /[⌘⌥⌃⇧]/g;

// Build a shortcut string ("⌘⇧1") from a keydown event; null if only modifiers held.
export function eventToShortcut(e: KeyboardEvent): string | null {
  const k = e.key;
  if (["Meta", "Shift", "Alt", "Control"].includes(k)) return null;
  let s = "";
  if (e.metaKey) s += "⌘";
  if (e.altKey) s += "⌥";
  if (e.ctrlKey) s += "⌃";
  if (e.shiftKey) s += "⇧";
  // Digits via code so Shift/Option remapping (Shift+1 → "!") doesn't leak.
  if (/^Digit[0-9]$/.test(e.code)) s += e.code.slice(5);
  else if (k.length === 1) s += k.toUpperCase();
  else s += k; // named keys (Enter, ArrowUp, …)
  return s;
}

export function matchesShortcut(e: KeyboardEvent, sc: string | undefined): boolean {
  if (!sc) return false;
  const key = sc.replace(MODS, "").trim();
  if (!key) return false;
  if (e.metaKey !== sc.includes("⌘")) return false;
  if (e.altKey !== sc.includes("⌥")) return false;
  if (e.ctrlKey !== sc.includes("⌃")) return false;
  if (e.shiftKey !== sc.includes("⇧")) return false;
  if (/^[0-9]$/.test(key)) return e.code === `Digit${key}`;
  return e.key.toLowerCase() === key.toLowerCase();
}
