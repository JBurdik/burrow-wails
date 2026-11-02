<template>
  <div v-if="!commits.length" class="flex flex-1 items-center justify-center text-xs text-muted-foreground">
    {{ loaded ? "No commits yet. Make the first one in Changes." : "Loading history…" }}
  </div>
  <div v-else ref="hostRef" class="flex min-h-0 min-w-0 flex-1 overflow-hidden">
    <!-- commit list -->
    <div class="gh-list flex min-w-[300px] flex-col" :style="{ width: split * 100 + '%' }">
      <button
        v-if="dirty > 0"
        class="flex h-[34px] shrink-0 items-center gap-3 border-b border-border pr-3 text-left hover:bg-hover"
        @click="emit('open-changes')"
      >
        <svg :width="graphW" height="34" class="shrink-0">
          <circle :cx="laneX(headRow?.lane ?? 0)" cy="17" r="5" fill="none" stroke="var(--accent)" stroke-width="1.6" stroke-dasharray="2.5 2" />
        </svg>
        <span class="text-[12.5px] text-accent">Uncommitted changes</span>
        <span class="text-[12px] text-muted-foreground">{{ dirty }} {{ dirty === 1 ? "file" : "files" }}</span>
      </button>
      <div ref="listRef" tabindex="0" class="min-h-0 flex-1 overflow-y-auto outline-none" @keydown="onKey" @scroll="onScroll">
        <div
          v-for="(c, i) in commits"
          :key="c.hash"
          :data-i="i"
          class="flex items-center pr-3"
          :class="c.hash === selected ? 'bg-accent/[0.14]' : 'hover:bg-hover'"
          :style="{ height: ROW_H + 'px' }"
          @mousedown="selected = c.hash"
          @contextmenu.prevent="openMenu(c, $event)"
        >
          <svg :width="graphW" :height="ROW_H" class="shrink-0 overflow-visible">
            <g fill="none" stroke-width="1.8" stroke-linecap="round">
              <path v-for="(e, k) in rows[i].up" :key="'u' + k" :d="curve(laneX(e.from), 0, laneX(e.to), e.from === e.to && e.to !== rows[i].lane ? MID + 0.5 : MID)" :stroke="laneColor(e.color)" />
              <path v-for="(e, k) in rows[i].down" :key="'d' + k" :d="curve(laneX(e.from), e.from === e.to && e.from !== rows[i].lane ? MID - 0.5 : MID, laneX(e.to), ROW_H)" :stroke="laneColor(e.color)" />
            </g>
            <!-- HEAD wears a paw (GitPaw's mark); merges are hollow -->
            <g v-if="c.hash === headHash" :fill="laneColor(rows[i].color)">
              <circle :cx="laneX(rows[i].lane)" :cy="MID" r="9" opacity="0.18" />
              <ellipse :cx="laneX(rows[i].lane)" :cy="MID + 1.6" rx="3.8" ry="3.3" stroke="var(--bg-panel)" stroke-width="1" />
              <circle v-for="([dx, dy], k) in PAW_TOES" :key="k" :cx="laneX(rows[i].lane) + dx" :cy="MID + dy" r="1.5" />
            </g>
            <circle v-else-if="rows[i].isMerge" :cx="laneX(rows[i].lane)" :cy="MID" r="4" fill="var(--bg-panel)" :stroke="laneColor(rows[i].color)" stroke-width="2" />
            <circle v-else :cx="laneX(rows[i].lane)" :cy="MID" r="4.5" :fill="laneColor(rows[i].color)" stroke="var(--bg-panel)" stroke-width="2" />
          </svg>
          <div class="flex min-w-0 flex-1 items-center gap-1.5">
            <span v-if="c.refs.length" class="flex max-w-[60%] shrink-0 items-center gap-1 overflow-hidden">
              <span
                v-for="r in refPills(c.refs)"
                :key="r.kind + r.label"
                class="flex h-[19px] shrink-0 items-center gap-1 rounded-[5px] border px-1.5 text-[11px] font-medium"
                :class="r.head && 'font-semibold'"
                :style="r.kind === 'tag' ? tagStyle : r.kind === 'remote' ? remoteStyle : pillStyle(laneColor(rows[i].color))"
              >
                <span v-if="r.head" class="size-1.5 rounded-full bg-accent" />
                <PhTag v-if="r.kind === 'tag'" :size="10" />
                <PhCloud v-if="r.kind === 'remote'" :size="10" class="opacity-70" />
                {{ r.label }}
                <PhCloud v-if="r.synced" :size="10" class="opacity-70" />
              </span>
            </span>
            <span
              class="truncate text-[12.5px]"
              :class="[c.hash === selected || c.hash === headHash ? 'text-foreground' : 'text-secondary-foreground', rows[i].isMerge && '!text-muted-foreground']"
            >{{ c.subject }}</span>
          </div>
          <span class="gh-author ml-3 w-[120px] shrink-0 truncate text-right text-[12px] text-muted-foreground">{{ c.author }}</span>
          <span class="ml-3 w-[86px] shrink-0 truncate text-right text-[12px] tabular-nums text-muted-foreground">{{ c.relTime }}</span>
        </div>
      </div>
    </div>

    <!-- divider -->
    <div class="group relative w-px shrink-0 cursor-col-resize bg-border" @mousedown.prevent="startDrag">
      <span class="absolute inset-y-0 -left-1 -right-1 group-hover:bg-accent/30" />
    </div>

    <!-- commit detail -->
    <div class="flex min-w-0 flex-1 flex-col overflow-hidden bg-[color-mix(in_srgb,var(--bg-panel)_60%,transparent)]">
      <GitCommitDetail v-if="selected" :key="selected" :cwd="cwd" :hash="selected" @select="selected = $event" />
      <div v-else class="flex flex-1 items-center justify-center text-xs text-muted-foreground">Select a commit.</div>
    </div>

    <!-- commit context menu (ported from GitPanel) -->
    <template v-if="menuAt">
      <div class="fixed inset-0 z-[999]" @click="menuAt = null" @contextmenu.prevent="menuAt = null" />
      <div class="fixed z-[1000] w-[210px] rounded-lg border border-border bg-[var(--bg-dropdown)] p-1 shadow-[0_14px_36px_rgba(0,0,0,0.55)]" :style="{ left: menuAt.x + 'px', top: menuAt.y + 'px' }">
        <div class="truncate px-2 pb-1 pt-0.5 font-mono text-[10px] text-muted-foreground">{{ menuAt.c.shortHash }} · {{ menuAt.c.subject }}</div>
        <template v-for="b in menuBranches" :key="b">
          <button class="gh-menu-item" @click="menu(() => git.switchBranch(b))">Checkout <b class="font-mono">{{ b }}</b></button>
          <button v-if="git.branch" class="gh-menu-item" @click="menu(() => askMerge(b))">Merge <b class="font-mono">{{ b }}</b> into current</button>
          <button v-if="git.branch" class="gh-menu-item" @click="menu(() => askRebase(b))">Rebase current onto <b class="font-mono">{{ b }}</b></button>
          <div class="gh-menu-sep" />
        </template>
        <button class="gh-menu-item" @click="menu((c) => git.checkoutCommit(c.hash))">Checkout (detached)</button>
        <button class="gh-menu-item" @click="menu((c) => askName('New branch', `From ${c.shortHash}`, (n) => git.branchAt(n, c.hash)))">New branch here…</button>
        <button class="gh-menu-item" @click="menu((c) => askName('New tag', `On ${c.shortHash}`, (n) => git.tagAt(n, c.hash)))">Tag here…</button>
        <div class="gh-menu-sep" />
        <button class="gh-menu-item" :disabled="menuAt.c.hash === headHash" @click="menu((c) => git.cherryPick(c.hash))">Cherry-pick</button>
        <button class="gh-menu-item" @click="menu((c) => git.revertCommit(c.hash))">Revert</button>
        <div class="gh-menu-sep" />
        <button class="gh-menu-item" @click="menu((c) => askReset(c, 'soft'))">Reset here (soft)</button>
        <button class="gh-menu-item" @click="menu((c) => askReset(c, 'mixed'))">Reset here (mixed)</button>
        <button class="gh-menu-item !text-destructive" @click="menu((c) => askReset(c, 'hard'))">Reset here (hard)…</button>
        <div class="gh-menu-sep" />
        <button class="gh-menu-item" @click="menu((c) => navigator.clipboard?.writeText(c.hash))">Copy hash</button>
      </div>
    </template>

    <!-- confirm / name prompt (window.confirm is a no-op in the Wails webview) -->
    <div v-if="dialog" class="fixed inset-0 z-[1001] flex items-center justify-center bg-black/60" @click.self="dialog = null" @keydown.esc="dialog = null">
      <div class="flex w-[380px] flex-col gap-3 rounded-[10px] border border-border bg-[var(--bg-dropdown)] p-5">
        <h3 class="text-sm font-semibold text-foreground">{{ dialog.title }}</h3>
        <p v-if="dialog.body" class="text-[12px] leading-[1.6] text-secondary-foreground">{{ dialog.body }}</p>
        <input
          v-if="dialog.input"
          v-model="dialog.value"
          v-focus
          class="w-full rounded border border-border bg-[color-mix(in_srgb,var(--border)_20%,var(--bg-panel))] px-2 py-1.5 font-mono text-xs text-foreground outline-none focus:border-accent/50"
          @keydown.enter.prevent="runDialog"
        />
        <div class="flex justify-end gap-2">
          <button class="rounded-md border border-border bg-hover px-3.5 py-1.5 text-xs text-secondary-foreground hover:text-foreground" @click="dialog = null">Cancel</button>
          <button
            v-focus="!dialog.input"
            class="rounded-md border-0 px-3.5 py-1.5 text-xs font-semibold text-white hover:opacity-90 disabled:opacity-40"
            :class="dialog.danger ? 'bg-destructive' : 'bg-accent'"
            :disabled="dialog.input && !dialog.value.trim()"
            @click="runDialog"
          >{{ dialog.confirm }}</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick } from "vue";
