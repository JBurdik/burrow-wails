<template>
  <aside class="flex w-[260px] min-w-[180px] shrink-0 flex-col border-r border-border bg-panel text-[12.5px]">
    <!-- Project header: the GitPaw repo switcher, listing Burrow workspaces (worktrees too) -->
    <div class="px-2.5 pb-2 pt-2.5">
      <button
        class="flex w-full items-center gap-2.5 rounded-lg px-2 py-1.5 text-left hover:bg-hover"
        @click="repoOpen = !repoOpen"
      >
        <RepoTile :ws="selected" :size="26" />
        <div class="min-w-0 flex-1">
          <div class="truncate text-[14px] font-semibold tracking-[-0.01em] text-foreground">{{ selected ? wsLabel(selected) : "Git" }}</div>
          <div class="truncate text-[11.5px] text-muted-foreground">{{ selected ? home(selected.path) : "No workspace" }}</div>
        </div>
        <PhCaretDown :size="14" class="shrink-0 text-muted-foreground transition-transform" :class="repoOpen && 'rotate-180'" />
      </button>
      <div v-if="repoOpen" class="mt-1 max-h-[50vh] overflow-y-auto rounded-lg border border-border bg-base p-1">
        <button
          v-for="w in wsList"
          :key="w.id"
          class="flex w-full min-w-0 items-center gap-2 rounded-md px-2 py-1.5 text-left"
          :class="w.id === wsId ? 'bg-accent/15 text-foreground' : 'text-secondary-foreground hover:bg-hover hover:text-foreground'"
          @click="wsId = w.id; repoOpen = false"
        >
          <PhFolderSimple v-if="w.parent_id" :size="14" class="ml-1 shrink-0 text-muted-foreground" />
          <RepoTile v-else :ws="w" :size="16" />
          <span class="truncate">{{ wsLabel(w) }}</span>
        </button>
      </div>
    </div>

    <div class="px-2.5">
      <div class="relative">
        <PhMagnifyingGlass :size="12" class="pointer-events-none absolute left-2 top-1/2 -translate-y-1/2 text-muted-foreground" />
        <input
          v-model="query"
          class="h-[26px] w-full rounded-md border border-border bg-base pl-6 pr-2 text-[12px] text-foreground outline-none placeholder:text-muted-foreground focus:border-accent"
          placeholder="Filter branches"
          spellcheck="false"
          @keydown.esc="query = ''"
        />
      </div>
    </div>
    <div class="min-h-0 flex-1 overflow-y-auto px-2.5 pb-4">
      <!-- Branches -->
      <Section title="Branches" :count="local.length" :force="!!q">
        <template #action>
          <button class="rounded p-0.5 text-muted-foreground hover:text-foreground" title="New branch (⌘⇧B)" aria-label="New branch" @click="startNewBranch()">
            <PhPlus :size="13" />
          </button>
        </template>
        <form v-if="newBranch.open" class="mb-1 px-1" @submit.prevent="confirmNewBranch">
          <input
            ref="newBranchInput"
            v-model="newBranch.name"
            class="h-[26px] w-full rounded-md border border-border bg-base px-2 font-mono text-[12px] text-foreground outline-none focus:border-accent"
            :placeholder="newBranch.at ? `branch at ${newBranch.at.slice(0, 7)}` : 'new-branch-name'"
            @keydown.esc="newBranch.open = false"
            @blur="newBranch.open = false"
          />
        </form>
        <div v-if="q && !localRows.length" class="px-2 py-1 text-[12px] text-muted-foreground">No match.</div>
        <template v-for="row in localRows" :key="row.key">
        <Folder v-if="row.kind === 'folder'" :row="row" @toggle="toggle('local:' + row.key)" />
        <template v-else><ContextMenuRoot v-for="b in [row.item]" :key="b.full">
          <ContextMenuTrigger as-child>
            <Row :active="b.isHead" :depth="row.depth" :title="b.name" @dblclick="wtOf.has(b.name) ? openPath(wtOf.get(b.name)!) : !b.isHead && checkout(b.name)">
              <span class="grid size-3.5 shrink-0 place-items-center">
                <span v-if="b.isHead" class="size-2 rounded-full bg-accent shadow-[0_0_8px_var(--accent)]" />
                <PhGitBranch v-else :size="12" class="text-muted-foreground" />
              </span>
              <span class="min-w-0 flex-1 truncate" :class="b.isHead && 'font-semibold text-foreground'">{{ row.label }}</span>
              <PhTreeStructure v-if="wtOf.has(b.name)" :size="11" class="shrink-0 text-muted-foreground" :title="`Checked out in worktree ${home(wtOf.get(b.name)!)}`" />
              <span v-if="b.track === '[gone]'" class="text-[10.5px] text-muted-foreground" title="Upstream branch is gone">gone</span>
              <span v-else-if="b.track" class="flex shrink-0 gap-1 text-[10.5px] tabular-nums">
                <span v-if="trackNum(b.track, 'ahead')" class="text-success">↑{{ trackNum(b.track, "ahead") }}</span>
                <span v-if="trackNum(b.track, 'behind')" class="text-warning">↓{{ trackNum(b.track, "behind") }}</span>
              </span>
            </Row>
          </ContextMenuTrigger>
          <Menu>
            <Item v-if="wtOf.has(b.name)" @select="openPath(wtOf.get(b.name)!)">Open its worktree</Item>
            <Item v-else :disabled="b.isHead" @select="checkout(b.name)">Checkout {{ b.name }}</Item>
            <Item :disabled="b.isHead" @select="git.mergeBranch(b.name)">Merge into current</Item>
            <Item :disabled="b.isHead" @select="git.rebaseOnto(b.name)">Rebase current onto {{ b.name }}</Item>
            <ContextMenuSeparator class="my-1 h-px bg-border" />
            <Item @select="startNewBranch(b.hash)">New branch from here…</Item>
            <Item danger :disabled="b.isHead" @select="git.deleteBranch(b.name, b.track === '[gone]')">Delete</Item>
          </Menu>
        </ContextMenuRoot></template>
        </template>
      </Section>

      <!-- Worktrees -->
      <Section v-if="worktrees.length > 1" title="Worktrees" :count="worktrees.length">
        <template #icon><PhTreeStructure :size="11" /></template>
        <ContextMenuRoot v-for="(w, i) in worktrees" :key="w.path">
          <ContextMenuTrigger as-child>
            <Row
              :active="w.path === git.cwd"
              :title="home(w.path) + (wsByPath.has(w.path) ? '' : ' (not a Burrow workspace)')"
              @click="w.path !== git.cwd && openPath(w.path)"
            >
              <PhTreeStructure :size="12" class="shrink-0" :class="w.path === git.cwd ? 'text-accent' : 'text-muted-foreground'" />
              <span class="min-w-0 flex-1 truncate" :class="!wsByPath.has(w.path) && 'opacity-60'">{{ baseName(w.path) }}</span>
              <span class="max-w-[45%] truncate text-[10.5px] text-muted-foreground">{{ w.branch ?? w.head.slice(0, 7) }}</span>
            </Row>
          </ContextMenuTrigger>
          <Menu>
            <Item :disabled="w.path === git.cwd || !wsByPath.has(w.path)" @select="openPath(w.path)">Open</Item>
            <Item @select="navigator.clipboard?.writeText(w.path)">Copy path</Item>
            <ContextMenuSeparator class="my-1 h-px bg-border" />
            <!-- git refuses to remove the main worktree; index 0 of `worktree list` is always it -->
            <Item danger :disabled="i === 0" @select="askRemove(w)">Remove worktree…</Item>
          </Menu>
        </ContextMenuRoot>
      </Section>

      <!-- Remotes -->
      <Section v-for="(branches, remote) in remoteGroups" :key="remote" :title="String(remote)" :count="branches.length" collapsed :force="!!q">
        <template #icon><PhCloud :size="11" /></template>
        <template v-for="row in remoteRows(String(remote), branches)" :key="row.key">
        <Folder v-if="row.kind === 'folder'" :row="row" @toggle="toggle(remote + ':' + row.key)" />
        <template v-else><ContextMenuRoot v-for="b in [row.item]" :key="b.full">
          <ContextMenuTrigger as-child>
            <Row :depth="row.depth" :title="b.name" @dblclick="checkoutRemote(b.name)">
              <PhGitBranch :size="12" class="shrink-0 text-muted-foreground" />
              <span class="truncate">{{ row.label }}</span>
            </Row>
          </ContextMenuTrigger>
          <Menu>
            <Item @select="checkoutRemote(b.name)">Checkout {{ b.name }}</Item>
            <Item @select="git.mergeBranch(b.name)">Merge into current</Item>
            <Item @select="git.rebaseOnto(b.name)">Rebase current onto {{ b.name }}</Item>
          </Menu>
        </ContextMenuRoot></template>
        </template>
      </Section>

      <!-- Stashes -->
      <Section title="Stashes" :count="git.stashes.length">
        <template #icon><PhArchiveBox :size="11" /></template>
        <div v-if="!git.stashes.length" class="px-2 py-1 text-[12px] text-muted-foreground">Nothing stashed.</div>
        <ContextMenuRoot v-for="s in git.stashes" :key="s.ref">
          <ContextMenuTrigger as-child>
            <Row :title="`${s.ref} · ${s.relTime}`" @dblclick="git.stashPop(s.ref)">
              <PhArchiveBox :size="12" class="shrink-0 text-muted-foreground" />
              <span class="min-w-0 flex-1 truncate">{{ s.message.replace(/^(On|WIP on) [^:]+: /, "") }}</span>
            </Row>
          </ContextMenuTrigger>
          <Menu>
            <Item @select="git.stashPop(s.ref)">Pop (apply and drop)</Item>
            <Item @select="git.stashApply(s.ref)">Apply (keep)</Item>
            <ContextMenuSeparator class="my-1 h-px bg-border" />
            <Item danger @select="git.stashDrop(s.ref)">Drop</Item>
          </Menu>
        </ContextMenuRoot>
      </Section>

      <!-- Tags -->
      <Section v-if="tags.length" title="Tags" :count="tags.length" collapsed>
        <template #icon><PhTag :size="11" /></template>
        <ContextMenuRoot v-for="t in tags" :key="t.full">
          <ContextMenuTrigger as-child>
            <Row>
              <PhTag :size="12" class="shrink-0" style="color: var(--yellow)" />
              <span class="truncate">{{ t.name }}</span>
            </Row>
          </ContextMenuTrigger>
          <Menu>
            <Item @select="git.checkoutCommit(t.name)">Checkout (detached)</Item>
            <ContextMenuSeparator class="my-1 h-px bg-border" />
            <Item danger @select="deleteTag(t.name)">Delete tag</Item>
          </Menu>
        </ContextMenuRoot>
      </Section>
    </div>
    <!-- remove-worktree confirm (window.confirm is a no-op in the Wails webview) -->
    <div v-if="removing" class="fixed inset-0 z-[1001] flex items-start justify-center bg-black/45 pt-[20vh]" @click.self="removing = null" @keydown.esc="removing = null">
      <div class="flex w-[400px] max-w-[calc(100%-24px)] flex-col rounded-xl border border-border bg-[var(--bg-dropdown)] p-5 shadow-[0_24px_60px_rgba(0,0,0,0.55)]">
        <h3 class="text-[14px] font-semibold text-foreground">Remove worktree {{ baseName(removing.wt.path) }}?</h3>
        <p class="mt-2 text-[12.5px] leading-relaxed text-muted-foreground">
          Deletes <span class="font-mono">{{ home(removing.wt.path) }}</span> from disk<template v-if="wsByPath.has(removing.wt.path)"> and its Burrow workspace</template>.
          <template v-if="removing.wt.branch"> Branch <span class="font-mono">{{ removing.wt.branch }}</span> is kept.</template>
        </p>
        <p v-if="removing.error" class="mt-2 whitespace-pre-wrap rounded-md bg-destructive/10 px-2.5 py-2 font-mono text-[11.5px] text-destructive">{{ removing.error }}</p>
        <label class="mt-3 flex items-center gap-2 text-[12.5px] text-secondary-foreground">
          <input v-model="removing.force" type="checkbox" />
          Force — discard uncommitted and untracked changes
        </label>
        <div class="mt-5 flex justify-end gap-2">
          <button class="h-7 rounded-md border border-border bg-hover px-2.5 text-xs text-secondary-foreground hover:text-foreground" @click="removing = null">Cancel</button>
          <button
            v-focus
            class="h-7 rounded-md bg-destructive px-3 text-xs font-semibold text-white hover:opacity-90 disabled:opacity-40"
            :disabled="removing.busy"
            @click="confirmRemove"
          >{{ removing.busy ? "Removing…" : "Remove" }}</button>
        </div>
      </div>
    </div>
  </aside>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, nextTick, reactive, ref, watch } from "vue";
