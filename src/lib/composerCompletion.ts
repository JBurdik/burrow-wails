// The composer's `@file` / `$skill` / `/command` completion, shared by the
// welcome composer and the chat composer.
//
// It used to be copy-pasted into both: the same regexes, the same git ls-files
// call, the same relevance sort, the same "replace the token before the cursor"
// splice — and two *separate* suggestion lists per component (one for `@`, one
// for `$`), rendered as two sibling dropdowns even though the regexes make them
// mutually exclusive. One list with one trigger is the same behaviour with a
// quarter of the state.
//
// A picked skill goes INLINE, spliced into the text as `/name` wherever the `$`
// was typed. ComposerTextInput draws those tokens as atomic inline chips, so
// the model this file edits stays a plain string and the prompt the agent gets
// already reads `fix /agent-browser now` — no prefixing, no side list.
import { nextTick, ref, type Ref } from "vue";
import { invoke } from "@tauri-apps/api/core";

/** Where a skill came from — drives the row's badge. */
export type SkillSource = "personal" | "project";

export interface ComposerSkill {
  /** Invocation name, e.g. `agent-browser` (sent as `/agent-browser`). */
  name: string;
  /** Display name, e.g. `Agent Browser`. */
  label: string;
  description: string;
  source: SkillSource;
}

export interface ComposerCommand { name: string; description: string }

/** Right-hand tag on a suggestion row. */
export type SuggestionBadge = SkillSource | "command";

/** One row in the suggestion panel. */
export interface ComposerSuggestion {
  /** v-for key. */
  key: string;
  /** Primary column — `Agent Browser`, `@App.vue`, `/compact`. */
  label: string;
  /** Secondary column — the skill/command description, or the full path. */
  hint: string;
  badge?: SuggestionBadge;
  /** Text spliced in at the cursor, replacing the trigger token. */
  insert: string;
}

// `@` completes repo paths; `/` completes built-in commands; `$` completes
// installed skills. All three require start-of-input or whitespace before the
// trigger, so completion works mid-message, not just at the start.
const AT_RE = /(?:^|\s)@([^\s@]*)$/;
const CMD_RE = /(?:^|\s)([/$])([^\s/$]*)$/;

/** `app-store-preflight` → `App Store Preflight`, the way the picker lists it. */
export function skillLabel(name: string): string {
  return name
    .split(/[-_]+/)
    .filter(Boolean)
    .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
    .join(" ");
}

interface RawSkill { dir: string; name: string; description: string; source: SkillSource; enabled: boolean }

// Keyed by cwd: the personal skills are the same everywhere, but the project
// ones are not, and one fetch per repo is cheap enough not to split the call.
const skillCache = new Map<string, Promise<ComposerSkill[]>>();
function loadSkills(cwd: string): Promise<ComposerSkill[]> {
  let hit = skillCache.get(cwd);
  if (!hit) {
    hit = invoke<RawSkill[]>("list_skills", { cwd })
      .then((skills) => {
        // A repo's own skill shadows a personal one of the same name — the
        // agent resolves it that way, so the picker has to agree. Personal
        // rows come first from Go, so a later project row overwrites.
        const merged = new Map<string, ComposerSkill>();
        for (const s of skills) {
          if (!s.enabled) continue;
          merged.set(s.name, {
            name: s.name,
            label: skillLabel(s.name),
            description: s.description,
            source: s.source === "project" ? "project" : "personal",
          });
        }
        return [...merged.values()].sort((a, b) => a.label.localeCompare(b.label));
      })
      .catch(() => []); // browser-only dev without a backend
    skillCache.set(cwd, hit);
  }
  return hit;
}

// ponytail: cached per cwd and never invalidated, so a file created after the
// first `@` in that repo won't complete until reload — same as before this was
// shared. Upgrade path: drop the entry on a file-tree change event.
const fileLists = new Map<string, string[]>();
async function loadFiles(cwd: string): Promise<string[]> {
  const hit = fileLists.get(cwd);
  if (hit) return hit;
  try {
    const out = await invoke<{ stdout: string }>("run_git", {
      cwd,
      args: ["ls-files", "--cached", "--others", "--exclude-standard"],
    });
    const list = out.stdout.split("\n").map((s) => s.trim()).filter(Boolean).slice(0, 20000);
    fileLists.set(cwd, list);
    return list;
  } catch {
    return [];
  }
}

/**
 * The caret half of the composer's input, as ComposerTextInput exposes it.
 * Offsets are into the plain-text model, not the DOM — a chip counts as the
 * `/name` characters it stands for.
 */
export interface ComposerInputHandle {
  focus(): void;
  caret(): number;
  setCaret(offset: number): void;
}

export interface ComposerCompletionOptions {
  /** The composer's text, as the host's v-model ref. */
  text: Ref<string>;
  /** The live input — needed for the caret position and for refocusing. */
  input: () => ComposerInputHandle | null | undefined;
  /** Repo whose files `@` completes and whose project skills `$` lists. */
  cwd: () => string;
  /** `/` command list. Omit on surfaces with no session (nothing to command). */
  commands?: Ref<ComposerCommand[]>;
  /** Ran after an insert, for whatever the host wants to re-measure. */
  onApplied?: () => void;
}