import { PhTag, PhCloud } from "@phosphor-icons/vue";
import { useGitStore, type GraphCommit } from "@/stores/git";
import { layoutGraph } from "@/lib/gitGraph";
import GitCommitDetail from "./GitCommitDetail.vue";

const props = defineProps<{ cwd: string }>();
const emit = defineEmits<{ "open-changes": [] }>();
const git = useGitStore();
const navigator = window.navigator;

// ── geometry (GitPaw History) ──
const ROW_H = 30;
const MID = ROW_H / 2;
const LANE_W = 15;
const MAX_LANES = 14;
const PAW_TOES = [[-4.4, -1.6], [-1.6, -4.6], [1.6, -4.6], [4.4, -1.6]] as const;
const LANE_COLORS = [
  "var(--accent)", "var(--green)", "var(--yellow)", "var(--blue)",
  "color-mix(in oklab, var(--accent) 55%, var(--red))", "color-mix(in oklab, var(--green) 50%, var(--blue))",
];
const laneColor = (i: number) => LANE_COLORS[i % LANE_COLORS.length];
const laneX = (i: number) => 10 + Math.min(i, MAX_LANES) * LANE_W;
const curve = (x1: number, y1: number, x2: number, y2: number) =>
  x1 === x2 ? `M${x1} ${y1}V${y2}` : `M${x1} ${y1}C${x1} ${(y1 + y2) / 2} ${x2} ${(y1 + y2) / 2} ${x2} ${y2}`;

