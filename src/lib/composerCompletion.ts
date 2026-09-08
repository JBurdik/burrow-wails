// The composer's `@file` / `$skill` / `/command` completion, shared by the
// welcome composer and the chat composer.
//
// It used to be copy-pasted into both: the same regexes, the same git ls-files
// call, the same relevance sort, the same "replace the token before the cursor"
// splice — and two *separate* suggestion lists per component (one for `@`, one
// for `$`), rendered as two sibling dropdowns even though the regexes make them
// mutually exclusive. One list with one trigger is the same behaviour with a
// quarter of the state.
import { computed, nextTick, ref, type Ref } from "vue";
import { invoke } from "@tauri-apps/api/core";
import { splitSkillTokens } from "@/lib/skillTokens";

export interface ComposerCommand { name: string; description: string }

/** One row in the suggestion dropdown. */
export interface ComposerSuggestion {
  /** v-for key. */
  key: string;
  /** Left column — the token as it reads once inserted. */
  label: string;
  /** Right column — full path (`@`) or the command/skill description. */
  hint: string;
  /** What replaces the trigger token in the textarea. */
  insert: string;
}

// `@` completes repo paths; `/` completes built-in commands; `$` completes
// installed skills and inserts `/name` — the invocation the agent understands,
// `$` is only the menu trigger. All three require start-of-input or whitespace
// before the trigger, so completion works mid-message, not just at the start.
const AT_RE = /(?:^|\s)@([^\s@]*)$/;
const CMD_RE = /(?:^|\s)([/$])([^\s/$]*)$/;

// Skills are installed globally, so one fetch serves every composer in the app
// (a workspace with several chats open would otherwise call list_skills per
// mount). Shared cache, resolved once.
let skillsPromise: Promise<ComposerCommand[]> | null = null;
function loadSkills(): Promise<ComposerCommand[]> {
  skillsPromise ??= invoke<{ name: string; description: string; enabled: boolean }[]>("list_skills")
    .then((skills) => {
      // Map-based dedup: list_skills can report the same name from two roots.
      const merged = new Map<string, ComposerCommand>();
      for (const s of skills) {
        if (s.enabled) merged.set(s.name, { name: s.name, description: s.description || `/${s.name} skill` });
      }
      return [...merged.values()].sort((a, b) => a.name.localeCompare(b.name));
    })
    .catch(() => []); // browser-only dev without a backend
  return skillsPromise;
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

export interface ComposerCompletionOptions {
  /** The composer's text, as the host's v-model ref. */
  text: Ref<string>;
  /** The live textarea — needed for the caret position and for refocusing. */
  element: () => HTMLTextAreaElement | null | undefined;
  /** Repo whose files `@` completes. Read per call, so it may change. */
  cwd: () => string;
  /** `/` command list. Omit on surfaces with no session (nothing to command). */
  commands?: Ref<ComposerCommand[]>;
  /** Ran after an insert — the chat composer re-runs its autoResize here. */
  onApplied?: () => void;
}

export function useComposerCompletion(opts: ComposerCompletionOptions) {
  const skills = ref<ComposerCommand[]>([]);
  void loadSkills().then((list) => { skills.value = list; });

  const suggestions = ref<ComposerSuggestion[]>([]);
  const activeIndex = ref(0);

  // Skill pills: a backdrop div re-renders the input's text with `/skill`
  // tokens wrapped in .skill-pill, and the textarea's own text goes
  // transparent while one is present (see styles/composer.css).
  const hlEl = ref<HTMLElement | null>(null);
  const skillParts = computed(() => splitSkillTokens(opts.text.value, skills.value.map((s) => s.name)));
  const hasSkillPill = computed(() => skillParts.value.some((p) => p.pill));
  function syncHighlightScroll() {
    const el = opts.element();
    if (hlEl.value && el) hlEl.value.scrollTop = el.scrollTop;
  }

  function caret(): number {
    return opts.element()?.selectionStart ?? opts.text.value.length;
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
    nextTick(syncHighlightScroll);
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
        .map((p) => ({ key: p, label: `@${p.slice(p.lastIndexOf("/") + 1)}`, hint: p, insert: `@${p}` }));
      activeIndex.value = 0;
      return;
    }

    const source = t.kind === "$" ? skills.value : (opts.commands?.value ?? []);
    suggestions.value = source
      .filter((c) => c.name.toLowerCase().startsWith(q))
      .slice(0, 8)
      .map((c) => ({ key: c.name, label: `/${c.name}`, hint: c.description, insert: `/${c.name}` }));
    activeIndex.value = 0;
  }

  /** Swap the trigger token before the cursor for `s.insert` plus a space. */
  function apply(s: ComposerSuggestion) {
    const t = triggerAtCursor();
    if (!t) return;
    const pos = caret();
    const upto = opts.text.value.slice(0, pos);
    const after = opts.text.value.slice(pos);
    const base = upto.slice(0, upto.length - (t.q.length + 1)); // +1 = the trigger char
    const sep = after.startsWith(" ") ? "" : " ";
    opts.text.value = `${base}${s.insert}${sep}${after}`;
    close();
    nextTick(() => {
      const el = opts.element();
      opts.onApplied?.();
      if (!el) return;
      el.focus();
      const c = base.length + s.insert.length + sep.length;
      el.selectionStart = el.selectionEnd = c;
    });
  }

  /**
   * Suggestion-list navigation. Returns true when the key was consumed — the
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
    hlEl, skillParts, hasSkillPill, syncHighlightScroll,
    update, apply, close, handleKeydown,
  };
}
