<template>
  <div class="flex min-h-0 flex-1">
    <!-- LEFT: lists + commit composer -->
    <div class="gc-left flex w-[32%] min-w-[300px] max-w-[460px] shrink-0 flex-col border-r border-border">
      <div v-if="git.opState" class="shrink-0 border-b px-3 py-2.5" :class="git.conflicted.length ? 'gc-banner-alarm' : 'gc-banner-warn'">
        <div class="text-[12.5px] font-medium text-foreground">
          {{ git.conflicted.length ? `${opLabel} stopped on a conflict.` : `${opLabel} is waiting to be finished.` }}
        </div>
        <div class="mt-0.5 text-[11.5px] text-secondary-foreground">
          {{ git.conflicted.length
            ? `Resolve ${git.conflicted.length} ${git.conflicted.length === 1 ? "file" : "files"} and mark them resolved.`
            : "Conflicts are resolved, you can continue." }}
        </div>
        <div class="mt-2 flex gap-1.5">
          <button class="gc-btn gc-btn-solid" :disabled="git.conflicted.length > 0" @click="git.continueOp()">Continue {{ git.opState }}</button>
          <button class="gc-btn" @click="git.abortOp()">Abort {{ git.opState }}</button>
        </div>
      </div>

      <div class="min-h-0 flex-1 overflow-y-auto p-1.5">
        <section v-for="sec in sections" :key="sec.key" class="mb-2">
          <div class="flex h-7 items-center gap-2 px-2">
            <span class="text-[11.5px] font-medium" :class="sec.tone">{{ sec.title }}</span>
            <span class="text-[11px] tabular-nums text-muted-foreground">{{ sec.items.length }}</span>
            <span class="flex-1" />
            <button v-if="sec.key === 'unstaged' && sec.items.length" class="gc-btn" @click="git.stageAll()"><PhCaretDoubleDown :size="12" /> Stage all</button>
            <button v-if="sec.key === 'staged' && sec.items.length" class="gc-btn" @click="git.unstageAll()"><PhCaretDoubleUp :size="12" /> Unstage all</button>
          </div>
          <div v-if="!sec.items.length && sec.key !== 'conflicted'" class="px-2 pb-1 text-[12px] text-muted-foreground opacity-80">
            {{ sec.key === "staged" ? "Nothing staged for commit." : "No changes." }}
          </div>
          <div
            v-for="e in sec.items"
            :key="e.key"
            class="group flex h-[27px] cursor-default items-center gap-2 rounded-md pl-2 pr-1 text-[12.5px]"
            :class="sel === e.key ? 'gc-row-active' : 'hover:bg-hover'"
            @mousedown="select(e.key)"
            @dblclick="e.kind === 'staged' ? git.unstageFile(e.path) : e.kind !== 'conflicted' && git.stageFile(e.path)"
          >
            <span class="w-3 shrink-0 text-center font-mono text-[11px] font-semibold" :class="badge(e).cls">{{ badge(e).label }}</span>
            <span class="min-w-0 flex-1 truncate" :title="e.path">
              <span class="text-foreground">{{ base(e.path) }}</span><span v-if="dir(e.path)" class="ml-1.5 text-muted-foreground">{{ dir(e.path) }}</span>
            </span>
            <span class="flex shrink-0 gap-0.5" :class="sel !== e.key && 'opacity-0 group-hover:opacity-100'" @mousedown.stop>
              <template v-if="e.kind === 'conflicted'">
                <button class="gc-row-btn !w-auto px-1 text-[11px]" title="Keep my version (ours)" @click="git.resolveWith(e.path, 'ours')">Ours</button>
                <button class="gc-row-btn !w-auto px-1 text-[11px]" title="Take their version (theirs)" @click="git.resolveWith(e.path, 'theirs')">Theirs</button>
                <button class="gc-row-btn" title="Mark resolved" @click="git.markResolved(e.path)"><PhCheck :size="13" /></button>
              </template>
              <button v-else-if="e.kind === 'staged'" class="gc-row-btn" title="Unstage" @click="git.unstageFile(e.path)"><PhMinus :size="13" /></button>
              <template v-else>
                <button class="gc-row-btn" title="Discard changes" @click="askDiscardFile(e)"><PhArrowCounterClockwise :size="13" /></button>
                <button class="gc-row-btn" title="Stage" @click="git.stageFile(e.path)"><PhPlus :size="13" /></button>
              </template>
            </span>
          </div>
        </section>
      </div>

      <!-- Commit composer -->
      <div class="shrink-0 border-t border-border p-2.5">
        <div class="relative">
          <textarea
            v-model="git.commitMsg"
            class="gc-input w-full resize-none rounded-lg border px-3 py-2 pb-8 text-[13px] leading-relaxed text-foreground outline-none placeholder:text-muted-foreground"
            :placeholder="git.staged.length ? 'Commit message' : 'Commit message (stages everything if nothing is staged)'"
            spellcheck="false"
            rows="4"
            @keydown.meta.enter.prevent="git.commit()"
            @keydown.ctrl.enter.prevent="git.commit()"
          />
          <button
            class="gc-btn absolute bottom-2.5 right-1.5"
            :disabled="!git.hasWorkingTreeChanges || git.generating"
            title="Generate commit message (stages all changes first if needed)"
            @click="git.generateCommitMessage()"
          >
            <PhSparkle :size="14" :class="git.generating && 'animate-pulse'" />
          </button>
        </div>
        <div v-if="git.generateError" class="mt-1 text-[11px] text-destructive">{{ git.generateError }}</div>
        <div class="mt-2 flex flex-wrap gap-1">
          <button
            v-for="t in COMMIT_TYPES"
            :key="t"
            class="gc-chip"
            :class="activeType === t && 'gc-chip-active'"
            @click="applyType(t)"
          >{{ t }}</button>
        </div>
        <div class="mt-2 flex items-center gap-1.5">
          <span class="flex-1" />
          <button class="gc-btn" :disabled="!git.hasWorkingTreeChanges || busy" title="⌘↵ (auto-generates a message if the box is empty)" @click="git.commit()">
            <PhGitCommit :size="13" :class="(git.generating || git.committing) && 'animate-spin'" />
            {{ git.generating ? "Generating…" : git.committing ? "Committing…" : `Commit${git.staged.length ? ` (${git.staged.length})` : ""}` }}
          </button>
          <button class="gc-btn gc-btn-solid" :disabled="!git.hasWorkingTreeChanges || busy" @click="commitAndPush()">
            <PhArrowUp :size="13" :class="git.pushing && 'animate-spin'" />
            {{ git.pushing ? "Pushing…" : "Commit & Push" }}
          </button>
        </div>
      </div>
    </div>

    <!-- RIGHT: every changed file's diff, stacked -->
    <div class="flex min-w-[360px] flex-1 flex-col">
      <div v-if="!entries.length" class="grid h-full place-items-center">
        <div class="flex flex-col items-center gap-2 text-center">
          <PhCheckCircle :size="40" class="text-muted-foreground opacity-60" />
          <div class="text-[14px] text-foreground">Working tree clean.</div>
          <div class="text-[12.5px] text-muted-foreground">No uncommitted changes on {{ git.branch || "(detached)" }}.</div>
        </div>
      </div>
      <template v-else>
        <div class="flex h-10 shrink-0 items-center gap-2 border-b border-border px-4">
          <span class="flex-1 text-[12.5px] text-secondary-foreground">{{ entries.length }} changed {{ entries.length === 1 ? "file" : "files" }}</span>
        </div>
        <div ref="scroller" class="min-h-0 flex-1 overflow-y-auto">
          <!-- ponytail: a file's diff is fetched + rendered only once it nears the viewport (vNear), and
               content-visibility skips layout of loaded ones offscreen; full virtualization if 1000s of files matter -->
          <div v-for="e in entries" :key="cwd + e.key" v-near="e.key" :data-diff="e.key" class="gc-file border-b border-border">
            <div class="gc-sticky sticky top-0 z-10 flex h-10 items-center gap-2 border-b px-4">
              <span class="w-3 shrink-0 text-center font-mono text-[11px] font-semibold" :class="badge(e).cls">{{ badge(e).label }}</span>
              <span v-if="e.kind === 'staged'" class="gc-staged-tag rounded px-1.5 text-[10.5px]">staged</span>
              <span class="min-w-0 flex-1 truncate text-[13px]" :title="e.path">
                <span class="text-foreground">{{ base(e.path) }}</span><span v-if="dir(e.path)" class="ml-1.5 text-muted-foreground">{{ dir(e.path) }}</span>
              </span>
            </div>
            <div class="p-3">
              <div v-if="diffs[e.key] == null" class="h-24" />
              <div v-else-if="isDir(e)" class="font-mono text-[12px] leading-[20px] text-secondary-foreground">
                <div class="mb-1 font-sans text-[12.5px] text-muted-foreground">Untracked folder · {{ dirFiles(e).length }} {{ dirFiles(e).length === 1 ? "file" : "files" }}</div>
                <div v-for="f in dirFiles(e).slice(0, 200)" :key="f" class="truncate" :title="f">{{ f.slice(e.path.length) }}</div>
                <div v-if="dirFiles(e).length > 200" class="font-sans text-muted-foreground">… {{ dirFiles(e).length - 200 }} more</div>
              </div>
              <GitDiff
                v-else
                :text="diffs[e.key]"
                :path="e.path"
                :hunk-actions="e.kind === 'staged' || e.kind === 'unstaged'"
                :staged="e.kind === 'staged'"
                @stage-hunk="(i) => hunkOp(e, i, git.stageHunk)"
                @unstage-hunk="(i) => hunkOp(e, i, git.unstageHunk)"
                @discard-hunk="(i) => askDiscardHunk(e, i)"
              />
            </div>
          </div>
        </div>
      </template>
    </div>

    <!-- confirm -->
    <div v-if="confirm" class="fixed inset-0 z-50 grid place-items-center bg-black/40" @mousedown.self="confirm = null" @keydown.esc="confirm = null">
      <div class="w-[360px] rounded-xl border border-border bg-panel p-4 shadow-2xl">
        <div class="text-[14px] font-medium text-foreground">{{ confirm.title }}</div>
        <div class="mt-1 text-[12.5px] text-secondary-foreground">{{ confirm.body }}</div>
        <div class="mt-4 flex justify-end gap-1.5">
          <button class="gc-btn" @click="confirm = null">Cancel</button>
          <button v-focus class="gc-btn gc-btn-danger" @click="runConfirm()">Discard</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from "vue";