import { invoke } from "@tauri-apps/api/core";
import {
  ContextMenuContent, ContextMenuItem, ContextMenuPortal, ContextMenuRoot, ContextMenuSeparator, ContextMenuTrigger,
} from "reka-ui";
import {
  PhArchiveBox, PhCaretDown, PhCaretRight, PhCloud, PhFolderSimple, PhGitBranch, PhMagnifyingGlass, PhPlus, PhTag, PhTreeStructure,
} from "@phosphor-icons/vue";
import { useGitStore } from "@/stores/git";
import { useWorkspaceStore, type Workspace } from "@/stores/workspace";
import { useNotificationsStore } from "@/stores/notifications";
import { useTerminalTabsStore } from "@/stores/terminalTabs";
import { branchTreeRows, type BranchRow } from "@/lib/branchTree";

/** The selected workspace id; GitWindow owns it. */
const wsId = defineModel<number | null>("wsId", { required: true });

const git = useGitStore();
const wsStore = useWorkspaceStore();
const notif = useNotificationsStore();
const termTabs = useTerminalTabsStore();
const navigator = window.navigator;
const vFocus = { mounted: (el: HTMLElement) => el.focus() };

interface Ref { full: string; name: string; hash: string; isHead: boolean; track: string; kind: "local" | "remote" | "tag" }
interface Worktree { path: string; head: string; branch: string | null }