export function useComposerCompletion(opts: ComposerCompletionOptions) {
  const skills = ref<ComposerSkill[]>([]);
  // Re-read when the composer changes repo: the project half of the list is
  // per-repo, and the welcome composer's target is a dropdown away.
  const loadedFor = ref<string | null>(null);
  async function ensureSkills() {
    const cwd = opts.cwd();
    if (loadedFor.value === cwd) return;
    loadedFor.value = cwd;
    const list = await loadSkills(cwd);
    if (loadedFor.value === cwd) skills.value = list;
  }
  void ensureSkills();

  const suggestions = ref<ComposerSuggestion[]>([]);
  const activeIndex = ref(0);

  function caret(): number {
    return opts.input()?.caret() ?? opts.text.value.length;
  }

  /** The completion token immediately before the cursor, if any. */
  function triggerAtCursor(): { kind: "@" | "/" | "$"; q: string } | null {
    const upto = opts.text.value.slice(0, caret());
    const at = upto.match(AT_RE);
    if (at) return { kind: "@", q: at[1] };
    const cmd = upto.match(CMD_RE);
    if (cmd) return { kind: cmd[1] as "/" | "$", q: cmd[2] };
    return null;
  }

  function close() { suggestions.value = []; }

  async function update() {
    const t = triggerAtCursor();
    if (!t) { close(); return; }
    const q = t.q.toLowerCase();

    if (t.kind === "@") {
      const files = await loadFiles(opts.cwd());
      // The cursor may have moved while git was running — a stale list landing
      // on top of a different query is worse than no list.
      const now = triggerAtCursor();
      if (now?.kind !== "@" || now.q !== t.q) return;
      suggestions.value = files
        .filter((p) => p.toLowerCase().includes(q))
        .sort((a, b) => {
          const ab = a.slice(a.lastIndexOf("/") + 1).toLowerCase();
          const bb = b.slice(b.lastIndexOf("/") + 1).toLowerCase();
          // Basename prefix matches first, then shortest path.
          return (Number(!ab.startsWith(q)) - Number(!bb.startsWith(q))) || a.length - b.length;
        })
        .slice(0, 8)
        .map((p) => ({
          key: p,
          label: `@${p.slice(p.lastIndexOf("/") + 1)}`,
          hint: p,
          insert: `@${p}`,
        }));
      activeIndex.value = 0;
      return;
    }

    if (t.kind === "$") {
      await ensureSkills();
      const now = triggerAtCursor();
      if (now?.kind !== "$" || now.q !== t.q) return;
      suggestions.value = skills.value
        .filter((s) => s.name.toLowerCase().includes(q) || s.label.toLowerCase().includes(q))
        .sort((a, b) => Number(!a.name.toLowerCase().startsWith(q)) - Number(!b.name.toLowerCase().startsWith(q)))
        .slice(0, 8)
        .map((s) => ({
          key: s.name,
          label: s.label,
          hint: s.description || `/${s.name}`,
          badge: s.source,
          // `$` is only the menu trigger — `/name` is what the agent understands.
          insert: `/${s.name}`,
        }));
      activeIndex.value = 0;
      return;
    }

    suggestions.value = (opts.commands?.value ?? [])
      .filter((c) => c.name.toLowerCase().startsWith(q))
      .slice(0, 8)
      .map((c) => ({
        key: c.name,
        label: `/${c.name}`,
        hint: c.description,
        badge: "command" as const,
        insert: `/${c.name}`,
      }));
    activeIndex.value = 0;
  }

  /** Swap the trigger token before the cursor for `insert` plus a space. */
  function apply(s: ComposerSuggestion) {
    const insert = s.insert;
    const t = triggerAtCursor();
    if (!t) return;
    const pos = caret();
    const upto = opts.text.value.slice(0, pos);
    const after = opts.text.value.slice(pos);
    const base = upto.slice(0, upto.length - (t.q.length + 1)); // +1 = the trigger char
    const sep = after.startsWith(" ") ? "" : " ";
    opts.text.value = `${base}${insert}${sep}${after}`;
    close();
    // The input rebuilds its DOM for the new chip on the model watcher, so the
    // caret has to be parked after that lands, not before.
    nextTick(() => {
      const el = opts.input();
      opts.onApplied?.();
      if (!el) return;
      el.focus();
      el.setCaret(base.length + insert.length + sep.length);
    });
  }

  /**
   * Suggestion-panel navigation. Returns true when the key was consumed — the
   * host must check that BEFORE its own Enter/Escape handling, or Enter picks
   * a suggestion and sends the message in the same keystroke.
   */
  function handleKeydown(e: KeyboardEvent): boolean {
    if (suggestions.value.length === 0) return false;
    if (e.key === "ArrowDown") {
      e.preventDefault();
      activeIndex.value = Math.min(activeIndex.value + 1, suggestions.value.length - 1);
      return true;
    }
    if (e.key === "ArrowUp") {
      e.preventDefault();
      activeIndex.value = Math.max(activeIndex.value - 1, 0);
      return true;
    }
    if (e.key === "Tab" || (e.key === "Enter" && !e.shiftKey)) {
      e.preventDefault();
      apply(suggestions.value[activeIndex.value]);
      return true;
    }
    if (e.key === "Escape") { close(); return true; }
    return false;
  }

  return {
    suggestions, activeIndex, skills,
    update, apply, close, handleKeydown,
  };
}