import { invoke } from "@tauri-apps/api/core";
import {
  PhArrowCounterClockwise, PhArrowUp, PhCaretDoubleDown, PhCaretDoubleUp, PhCheck, PhCheckCircle,
  PhGitCommit, PhMinus, PhPlus, PhSparkle,
} from "@phosphor-icons/vue";
import { useGitStore } from "@/stores/git";
import { splitHunks } from "@/lib/gitHunks";
import GitDiff from "./GitDiff.vue";

const props = defineProps<{ cwd: string }>();
const git = useGitStore();

type Kind = "conflicted" | "unstaged" | "untracked" | "staged";
interface Entry { key: string; path: string; kind: Kind; code: string }

const mk = (kind: Kind, f: { path: string; x: string; y: string }): Entry =>
  ({ key: `${kind}:${f.path}`, path: f.path, kind, code: kind === "staged" ? f.x : kind === "untracked" ? "?" : kind === "conflicted" ? "U" : f.y });

const sections = computed(() => [
  ...(git.conflicted.length ? [{ key: "conflicted", title: "Conflicts", tone: "text-destructive", items: git.conflicted.map((f) => mk("conflicted", f)) }] : []),
  { key: "unstaged", title: "Unstaged", tone: "text-muted-foreground", items: [...git.unstaged.map((f) => mk("unstaged", f)), ...git.untracked.map((f) => mk("untracked", f))] },
  { key: "staged", title: "Staged", tone: "gc-green", items: git.staged.map((f) => mk("staged", f)) },
]);
const entries = computed(() => sections.value.flatMap((s) => s.items));