const refs = ref<Ref[]>([]);
const worktrees = ref<Worktree[]>([]);
const repoOpen = ref(false);

const home = (p: string) => p.replace(/^\/Users\/[^/]+/, "~");
const baseName = (p: string) => p.split("/").pop() || p;
const trackNum = (track: string, k: "ahead" | "behind") => track.match(new RegExp(`${k} (\\d+)`))?.[1];

// Same list GitPanel's picker shows: each repo followed by its worktrees.
const wsList = computed(() => wsStore.topLevel.flatMap((w) => [w, ...(wsStore.worktreesByParent[w.id] ?? [])]));
const selected = computed(() => wsStore.workspaces.find((w) => w.id === wsId.value) ?? null);
const wsByPath = computed(() => new Map(wsStore.workspaces.map((w) => [w.path, w])));
function wsLabel(w: Workspace) {
  if (!w.parent_id) return w.name;
  const parent = wsStore.workspaces.find((p) => p.id === w.parent_id);
  return `${parent?.name ?? ""} / ${w.worktree_branch || w.name}`;
}

// ── branch filter + `/` tree (collapsed folders keyed `local:JB/fix` / `origin:JB/fix`, session-only) ──
const query = ref("");
const q = computed(() => query.value.trim().toLowerCase());
const matches = (r: Ref) => !q.value || r.name.toLowerCase().includes(q.value);
const collapsed = reactive(new Set<string>());
const toggle = (k: string) => (collapsed.has(k) ? collapsed.delete(k) : collapsed.add(k));

