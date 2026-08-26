<template>
  <aside
    ref="panelEl"
    class="flex w-[var(--right-panel-width,300px)] shrink-0 grow-0 basis-[var(--right-panel-width,300px)] flex-col overflow-hidden border-l border-border bg-panel text-xs [backdrop-filter:var(--blur-panels,none)]"
  >
    <!-- Tab bar -->
    <div class="flex h-8 shrink-0 border-b border-border">
      <button
        v-for="tab in tabs"
        :key="tab.id"
        class="flex flex-1 items-center justify-center gap-1 border-0 border-b-2 border-b-transparent bg-transparent px-1 font-sans text-[11px] text-muted-foreground transition-colors hover:text-secondary-foreground"
        :class="activeTab === tab.id && 'border-b-accent bg-accent/5 text-foreground'"
        @click="activeTab = tab.id"
      >
        <component :is="tab.icon" :size="12" />
        <span v-if="cq.is.sm">{{ tab.label }}</span>
      </button>
    </div>

    <!-- Explorer tab -->
    <div v-if="activeTab === 'explorer'" class="flex flex-1 flex-col overflow-y-auto">
      <div v-if="!props.cwd" class="p-4 text-center text-[11px] text-muted-foreground">No workspace open</div>
      <div v-else-if="fileTree.rootError" class="p-4 text-center text-[11px] text-destructive">{{ fileTree.rootError }}</div>
      <div v-else class="flex-1 py-1">
        <FileTreeNode v-for="node in fileTree.tree" :key="node.id" :node="node" :depth="0" />
      </div>
    </div>

    <!-- Git tab -->
    <div v-else-if="activeTab === 'git'" class="flex flex-1 flex-col overflow-hidden overflow-y-auto">
      <!-- Header -->
      <div class="flex shrink-0 items-center justify-between border-b border-border px-2 py-[5px]">
        <div class="flex items-center gap-1 font-mono text-[11px] text-secondary-foreground">
          <PhGitBranch :size="12" class="shrink-0 text-yellow-400" style="color: var(--yellow);" />
          <span>{{ git.branch || "—" }}</span>
          <span v-if="git.ahead > 0" class="text-[10px] text-success" title="Commits ahead of upstream">↑{{ git.ahead }}</span>
          <span v-if="git.behind > 0" class="text-[10px] text-warning" title="Commits behind upstream">↓{{ git.behind }}</span>
        </div>
        <div class="flex items-center gap-[3px]">
          <button
            v-if="!git.error && git.hasUpstream && git.behind > 0"
            class="flex items-center gap-[3px] rounded border border-border bg-transparent px-[7px] py-0.5 font-sans text-[10px] font-medium text-secondary-foreground transition-colors hover:border-accent/40 hover:bg-hover hover:text-foreground disabled:cursor-default disabled:opacity-35"
            :disabled="git.pulling || git.pushing || git.loading"
            @click="git.pull()"
            title="git pull --ff-only"
          >
            <PhArrowDown :size="11" :class="git.pulling && 'animate-spin'" />
            Pull
            <span>({{ git.behind }})</span>
          </button>
          <button
            v-if="!git.error"
            class="flex items-center gap-[3px] rounded border border-border bg-transparent px-[7px] py-0.5 font-sans text-[10px] font-medium text-secondary-foreground transition-colors hover:border-accent/40 hover:bg-hover hover:text-foreground disabled:cursor-default disabled:opacity-35"
            :disabled="git.pushing || git.loading || (git.hasUpstream && git.ahead === 0)"
            @click="git.push()"
            :title="git.hasUpstream ? 'git push' : 'git push -u origin ' + git.branch"
          >
            <PhArrowUp :size="11" :class="git.pushing && 'animate-spin'" />
            {{ git.hasUpstream ? "Push" : "Publish" }}
            <span v-if="git.ahead > 0">({{ git.ahead }})</span>
          </button>
          <AutoRefreshButton
            :current-interval="ar.currentInterval.value"
            :is-running="ar.isRunning.value"
            :next-refresh-in="ar.nextRefreshIn.value"
            :toggle="ar.toggle"
            :set-refresh-interval="ar.setRefreshInterval"
          />
          <button
            class="flex items-center rounded p-[3px] text-muted-foreground transition-colors hover:bg-hover hover:text-foreground disabled:cursor-default disabled:opacity-35"
            :disabled="git.loading"
            @click="git.refresh()"
            title="Refresh"
          >
            <PhArrowClockwise :size="13" :class="git.loading && 'animate-spin'" />
          </button>
        </div>
      </div>

      <!-- Push/pull loader -->
      <div v-if="git.pushing || git.pulling" class="flex shrink-0 items-center gap-2 border-b border-border px-2 py-1">
        <div class="relative h-0.5 flex-1 overflow-hidden rounded-full bg-border">
          <div class="git-progress-bar" />
        </div>
        <span class="text-[10px] text-muted-foreground">{{ git.pushing ? "Pushing…" : "Pulling…" }}</span>
      </div>

      <div class="flex flex-1 flex-col overflow-y-auto py-1.5">
        <!-- Error -->
        <div v-if="git.error" class="flex flex-wrap items-center gap-1.5 px-2.5 py-4 text-[11px] text-secondary-foreground">
          <PhWarning :size="13" />
          Not a git repository
          <button
            class="ml-auto flex items-center gap-1 rounded border border-border bg-hover px-2 py-[3px] text-[11px] text-foreground hover:border-warning hover:bg-warning hover:text-black disabled:cursor-default disabled:opacity-35"
            :disabled="git.loading"
            @click="git.gitInit()"
          >
            <PhGitBranch :size="12" />
            Git Init
          </button>
        </div>

        <template v-else>
          <!-- Staged -->
          <div class="flex items-center justify-between px-2 pb-[3px] pt-1 text-[10px] font-semibold uppercase tracking-[0.06em] text-muted-foreground opacity-65">
            Staged
            <button
              v-if="git.staged.length > 0"
              class="flex items-center gap-0.5 rounded border border-border bg-transparent px-[5px] py-px text-[10px] font-medium normal-case tracking-normal text-muted-foreground opacity-80 transition-colors hover:bg-hover hover:text-foreground hover:opacity-100"
              @click="openAllDiffInTab(true)"
              title="Open all staged diffs in new tab"
            ><PhArrowUpRight :size="10" /> View</button>
          </div>
          <div v-if="git.staged.length === 0" class="px-2 pb-1.5 pt-0.5 text-[11px] text-muted-foreground opacity-60">Nothing staged</div>
          <div
            v-for="f in git.staged"
            :key="'s:' + f.path"
            class="group mx-[3px] flex cursor-pointer items-center gap-[5px] rounded px-2 py-0.5 transition-colors hover:bg-hover"
            @click="git.showDiff(f.path, true)"
          >
            <span class="w-[11px] shrink-0 text-center font-mono text-[10px] font-bold text-success">{{ f.x }}</span>
            <span class="flex-1 overflow-hidden text-ellipsis whitespace-nowrap font-mono text-[11px] text-secondary-foreground transition-colors group-hover:text-foreground" :title="f.path">{{ f.path }}</span>
            <button class="hidden shrink-0 border-0 bg-transparent p-0 px-0.5 text-[13px] leading-none text-muted-foreground hover:text-foreground group-hover:block" @click.stop="git.unstageFile(f.path)" title="Unstage">−</button>
          </div>

          <!-- Unstaged + untracked -->
          <div class="mt-2 flex items-center justify-between px-2 pb-[3px] pt-1 text-[10px] font-semibold uppercase tracking-[0.06em] text-muted-foreground opacity-65">
            Changes
            <div class="flex items-center gap-[3px]">
              <button
                v-if="git.unstaged.length > 0"
                class="flex items-center gap-0.5 rounded border border-border bg-transparent px-[5px] py-px text-[10px] font-medium normal-case tracking-normal text-muted-foreground opacity-80 transition-colors hover:bg-hover hover:text-foreground hover:opacity-100"
                @click="openAllDiffInTab(false)"
                title="Open all unstaged diffs in new tab"
              ><PhArrowUpRight :size="10" /> View</button>
              <button
                v-if="git.unstaged.length > 0 || git.untracked.length > 0"
                class="rounded border border-border bg-transparent px-[5px] py-px text-[10px] font-medium normal-case tracking-normal text-muted-foreground opacity-80 transition-colors hover:bg-hover hover:text-foreground hover:opacity-100 disabled:cursor-default disabled:opacity-30"
                :disabled="git.loading"
                @click="git.stageAll()"
                title="Stage all"
              >+ All</button>
            </div>
          </div>
          <div v-if="git.unstaged.length === 0 && git.untracked.length === 0" class="px-2 pb-1.5 pt-0.5 text-[11px] text-muted-foreground opacity-60">
            Working tree clean
          </div>
          <div
            v-for="f in git.unstaged"
            :key="'u:' + f.path"
            class="group mx-[3px] flex cursor-pointer items-center gap-[5px] rounded px-2 py-0.5 transition-colors hover:bg-hover"
            @click="git.showDiff(f.path, false)"
          >
            <span class="w-[11px] shrink-0 text-center font-mono text-[10px] font-bold text-warning">{{ f.y }}</span>
            <span class="flex-1 overflow-hidden text-ellipsis whitespace-nowrap font-mono text-[11px] text-secondary-foreground transition-colors group-hover:text-foreground" :title="f.path">{{ f.path }}</span>
            <button class="hidden shrink-0 border-0 bg-transparent p-0 px-0.5 text-[13px] leading-none text-muted-foreground hover:text-success group-hover:block" @click.stop="git.stageFile(f.path)" title="Stage">+</button>
          </div>
          <div
            v-for="f in git.untracked"
            :key="'t:' + f.path"
            class="group mx-[3px] flex cursor-pointer items-center gap-[5px] rounded px-2 py-0.5 transition-colors hover:bg-hover"
          >
            <span class="w-[11px] shrink-0 text-center font-mono text-[10px] font-bold text-muted-foreground">?</span>
            <span class="flex-1 overflow-hidden text-ellipsis whitespace-nowrap font-mono text-[11px] text-secondary-foreground transition-colors group-hover:text-foreground" :title="f.path">{{ f.path }}</span>
            <button class="hidden shrink-0 border-0 bg-transparent p-0 px-0.5 text-[13px] leading-none text-muted-foreground hover:text-success group-hover:block" @click.stop="git.stageFile(f.path)" title="Stage">+</button>
          </div>

          <!-- Commit -->
          <div class="mt-1.5 flex shrink-0 flex-col gap-[5px] border-t border-border p-2">
            <textarea
              v-model="git.commitMsg"
              class="commit-input box-border w-full min-h-[52px] max-h-[100px] resize-none rounded border border-border bg-[color-mix(in_srgb,var(--border)_15%,var(--bg-panel))] px-2 py-1.5 font-sans text-[11px] leading-normal text-foreground outline-none transition-colors placeholder:text-muted-foreground placeholder:opacity-60 focus:border-accent/60"
              placeholder="Commit message…"
              rows="3"
              @keydown.ctrl.enter="git.commit()"
              @keydown.meta.enter="git.commit()"
            />
            <button
              class="flex w-full items-center justify-center gap-[5px] rounded border-0 bg-accent/85 px-2.5 py-[5px] font-sans text-[11px] font-semibold text-white transition-colors hover:bg-accent disabled:cursor-default disabled:opacity-35"
              :disabled="!git.commitMsg.trim() || git.staged.length === 0"
              @click="git.commit()"
            >
              <PhGitCommit :size="12" />
              Commit
            </button>
          </div>

          <!-- Diff (hidden when panel is too narrow — use cq.is.md = ≥320px) -->
          <div v-if="git.diffFile && cq.is.md" class="flex max-h-[220px] shrink-0 flex-col border-t border-border">
            <div class="flex shrink-0 items-center gap-1.5 bg-[color-mix(in_srgb,var(--border)_20%,var(--bg-panel))] px-2 py-1">
              <span class="flex-1 overflow-hidden text-ellipsis whitespace-nowrap font-mono text-[10px] text-secondary-foreground">{{ git.diffFile }}</span>
              <span class="shrink-0 text-[10px] text-muted-foreground">{{ git.diffStaged ? "staged" : "unstaged" }}</span>
              <button class="flex items-center rounded p-[3px] text-muted-foreground transition-colors hover:bg-hover hover:text-foreground" @click="git.clearDiff()" title="Close">
                <PhX :size="11" />
              </button>
            </div>
            <pre class="m-0 flex-1 overflow-auto whitespace-pre px-0 py-[5px] font-mono text-[10px] leading-normal"><span
              v-for="(line, i) in git.diff.split('\n')"
              :key="i"
              class="block"
              :class="diffLineClass(line)"
            >{{ line }}