// ── data ──
// ponytail: plain v-for, no virtualization; fine to a few thousand rows, add a virtual list if history pages past that
const PAGE = 500;
const limit = ref(PAGE);
const commits = ref<GraphCommit[]>([]);
const loaded = ref(false);
const layout = computed(() => layoutGraph(commits.value));
const rows = computed(() => layout.value.rows);
const graphW = computed(() => laneX(Math.min(layout.value.maxLanes, MAX_LANES)) + 4);
const headHash = computed(() => commits.value.find((c) => c.refs.some((r) => r === "HEAD" || r.startsWith("HEAD -> ")))?.hash);
const headRow = computed(() => rows.value[commits.value.findIndex((c) => c.hash === headHash.value)]);
const dirty = computed(() => git.staged.length + git.unstaged.length + git.untracked.length + git.conflicted.length);
const selected = ref<string | null>(null);

watch(() => props.cwd, () => { selected.value = null; limit.value = PAGE; loaded.value = false; commits.value = []; });
// git.log is re-read on every refresh, so it is the "history may have moved" signal
let gen = 0;
watch(() => [props.cwd, git.log, limit.value], async () => {
  const g = ++gen;
  const list = await git.fetchGraph(limit.value);
  if (g !== gen) return;
  commits.value = list;
  loaded.value = true;
  if (!selected.value || !list.some((c) => c.hash === selected.value)) selected.value = headHash.value ?? list[0]?.hash ?? null;
}, { immediate: true });