const BADGES: Record<string, [string, string]> = {
  M: ["M", "gc-yellow"], A: ["A", "gc-green"], "?": ["N", "gc-green"], D: ["D", "text-destructive"],
  R: ["R", "gc-blue"], C: ["C", "gc-blue"], T: ["T", "gc-blue"], U: ["!", "text-destructive"],
};
const badge = (e: Entry) => { const [label, cls] = BADGES[e.code] ?? [e.code, "text-secondary-foreground"]; return { label, cls }; };
// porcelain collapses an untracked folder to one `dir/` entry
const isDir = (e: Entry) => e.path.endsWith("/");
const dirFiles = (e: Entry) => (diffs.value[e.key] ?? "").split("\n").filter(Boolean);
const trim = (p: string) => p.replace(/\/$/, "");
const base = (p: string) => trim(p).slice(trim(p).lastIndexOf("/") + 1);
const dir = (p: string) => trim(p).slice(0, Math.max(0, trim(p).lastIndexOf("/")));

// ── selection: picking a file scrolls its diff into view ──
const sel = ref<string | null>(null);
const scroller = ref<HTMLElement | null>(null);
function select(key: string) {
  sel.value = key;
  nextTick(() => scroller.value?.querySelector(`[data-diff="${CSS.escape(key)}"]`)?.scrollIntoView({ block: "start" }));
}