</span></pre>
          </div>

          <!-- History -->
          <div class="mt-1.5 shrink-0 border-t border-border pt-1">
            <div
              class="flex cursor-pointer select-none items-center justify-between px-2 pb-[3px] pt-1 text-[10px] font-semibold uppercase tracking-[0.06em] text-muted-foreground opacity-65"
              @click="showHistory = !showHistory"
            >
              <span class="flex items-center gap-1"><PhCaretRight :size="9" class="transition-transform" :class="showHistory && 'rotate-90'" /> History</span>
            </div>
            <template v-if="showHistory">
              <div v-if="git.log.length === 0" class="px-2 pb-1.5 pt-0.5 text-[11px] text-muted-foreground opacity-60">No commits</div>
              <div
                v-for="(c, i) in git.log"
                :key="c.hash"
                class="mx-[3px] flex cursor-pointer items-center gap-[5px] rounded px-2 py-0.5 transition-colors hover:bg-hover"
                :class="i < git.ahead && 'bg-accent/[0.06] hover:bg-accent/[0.12]'"
                :title="c.subject + '\n' + c.author + (i < git.ahead ? '\n↑ Not pushed' : '')"
                @click="openCommitDiff(c)"
              >
                <span class="shrink-0 font-mono text-[10px] text-warning" :class="i < git.ahead && 'text-accent'">{{ c.shortHash }}</span>
                <span v-if="i < git.ahead" class="shrink-0 text-[9px] font-bold text-accent" title="Not pushed">↑</span>
                <span class="flex-1 overflow-hidden text-ellipsis whitespace-nowrap text-[11px] text-secondary-foreground transition-colors hover:text-foreground">{{ c.subject }}</span>
                <span class="shrink-0 text-[10px] text-muted-foreground">{{ c.relTime }}</span>
              </div>
            </template>
          </div>
        </template>
      </div>
    </div>

    <PullRequestsPanel v-else-if="activeTab === 'pull-requests'" :cwd="props.cwd" />
  </aside>