function onScroll(e: Event) {
  const el = e.target as HTMLElement;
  if (el.scrollTop + el.clientHeight > el.scrollHeight - 50 * ROW_H && commits.value.length >= limit.value) limit.value += PAGE;
}

// ── keyboard ──
const listRef = ref<HTMLElement | null>(null);
function onKey(e: KeyboardEvent) {
  if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
  e.preventDefault();
  const cur = commits.value.findIndex((c) => c.hash === selected.value);
  const next = Math.max(0, Math.min(commits.value.length - 1, cur + (e.key === "ArrowDown" ? 1 : -1)));
  selected.value = commits.value[next]?.hash ?? null;
  nextTick(() => listRef.value?.querySelector(`[data-i="${next}"]`)?.scrollIntoView({ block: "nearest" }));
}

// ── split (persisted, GitPaw's useSplit) ──
const SPLIT_KEY = "burrow.git.split.history";
const split = ref(Number(localStorage.getItem(SPLIT_KEY)) || 0.56);
const hostRef = ref<HTMLElement | null>(null);
function startDrag() {
  const host = hostRef.value?.getBoundingClientRect();
  if (!host) return;
  const move = (ev: MouseEvent) => { split.value = Math.min(0.8, Math.max(0.25, (ev.clientX - host.left) / host.width)); };
  const up = () => {
    window.removeEventListener("mousemove", move);
    window.removeEventListener("mouseup", up);
    document.body.style.cursor = document.body.style.userSelect = "";
    localStorage.setItem(SPLIT_KEY, String(split.value));
  };
  window.addEventListener("mousemove", move);
  window.addEventListener("mouseup", up);
  document.body.style.cursor = "col-resize";
  document.body.style.userSelect = "none";
}

// ── ref pills: local + its same-named remote collapse into one pill with a cloud ──
interface RefPill { label: string; kind: "local" | "remote" | "tag"; head: boolean; synced: boolean }
function refPills(refs: string[]): RefPill[] {
  const locals = new Map<string, RefPill>();
  const remotes: string[] = [];
  const tags: RefPill[] = [];
  const known = new Set(git.branches);
  for (const r of refs) {
    if (r.startsWith("tag: ")) tags.push({ label: r.slice(5), kind: "tag", head: false, synced: false });
    else if (r.startsWith("HEAD -> ")) locals.set(r.slice(8), { label: r.slice(8), kind: "local", head: true, synced: false });
    else if (r === "HEAD" || r.endsWith("/HEAD")) continue;
    else if (known.has(r)) locals.set(r, { label: r, kind: "local", head: false, synced: false });
    else remotes.push(r);
  }
  const out: RefPill[] = [];
  for (const r of remotes) {
    const l = locals.get(r.split("/").slice(1).join("/"));
    if (l) l.synced = true;
    else out.push({ label: r, kind: "remote", head: false, synced: false });
  }
  return [...locals.values(), ...out, ...tags];
}
const pillStyle = (color: string) => ({
  color,
  borderColor: `color-mix(in oklab, ${color} 45%, transparent)`,
  background: `color-mix(in oklab, ${color} 13%, transparent)`,
});
const tagStyle = { color: "var(--yellow)", borderColor: "transparent", background: "color-mix(in oklab, var(--yellow) 15%, transparent)" };
const remoteStyle = { color: "var(--text-muted)", borderColor: "var(--border)", background: "transparent" };