// ── diffs: fetched lazily per file once it nears the viewport; the near ones refetch on every status change ──
const diffs = ref<Record<string, string>>({});
const near = new Set<string>();
function diffArgs(e: Entry): string[] {
  if (e.kind === "staged") return ["diff", "--cached", "--", e.path];
  if (isDir(e)) return ["ls-files", "--others", "--exclude-standard", "--", e.path];
  if (e.kind === "untracked") return ["diff", "--no-index", "--", "/dev/null", e.path];
  return ["diff", "--", e.path];
}
let loadGen = 0;
async function loadDiffs(list: Entry[]) {
  const g = loadGen;
  const dirAtStart = props.cwd;
  await Promise.all(list.map(async (e) => {
    // run_git directly: `diff --no-index` exits 1 when files differ, which runGit would treat as an error
    const text = await invoke<{ stdout: string }>("run_git", { cwd: dirAtStart, args: diffArgs(e) }).then((o) => o.stdout, () => "");
    // unchanged text → no write, so GitDiff doesn't re-parse / re-render
    if (g === loadGen && dirAtStart === props.cwd && diffs.value[e.key] !== text) diffs.value[e.key] = text;
  }));
}
watch(() => [git.staged, git.unstaged, git.untracked, git.conflicted], () => loadDiffs(entries.value.filter((e) => near.has(e.key))));
watch(() => props.cwd, () => { loadGen++; sel.value = null; near.clear(); diffs.value = {}; });

let io: IntersectionObserver | null = null;
const vNear = {
  mounted(el: HTMLElement, b: { value: string }) {
    if (io?.root !== el.parentElement) {
      io?.disconnect();
      io = new IntersectionObserver((hits) => {
        const fresh = hits.filter((h) => h.isIntersecting).map((h) => (h.target as HTMLElement).dataset.diff!).filter((k) => !near.has(k));
        fresh.forEach((k) => near.add(k));
        if (fresh.length) loadDiffs(entries.value.filter((e) => fresh.includes(e.key)));
      }, { root: el.parentElement, rootMargin: "800px 0px" });
    }
    el.dataset.diff = b.value;
    io.observe(el);
  },
  unmounted(el: HTMLElement) { io?.unobserve(el); },
};
onBeforeUnmount(() => io?.disconnect());

// ── hunks: GitDiff's index i == splitHunks(text)[i] for a one-file diff ──
function hunkOp(e: Entry, i: number, run: (patch: string) => unknown) {
  const h = splitHunks(diffs.value[e.key] ?? "")[i];
  if (h) run(h.patch);
}

// ── discard confirm ──
const confirm = ref<{ title: string; body: string; run: () => unknown } | null>(null);
function runConfirm() { const c = confirm.value; confirm.value = null; c?.run(); }
const askDiscardHunk = (e: Entry, i: number) =>
  (confirm.value = { title: "Discard this hunk?", body: "The changes in this block are lost.", run: () => hunkOp(e, i, git.discardHunk) });