</template>

<script setup lang="ts">
import { ref, computed, watch, inject, onMounted, onBeforeUnmount } from "vue";
import { invoke } from "@tauri-apps/api/core";
import {
  PhFiles, PhGitBranch, PhGitCommit,
  PhArrowClockwise, PhWarning, PhX, PhArrowUpRight,
  PhArrowUp, PhArrowDown, PhCaretRight,
} from "@phosphor-icons/vue";
import { useGitStore, type GitCommit } from "@/stores/git";
import { useFileTreeStore } from "@/stores/fileTree";
import FileTreeNode from "./FileTreeNode.vue";
import { useAutoRefresh } from "@/composables/useAutoRefresh";
import { useContainerQuery } from "@/composables/useContainerQuery";
import AutoRefreshButton from "./AutoRefreshButton.vue";
import PullRequestsPanel from "./PullRequestsPanel.vue";

const props = withDefaults(defineProps<{ cwd: string; isGit?: boolean }>(), { isGit: true });
const git = useGitStore();
const fileTree = useFileTreeStore();
const activeTab = ref("git");
const showHistory = ref(false);
const activeTerm = inject<() => any>('activeTerm', () => undefined);

const panelEl = ref<HTMLElement | null>(null);
// sm=220: show tab labels; md=320: show inline diff; lg=440: not used yet
const cq = useContainerQuery(panelEl, { sm: 220, md: 320, lg: 440 });

