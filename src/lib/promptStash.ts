// Prompt stash — park the composer's content and bring it back later, ported in
// principle from t3code's promptStashStore. Global (not per workspace), so a
// prompt stashed in one chat can be restored in another or on the welcome screen.
import { ref } from "vue";

export const STASH_STORAGE_KEY = "burrow.promptStash";
export const MAX_STASH_ENTRIES = 20;
// localStorage's ~5 MB quota is shared with drafts and config; images past this
// budget are dropped and the entry records which ones.
export const MAX_STASH_IMAGE_CHARS = 2_700_000;

export interface StashEntry {
  id: string;
  createdAt: number;
  text: string;
  /** Data URIs, like the composers' pendingImages. */
  images: string[];
  /** Labels of the images that did not fit the budget. */
  droppedImages: string[];
}

export interface ComposerContent {
  text: string;
  images: string[];
}

export function isEmptyContent(c: ComposerContent): boolean {
  return !c.text.trim() && c.images.length === 0;
}

export function makeEntry(c: ComposerContent, now = Date.now()): StashEntry {
  const images: string[] = [];
  const droppedImages: string[] = [];
  let used = 0;
  c.images.forEach((uri, i) => {
    if (used + uri.length > MAX_STASH_IMAGE_CHARS) { droppedImages.push(`image ${i + 1}`); return; }
    used += uri.length;
    images.push(uri);
  });
  return { id: `${now}-${Math.random().toString(36).slice(2, 8)}`, createdAt: now, text: c.text, images, droppedImages };
}

/** Newest first; past the cap the oldest falls off. */
export function pushEntry(entries: readonly StashEntry[], entry: StashEntry): StashEntry[] {
  return [entry, ...entries].slice(0, MAX_STASH_ENTRIES);
}

/** Take `id` out of the stash; a non-empty composer is stashed first so restoring never loses it. */
export function restoreEntry(
  entries: readonly StashEntry[],
  id: string,
  current: ComposerContent,
  now = Date.now(),
): { entries: StashEntry[]; restored: StashEntry | null } {
  const restored = entries.find((e) => e.id === id) ?? null;
  if (!restored) return { entries: [...entries], restored: null };
  const rest = entries.filter((e) => e.id !== id);
  return { entries: isEmptyContent(current) ? rest : pushEntry(rest, makeEntry(current, now)), restored };
}

function load(): StashEntry[] {
  try {
    const parsed = JSON.parse(localStorage.getItem(STASH_STORAGE_KEY) ?? "[]");
    return Array.isArray(parsed) ? parsed : [];
  } catch {
    return [];
  }
}

/** Shared by every composer. */
export const stashEntries = ref<StashEntry[]>(load());

/** False when the write failed (quota) — the caller keeps the composer intact. */
export function commitStash(next: StashEntry[]): boolean {
  try {
    localStorage.setItem(STASH_STORAGE_KEY, JSON.stringify(next));
  } catch (err) {
    console.error("[prompt-stash] could not persist", err);
    return false;
  }
  stashEntries.value = next;
  return true;
}

export function isStashShortcut(e: KeyboardEvent): boolean {
  return (e.metaKey || e.ctrlKey) && e.shiftKey && !e.altKey && e.key.toLowerCase() === "s";
}