// ── commit context menu ──
const menuAt = ref<{ c: GraphCommit; x: number; y: number } | null>(null);
function openMenu(c: GraphCommit, e: MouseEvent) {
  selected.value = c.hash;
  // keep the 210px-wide, ~420px-tall menu on screen
  menuAt.value = { c, x: Math.min(e.clientX, window.innerWidth - 220), y: Math.min(e.clientY, window.innerHeight - 440) };
}
const menuBranches = computed(() =>
  menuAt.value ? refPills(menuAt.value.c.refs).filter((r) => r.kind === "local" && r.label !== git.branch).map((r) => r.label) : [],
);
function menu(fn: (c: GraphCommit) => unknown) {
  const c = menuAt.value?.c;
  menuAt.value = null;
  if (c) fn(c);
}

// ── confirm / prompt dialog ──
const dialog = ref<{ title: string; body?: string; input?: boolean; value: string; confirm: string; danger?: boolean; run: (v: string) => unknown } | null>(null);
function runDialog() {
  const d = dialog.value;
  if (!d || (d.input && !d.value.trim())) return;
  dialog.value = null;
  d.run(d.value.trim());
}
const askName = (title: string, body: string, run: (name: string) => unknown) =>
  (dialog.value = { title, body, input: true, value: "", confirm: "Create", run });
const askMerge = (b: string) =>
  (dialog.value = { title: `Merge ${b} into ${git.branch}?`, body: "Creates a merge commit unless it can fast-forward.", value: "", confirm: "Merge", run: () => git.mergeBranch(b) });
const askRebase = (b: string) => {
  // ponytail: "has upstream" ≈ "already shared"; an exact check would compare with the upstream's merge-base
  const shared = git.hasUpstream;
  dialog.value = {
    title: `Rebase ${git.branch} onto ${b}?`,
    body: shared
      ? `${git.branch} is already pushed. Rebasing rewrites its history and the next push will need --force.`
      : `Replays the commits of ${git.branch} on top of ${b}.`,
    value: "", confirm: "Rebase", danger: shared, run: () => git.rebaseOnto(b),
  };
};
const RESET_BODY = {
  soft: "Commits after this point go back to staged changes. Nothing is lost.",
  mixed: "Commits after this point come back as unstaged changes. Nothing is lost.",
  hard: "Commits after this point AND every uncommitted change are thrown away. This cannot be undone.",
} as const;
const askReset = (c: GraphCommit, mode: "soft" | "mixed" | "hard") =>
  (dialog.value = { title: `Reset ${git.branch || "HEAD"} to ${c.shortHash} (${mode})?`, body: RESET_BODY[mode], value: "", confirm: "Reset", danger: mode === "hard", run: () => git.resetTo(c.hash, mode) });

const vFocus = { mounted: (el: HTMLElement, b: { value?: boolean }) => { if (b.value !== false) el.focus(); } };
</script>

<style scoped>
/* ponytail: narrow list drops the author column so the subject keeps its room */
.gh-list { container-type: inline-size; }
@container (max-width: 620px) { .gh-author { display: none; } }
.gh-menu-item {
  display: block; width: 100%; text-align: left; border-radius: 5px;
  padding: 5px 8px; font-size: 12px; color: var(--text-secondary);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.gh-menu-item:hover:not(:disabled) { background: var(--bg-hover); color: var(--text-primary); }
.gh-menu-item:disabled { opacity: 0.4; }
.gh-menu-sep { margin: 4px 0; height: 1px; background: var(--border); }
</style>