async function openAllDiffInTab(staged: boolean) {
  const diff = await git.fetchAllDiff(staged);
  if (!diff) return;
  activeTerm()?.openDiffInTab(staged ? "Staged changes" : "Unstaged changes", staged, diff);
}

async function openCommitDiff(c: GitCommit) {
  const out = await invoke<{ stdout: string; stderr: string; code: number }>("run_git", {
    cwd: props.cwd,
    args: ["show", c.hash],
  });
  if (out.code !== 0 || !out.stdout) return;
  activeTerm()?.openDiffInTab(`${c.shortHash} ${c.subject}`, false, out.stdout);
}

// Non-git folders are first-class workspaces but expose no Git tab.
const tabs = computed(() => {
  const all = [
    { id: "git",      label: "Git",      icon: PhGitBranch },
    { id: "pull-requests", label: "PRs", icon: PhGitBranch },
    { id: "explorer", label: "Explorer", icon: PhFiles },
  ];
  return props.isGit ? all : all.filter((t) => t.id !== "git");
});

// Keep the active tab valid: a non-git workspace can't sit on the hidden Git tab.
watch(() => props.isGit, (isGit) => {
  if (!isGit && activeTab.value === "git") activeTab.value = "explorer";
}, { immediate: true });

watch(() => props.cwd, (p) => {
  if (p) {
    git.setCwd(p);
    fileTree.loadRoot(p);
  } else {
    fileTree.clearTree();
  }
}, { immediate: true });