const local = computed(() => refs.value.filter((r) => r.kind === "local"));
const localRows = computed(() => branchTreeRows(local.value.filter(matches), (b) => b.name, (k) => collapsed.has("local:" + k), !!q.value));
const remoteRows = (remote: string, branches: Ref[]) =>
  branchTreeRows(branches, (b) => b.name.slice(remote.length + 1), (k) => collapsed.has(`${remote}:${k}`), !!q.value);
const tags = computed(() => refs.value.filter((r) => r.kind === "tag").reverse());
const remoteGroups = computed(() => {
  const g: Record<string, Ref[]> = {};
  for (const r of refs.value) if (r.kind === "remote" && matches(r)) (g[r.name.split("/")[0]] ??= []).push(r);
  return g;
});
// branch -> path of the *other* worktree that has it checked out
const wtOf = computed(() => new Map(worktrees.value.filter((w) => w.branch && w.path !== git.cwd).map((w) => [w.branch!, w.path])));

async function gitOut(args: string[]) {
  const dir = git.cwd;
  const out = await invoke<{ stdout: string; code: number }>("run_git", { cwd: dir, args }).catch(() => null);
  return { dir, stdout: out?.code === 0 ? out.stdout : "" };
}

async function loadRefs() {
  if (!git.cwd) { refs.value = []; worktrees.value = []; return; }
  const [r, wt] = await Promise.all([
    gitOut([
      "for-each-ref", "--sort=refname",
      "--format=%(refname)%1f%(refname:short)%1f%(objectname)%1f%(HEAD)%1f%(upstream:track)",
      "refs/heads", "refs/remotes", "refs/tags",
    ]),
    gitOut(["worktree", "list", "--porcelain"]),
  ]);
  if (r.dir !== git.cwd) return; // workspace switched mid-load
  refs.value = r.stdout.split("\n").filter(Boolean).flatMap((l) => {
    const [full, name, hash, head, track] = l.split("\x1f");
    if (full.endsWith("/HEAD")) return []; // origin/HEAD symref
    const kind = full.startsWith("refs/heads/") ? "local" : full.startsWith("refs/tags/") ? "tag" : "remote";
    return [{ full, name, hash, isHead: head === "*", track, kind } as Ref];
  });
  worktrees.value = wt.stdout.split("\n\n").filter((b) => b.trim()).map((block) => {
    const get = (k: string) => block.split("\n").find((l) => l.startsWith(k + " "))?.slice(k.length + 1);
    return { path: get("worktree") ?? "", head: get("HEAD") ?? "", branch: get("branch")?.replace(/^refs\/heads\//, "") ?? null };
  });
}
// `log` is replaced on every store refresh, so this reloads after each git op too.
watch(() => [git.cwd, git.log, git.stashes], loadRefs, { immediate: true });

function openPath(path: string) {
  const w = wsByPath.value.get(path);
  if (w) wsId.value = w.id;
  // ponytail: worktrees Burrow doesn't know as workspaces can't be selected; add one to open it here.
}

// ── remove worktree (git deletes the directory) ──
const removing = ref<{ wt: Worktree; force: boolean; busy: boolean; error: string } | null>(null);
const askRemove = (wt: Worktree) => (removing.value = { wt, force: false, busy: false, error: "" });
async function confirmRemove() {
  const r = removing.value;
  if (!r || r.busy) return;
  r.busy = true;
  r.error = "";
  const ws = wsByPath.value.get(r.wt.path);
  const main = worktrees.value[0]?.path ?? git.cwd;
  try {
    if (ws?.parent_id) {
      // Burrow's own path: same Go call as the Sidebar's delete, drops the workspace row too.
      await wsStore.removeWorktree(ws.id, r.force);
      termTabs.clear(ws.id);
    } else {
      const out = await invoke<{ stdout: string; stderr: string; code: number }>("run_git_input", {
        cwd: main,
        args: ["-c", "status.showUntrackedFiles=normal", "worktree", "remove", r.wt.path, ...(r.force ? ["--force"] : [])],
        stdin: "",
      });
      if (out.code !== 0) throw new Error((out.stderr || out.stdout).trim());
    }
    if (r.wt.path === git.cwd) wsId.value = wsByPath.value.get(main)?.id ?? ws?.parent_id ?? null;
    removing.value = null;
    await git.refresh(true);
    await loadRefs();
  } catch (e) {
    r.error = e instanceof Error ? e.message : String(e);
    r.busy = false;
  }
}

const checkout = (name: string) => git.switchBranch(name).catch(() => git.refresh());
// `git checkout origin/x` would detach; `switch` DWIMs a tracking branch for the remote's name.
const checkoutRemote = (name: string) => op(`Checkout ${name}`, ["switch", name.slice(name.indexOf("/") + 1)]);
const deleteTag = (name: string) => op(`Delete tag ${name}`, ["tag", "-d", name]);

// The store's `op` isn't exported; same shape for the two writes it lacks.
async function op(title: string, args: string[]) {
  const out = await invoke<{ stdout: string; stderr: string; code: number }>("run_git_input", { cwd: git.cwd, args, stdin: "" })
    .catch((e: unknown) => ({ stdout: "", stderr: String(e), code: 1 }));
  if (out.code !== 0) notif.push({ type: "error", title: `${title} failed`, body: (out.stderr || out.stdout).trim(), source: "git" });
  await git.refresh(true);
}

// ── new branch (also opened by the toolbar's Branch button) ──
const newBranch = reactive({ open: false, name: "", at: "" });
const newBranchInput = ref<HTMLInputElement | null>(null);
async function startNewBranch(at = "") {
  Object.assign(newBranch, { open: true, name: "", at });
  await nextTick();
  newBranchInput.value?.focus();
}
async function confirmNewBranch() {
  const name = newBranch.name.trim();
  const at = newBranch.at;
  newBranch.open = false;
  if (!name) return;
  if (at) await git.branchAt(name, at);
  else await git.createBranch(name).catch(() => git.refresh());
}
defineExpose({ startNewBranch });

// ── small local pieces (GitPaw's Section / Row / Menu) ──
const Section = defineComponent({
  props: { title: { type: String, required: true }, count: Number, collapsed: Boolean, force: Boolean },
  setup(p, { slots }) {
    const open = ref(!p.collapsed);
    return () => h("div", { class: "mt-3" }, [
      h("div", { class: "group flex h-6 items-center gap-1 px-1" }, [
        h("button", {
          class: "flex min-w-0 flex-1 items-center gap-1.5 text-[11.5px] font-medium text-muted-foreground hover:text-secondary-foreground",
          onClick: () => (open.value = !open.value),
        }, [
          h(open.value || p.force ? PhCaretDown : PhCaretRight, { size: 12 }),
          slots.icon?.(),
          h("span", { class: "truncate" }, p.title),
          p.count !== undefined ? h("span", { class: "tabular-nums opacity-70" }, String(p.count)) : null,
        ]),
        slots.action?.(),
      ]),
      open.value || p.force ? h("div", { class: "mt-0.5" }, slots.default?.()) : null,
    ]);
  },
});

const Row = defineComponent({
  props: { active: Boolean, depth: { type: Number, default: 0 } },
  setup(p, { slots }) {
    return () => h("div", {
      style: p.depth ? { paddingLeft: `${8 + p.depth * 14}px` } : undefined,
      class: [
        "flex h-[26px] cursor-default select-none items-center gap-2 rounded-md px-2 text-[12.5px]",
        p.active ? "bg-accent/15 text-foreground" : "text-secondary-foreground hover:bg-hover hover:text-foreground",
      ],
    }, slots.default?.());
  },
});

const Folder = defineComponent({
  props: { row: { type: Object as () => Extract<BranchRow<Ref>, { kind: "folder" }>, required: true } },
  emits: ["toggle"],
  setup(p, { emit }) {
    return () => h(Row, { depth: p.row.depth, title: p.row.key, onClick: () => emit("toggle") }, () => [
      h(p.row.open ? PhCaretDown : PhCaretRight, { size: 12, class: "shrink-0 text-muted-foreground" }),
      h("span", { class: "min-w-0 flex-1 truncate" }, p.row.label),
      h("span", { class: "text-[10.5px] tabular-nums text-muted-foreground" }, String(p.row.count)),
    ]);
  },
});

const Menu = defineComponent({
  setup(_, { slots }) {
    return () => h(ContextMenuPortal, null, () => h(ContextMenuContent, {
      class: "z-50 min-w-[12rem] overflow-hidden rounded-md border border-border bg-popover p-1 text-[12.5px] text-popover-foreground shadow-md",
    }, () => slots.default?.()));
  },
});

const Item = defineComponent({
  props: { danger: Boolean, disabled: Boolean },
  emits: ["select"],
  setup(p, { slots, emit }) {
    return () => h(ContextMenuItem, {
      disabled: p.disabled,
      class: [
        "relative flex cursor-pointer select-none items-center rounded-sm px-2 py-1.5 outline-none data-[highlighted]:bg-hover data-[disabled]:pointer-events-none data-[disabled]:opacity-50",
        p.danger && "text-destructive",
      ],
      onSelect: () => emit("select"),
    }, () => slots.default?.());
  },
});

const TILE_COLORS = ["#e07a5f", "#81b29a", "#7aa5d6", "#b48ead", "#e2b65a", "#f4f1de"];
const RepoTile = defineComponent({
  props: { ws: { type: Object as () => Workspace | null, default: null }, size: { type: Number, default: 22 } },
  setup(p) {
    return () => {
      const icon = p.ws ? wsStore.icons[p.ws.parent_id ?? p.ws.id] : undefined;
      if (icon) return h("img", { src: icon, width: p.size, height: p.size, class: "shrink-0 rounded-[22%] object-cover" });
      const name = p.ws?.name ?? "G";
      let hash = 0;
      for (const ch of name) hash = (hash * 31 + ch.charCodeAt(0)) | 0;
      return h("span", {
        class: "grid shrink-0 place-items-center rounded-[26%] font-semibold text-[#1e1e24]",
        style: { width: `${p.size}px`, height: `${p.size}px`, fontSize: `${p.size * 0.52}px`, background: TILE_COLORS[Math.abs(hash) % TILE_COLORS.length] },
      }, name[0]?.toUpperCase());
    };
  },
});
</script>