const askDiscardFile = (e: Entry) =>
  (confirm.value = {
    title: `Discard changes in ${base(e.path)}?`,
    body: e.kind === "untracked" ? "The untracked file is deleted. This cannot be undone." : "Uncommitted changes are lost and cannot be restored.",
    run: () => (e.kind === "untracked" ? cleanFile(e.path) : git.discardFile(e.path)),
  });
async function cleanFile(path: string) {
  await invoke("run_git", { cwd: props.cwd, args: ["clean", "-fd", "-q", "--", path] }).catch(() => {});
  await git.refresh(true);
}
const vFocus = { mounted: (el: HTMLElement) => el.focus() };

// ── commit composer (same behaviour as GitPanel) ──
const COMMIT_TYPES = ["feat", "fix", "docs", "chore", "refactor", "test", "style"] as const;
const activeType = computed(() => git.commitMsg.match(/^([a-z]+)(\([^)]+\))?:\s/)?.[1] ?? null);
function applyType(t: string) {
  const prefix = /^[a-z]+(\([^)]+\))?:\s/;
  git.commitMsg = prefix.test(git.commitMsg) ? git.commitMsg.replace(prefix, `${t}: `) : `${t}: ${git.commitMsg}`;
}
const busy = computed(() => git.generating || git.committing || git.pushing);
async function commitAndPush() {
  await git.push(await git.commit());
}
const opLabel = computed(() => ({ merge: "Merge", rebase: "Rebase", "cherry-pick": "Cherry-pick", revert: "Revert" })[git.opState ?? "merge"]);
</script>

<style scoped>
.gc-left { background: color-mix(in srgb, var(--bg-base) 40%, var(--bg-panel)); }
.gc-green { color: var(--green); }
.gc-yellow { color: var(--yellow); }
.gc-blue { color: var(--blue, var(--accent)); }
.gc-row-active { background: color-mix(in srgb, var(--accent) 14%, transparent); }
.gc-row-btn {
  display: grid; place-items: center; width: 22px; height: 22px; border-radius: 4px; color: var(--text-secondary);
}
.gc-row-btn:hover { background: var(--bg-hover); color: var(--text-primary); }
.gc-btn {
  display: inline-flex; align-items: center; gap: 5px; height: 26px; flex-shrink: 0;
  border-radius: 6px; padding: 0 9px; font-size: 12px; color: var(--text-secondary);
  background: color-mix(in srgb, var(--border) 35%, transparent);
}
.gc-btn:hover:not(:disabled) { background: var(--bg-hover); color: var(--text-primary); }
.gc-btn:disabled { opacity: 0.35; cursor: default; }
.gc-btn-solid { background: var(--accent); color: white; font-weight: 500; }
.gc-btn-solid:hover:not(:disabled) { background: color-mix(in srgb, var(--accent) 85%, white); color: white; }
.gc-btn-danger { background: var(--red); color: white; font-weight: 500; }
.gc-btn-danger:hover { background: color-mix(in srgb, var(--red) 85%, white); color: white; }
.gc-input {
  border-color: color-mix(in srgb, var(--border) 80%, transparent);
  background: var(--bg-panel);
}
.gc-input:focus { border-color: color-mix(in srgb, var(--accent) 60%, transparent); }
.gc-chip {
  border-radius: 5px; border: 1px solid var(--border); padding: 1px 7px;
  font-family: var(--font-mono); font-size: 11px; color: var(--text-muted);
}
.gc-chip:hover { background: var(--bg-hover); color: var(--text-secondary); }
.gc-chip-active {
  border-color: color-mix(in srgb, var(--accent) 45%, transparent);
  background: color-mix(in srgb, var(--accent) 14%, transparent); color: var(--accent);
}
.gc-banner-alarm { border-color: color-mix(in srgb, var(--red) 30%, transparent); background: color-mix(in srgb, var(--red) 10%, transparent); }
.gc-banner-warn { border-color: color-mix(in srgb, var(--yellow) 25%, transparent); background: color-mix(in srgb, var(--yellow) 10%, transparent); }
.gc-file { content-visibility: auto; contain-intrinsic-size: auto 400px; }
.gc-sticky { background: var(--bg-base); border-color: color-mix(in srgb, var(--border) 60%, transparent); }
.gc-staged-tag { background: color-mix(in srgb, var(--green) 15%, transparent); color: var(--green); }
</style>