function diffLineClass(line: string) {
  if (line.startsWith("+") && !line.startsWith("+++")) return "diff-add";
  if (line.startsWith("-") && !line.startsWith("---")) return "diff-del";
  if (line.startsWith("@@")) return "diff-hunk";
  return "diff-ctx";
}

// --- Auto-refresh: window focus + configurable interval ---
function autoRefresh() {
  if (activeTab.value === "git" && props.cwd && !document.hidden) {
    git.refresh(true);
  }
}

const ar = useAutoRefresh(autoRefresh, "burrow-git-refresh-interval");

function onFocus() { autoRefresh(); }
function onVisible() { if (!document.hidden) autoRefresh(); }

watch(activeTab, (t) => { if (t === "git") autoRefresh(); });

onMounted(() => {
  window.addEventListener("focus", onFocus);
  document.addEventListener("visibilitychange", onVisible);
});

onBeforeUnmount(() => {
  window.removeEventListener("focus", onFocus);
  document.removeEventListener("visibilitychange", onVisible);
});
</script>

<style scoped>
.git-progress-bar::after {
  content: "";
  position: absolute;
  top: 0; left: 0;
  height: 100%;
  width: 40%;
  border-radius: 2px;
  background: var(--accent);
  animation: git-indeterminate 1.1s ease-in-out infinite;
}
@keyframes git-indeterminate {
  0%   { left: -40%; }
  100% { left: 100%; }
}

.commit-input::-webkit-scrollbar { display: none; }
</style>
