<template>
  <div class="flex h-full flex-col overflow-hidden bg-base text-xs" :data-size="props.size">

    <!-- Top bar: title + workspace selector + actions -->
    <div class="flex h-[38px] shrink-0 items-center gap-1.5 border-b border-border bg-panel px-3">
      <span class="shrink-0 text-[11px] font-semibold uppercase tracking-[0.05em] text-muted-foreground opacity-70">Git</span>

      <!-- Workspace selector -->
      <div class="flex h-6 items-center gap-1 rounded border border-border bg-[color-mix(in_srgb,var(--border)_25%,var(--bg-panel))] py-0 pl-[7px] pr-1.5">
        <PhFolder :size="11" class="shrink-0 text-muted-foreground" />
        <Select v-model="selectedWsIdModel" :options="workspaceOptions" class="h-5 min-w-0 border-0 bg-transparent px-0 py-0 font-sans text-[11px] text-secondary-foreground" />
      </div>

      <!-- Branch chip -->
      <button
        class="branch-chip flex h-6 max-w-[220px] items-center gap-1 rounded border border-border bg-transparent px-[7px] py-[3px] font-mono text-[11px] text-secondary-foreground transition-colors hover:bg-hover hover:text-foreground disabled:cursor-default disabled:opacity-40"
        :class="showBranchDropdown && 'bg-hover text-foreground'"
        @click="toggleBranchDropdown"
        :title="`Branch: ${git.branch}`"
        :disabled="!!git.error"
      >
        <PhGitBranch :size="11" class="shrink-0 text-yellow-400" style="color:var(--yellow)" />
        <span class="overflow-hidden text-ellipsis whitespace-nowrap">{{ git.branch || "—" }}</span>
        <span v-if="git.ahead > 0" class="shrink-0 text-[10px] text-success">↑{{ git.ahead }}</span>
        <span v-if="git.behind > 0" class="shrink-0 text-[10px] text-warning">↓{{ git.behind }}</span>
        <PhCaretDown :size="8" class="shrink-0 text-muted-foreground transition-transform" :class="showBranchDropdown && 'rotate-180'" />
      </button>

      <div class="flex-1" />

      <!-- Network actions -->
      <button class="flex h-6 items-center gap-1 whitespace-nowrap rounded border border-border bg-transparent px-2 font-sans text-[11px] font-medium text-muted-foreground transition-colors hover:border-accent/35 hover:bg-hover hover:text-foreground disabled:cursor-default disabled:opacity-30" :disabled="git.fetching || git.loading" @click="git.fetch()" title="Fetch">
        <PhArrowsClockwise :size="13" :class="git.fetching && 'animate-spin'" />
        Fetch
      </button>
      <button
        v-if="!git.error && git.hasUpstream && git.behind > 0"
        class="flex h-6 items-center gap-1 whitespace-nowrap rounded border border-border bg-transparent px-2 font-sans text-[11px] font-medium text-muted-foreground transition-colors hover:border-accent/35 hover:bg-hover hover:text-foreground disabled:cursor-default disabled:opacity-30"
        :disabled="git.pulling || git.pushing"
        @click="git.pull()"
        title="Pull (ff-only)"
      >
        <PhArrowDown :size="13" :class="git.pulling && 'animate-spin'" />
        Pull <span class="rounded-lg bg-accent/20 px-1 text-[9px] font-semibold text-accent">{{ git.behind }}</span>
      </button>
      <button
        v-if="!git.error"
        class="flex h-6 items-center gap-1 whitespace-nowrap rounded border border-border bg-transparent px-2 font-sans text-[11px] font-medium text-muted-foreground transition-colors hover:border-accent/35 hover:bg-hover hover:text-foreground disabled:cursor-default disabled:opacity-30"
        :disabled="git.pushing || git.loading || (git.hasUpstream && git.ahead === 0)"
        @click="git.push()"
        :title="git.hasUpstream ? 'Push' : 'Publish branch'"
      >
        <PhArrowUp :size="13" :class="git.pushing && 'animate-spin'" />
        {{ git.hasUpstream ? "Push" : "Publish" }}
        <span v-if="git.ahead > 0" class="rounded-lg bg-accent/20 px-1 text-[9px] font-semibold text-accent">{{ git.ahead }}</span>
      </button>
      <button class="flex items-center rounded p-1 text-muted-foreground transition-colors hover:bg-hover hover:text-foreground disabled:cursor-default disabled:opacity-30" :disabled="git.loading" @click="git.refresh()" title="Refresh">
        <PhArrowClockwise :size="14" :class="git.loading && 'animate-spin'" />
      </button>
      <button v-if="!isPopout" class="flex items-center rounded p-1 text-muted-foreground transition-colors hover:bg-hover hover:text-foreground disabled:cursor-default disabled:opacity-30" @click="popout()" title="Pop out to window">
        <PhArrowSquareOut :size="14" />
      </button>
    </div>

    <!-- Branch dropdown -->
    <template v-if="showBranchDropdown">
      <div class="fixed inset-0 z-[99]" @click="showBranchDropdown = false" />
      <div class="absolute left-[240px] top-[42px] z-[100] max-h-[260px] min-w-[220px] overflow-y-auto rounded-[5px] border border-border bg-panel py-[3px] shadow-[0_6px_20px_rgba(0,0,0,0.35)]">
        <div
          v-for="b in git.branches"
          :key="b"
          class="flex cursor-pointer items-center gap-[7px] px-3 py-[5px] font-mono text-xs text-secondary-foreground transition-colors hover:bg-hover hover:text-foreground"
          :class="b === git.branch && 'text-foreground'"
          @click="selectBranch(b)"
        >
          <PhCheck v-if="b === git.branch" :size="10" class="w-3 shrink-0 text-success" />
          <span v-else class="w-3 shrink-0 text-success" />
          {{ b }}
        </div>
        <div class="my-[3px] h-px bg-border" />
        <div v-if="!newBranchMode" class="flex cursor-pointer items-center gap-[7px] px-3 py-[5px] text-xs text-muted-foreground transition-colors hover:bg-hover hover:text-foreground" @click.stop="startNewBranch">
          <PhPlus :size="10" /> New branch…
        </div>
        <div v-else class="px-2.5 py-[5px]">
          <input
            ref="newBranchInputRef"
            v-model="newBranchName"
            class="box-border w-full rounded-[3px] border border-border bg-[color-mix(in_srgb,var(--border)_20%,var(--bg-panel))] px-[7px] py-1 font-mono text-xs text-foreground outline-none focus:border-accent/50"
            placeholder="branch-name"
            @keydown.enter.prevent="confirmNewBranch"
            @keydown.esc="newBranchMode = false"
          />
        </div>
      </div>
    </template>

    <!-- Push/pull progress bar -->
    <div v-if="git.pushing || git.pulling" class="flex shrink-0 items-center gap-2.5 border-b border-border px-3 py-1">
      <div class="gp-progress-bar relative h-0.5 flex-1 overflow-hidden rounded-sm bg-border" />
      <span class="text-[11px] text-muted-foreground">{{ git.pushing ? "Pushing…" : "Pulling…" }}</span>
    </div>

    <!-- No repo error -->
    <div v-if="git.error" class="flex flex-1 items-center justify-center gap-2.5 text-[13px] text-muted-foreground">
      <PhWarning :size="20" />
      <span>Not a git repository</span>
      <button class="flex items-center gap-[5px] rounded-[5px] border border-border bg-hover px-3 py-[5px] text-xs text-foreground hover:border-warning hover:bg-warning hover:text-black" :disabled="git.loading" @click="git.gitInit()">
        <PhGitBranch :size="12" /> Git Init
      </button>
    </div>

    <!-- Main two-column layout -->
    <div v-else class="flex flex-1 overflow-hidden">

      <!-- LEFT: file lists + commit -->
      <div class="flex w-[280px] shrink-0 grow-0 basis-[280px] flex-col overflow-hidden border-r border-border">

        <!-- COMMIT FILES MODE -->
        <template v-if="selectedCommit">
          <div class="flex min-w-0 shrink-0 items-center gap-1.5 border-b border-border bg-[color-mix(in_srgb,var(--border)_18%,var(--bg-panel))] py-1.5 pl-1.5 pr-2">
            <button class="flex items-center rounded p-1 text-muted-foreground transition-colors hover:bg-hover hover:text-foreground" @click="clearSelectedCommit" title="Back to changes">
              <PhArrowLeft :size="12" />
            </button>
            <span class="shrink-0 font-mono text-[10px] text-warning">{{ selectedCommit.shortHash }}</span>
            <span class="flex-1 overflow-hidden text-ellipsis whitespace-nowrap text-[11px] text-secondary-foreground" :title="selectedCommit.subject">{{ selectedCommit.subject }}</span>
            <span v-if="commitFiles.length" class="shrink-0 rounded-lg bg-accent/[0.14] px-[5px] text-[10px] font-medium text-accent">{{ commitFiles.length }}</span>
          </div>
          <div class="flex-1 overflow-y-auto py-2">
            <div v-if="commitFilesLoading" class="pt-3 px-3 text-[11px] text-muted-foreground opacity-55">Loading…</div>
            <div v-else-if="!commitFiles.length" class="pt-3 px-3 text-[11px] text-muted-foreground opacity-55">No files changed</div>
            <div
              v-for="f in commitFiles"
              :key="f.path"
              class="group flex cursor-pointer items-center gap-1.5 px-3 py-[3px] transition-colors hover:bg-hover"
              :class="commitDiff?.filePath === f.path && 'bg-accent/[0.09]'"
              @click="openCommitFileDiff(f)"
            >
              <span class="w-3 shrink-0 text-center font-mono text-[10px] font-bold" :class="commitFileStatusClass(f.status)">{{ f.status }}</span>
              <span class="flex-1 overflow-hidden text-ellipsis whitespace-nowrap font-mono text-[11px] text-secondary-foreground transition-colors group-hover:text-foreground" :class="commitDiff?.filePath === f.path && '!text-foreground'" :title="f.path">{{ f.path }}</span>
              <span class="flex shrink-0 gap-[3px] font-mono text-[10px]">
                <span v-if="f.added" class="text-success">+{{ f.added }}</span>
                <span v-if="f.deleted" class="text-destructive">-{{ f.deleted }}</span>
              </span>
            </div>
          </div>
        </template>

        <!-- NORMAL MODE: staged + changes + commit area -->
        <template v-else>
          <div class="flex-1 overflow-y-auto py-2">

            <!-- STAGED -->
            <div class="flex items-center justify-between px-3 py-[3px]">
              <span class="flex items-center gap-[5px] text-[10px] font-semibold uppercase tracking-[0.06em] text-muted-foreground opacity-70">Staged <span v-if="git.staged.length" class="rounded-lg bg-accent/[0.14] px-[5px] text-[10px] font-medium normal-case tracking-normal text-accent">{{ git.staged.length }}</span></span>
              <button v-if="git.staged.length" class="rounded-sm border border-border bg-transparent px-1.5 py-px text-[10px] text-muted-foreground transition-colors hover:bg-hover hover:text-foreground" @click="git.unstageAll()">−All</button>
            </div>
            <div v-if="git.staged.length === 0" class="px-3 pb-[5px] pt-0.5 text-[11px] text-muted-foreground opacity-55">Nothing staged</div>
            <div
              v-for="f in git.staged"
              :key="'s:' + f.path"
              class="group flex cursor-pointer items-center gap-1.5 px-3 py-[3px] transition-colors hover:bg-hover"
              :class="activeDiff?.path === f.path && activeDiff?.staged && 'bg-accent/[0.09]'"
              @click="toggleDiff(f.path, true)"
            >
              <span class="w-3 shrink-0 text-center font-mono text-[10px] font-bold text-success">{{ f.x }}</span>
              <span class="flex-1 overflow-hidden text-ellipsis whitespace-nowrap font-mono text-[11px] text-secondary-foreground transition-colors group-hover:text-foreground" :class="activeDiff?.path === f.path && activeDiff?.staged && '!text-foreground'" :title="f.path">{{ f.path }}</span>
              <button class="hidden shrink-0 rounded-sm border-0 bg-transparent p-0 px-[3px] text-[13px] leading-none text-muted-foreground transition-colors hover:text-foreground group-hover:block" @click.stop="git.unstageFile(f.path)" title="Unstage">−</button>
            </div>

            <!-- CHANGES -->
            <div class="mt-2.5 flex items-center justify-between px-3 py-[3px]">
              <span class="flex items-center gap-[5px] text-[10px] font-semibold uppercase tracking-[0.06em] text-muted-foreground opacity-70">Changes <span v-if="changesCount" class="rounded-lg bg-accent/[0.14] px-[5px] text-[10px] font-medium normal-case tracking-normal text-accent">{{ changesCount }}</span></span>
              <button v-if="changesCount" class="rounded-sm border border-border bg-transparent px-1.5 py-px text-[10px] text-muted-foreground transition-colors hover:bg-hover hover:text-foreground" @click="git.stageAll()">+All</button>
            </div>
            <div v-if="!changesCount" class="px-3 pb-[5px] pt-0.5 text-[11px] text-muted-foreground opacity-55">Working tree clean</div>
            <div
              v-for="f in git.unstaged"
              :key="'u:' + f.path"
              class="group flex cursor-pointer items-center gap-1.5 px-3 py-[3px] transition-colors hover:bg-hover"
              :class="activeDiff?.path === f.path && !activeDiff?.staged && 'bg-accent/[0.09]'"
              @click="toggleDiff(f.path, false)"
            >
              <span class="w-3 shrink-0 text-center font-mono text-[10px] font-bold text-warning">{{ f.y }}</span>
              <span class="flex-1 overflow-hidden text-ellipsis whitespace-nowrap font-mono text-[11px] text-secondary-foreground transition-colors group-hover:text-foreground" :class="activeDiff?.path === f.path && !activeDiff?.staged && '!text-foreground'" :title="f.path">{{ f.path }}</span>
              <div class="hidden items-center gap-0.5 group-hover:flex">
                <button class="rounded-sm border-0 bg-transparent p-0 px-[3px] text-[13px] leading-none text-muted-foreground transition-colors hover:text-success" @click.stop="git.stageFile(f.path)" title="Stage">+</button>
                <button class="rounded-sm border-0 bg-transparent p-0 px-[3px] text-[13px] leading-none text-muted-foreground transition-colors hover:text-destructive" @click.stop="git.discardFile(f.path)" title="Discard">✕</button>
              </div>
            </div>
            <div
              v-for="f in git.untracked"
              :key="'t:' + f.path"
              class="group flex cursor-pointer items-center gap-1.5 px-3 py-[3px] transition-colors hover:bg-hover"
            >
              <span class="w-3 shrink-0 text-center font-mono text-[10px] font-bold text-muted-foreground">?</span>
              <span class="flex-1 overflow-hidden text-ellipsis whitespace-nowrap font-mono text-[11px] text-secondary-foreground transition-colors group-hover:text-foreground" :title="f.path">{{ f.path }}</span>
              <div class="hidden items-center gap-0.5 group-hover:flex">
                <button class="rounded-sm border-0 bg-transparent p-0 px-[3px] text-[13px] leading-none text-muted-foreground transition-colors hover:text-success" @click.stop="git.stageFile(f.path)" title="Stage">+</button>
              </div>
            </div>
          </div>

          <!-- Commit area (pinned to bottom of left column) -->
          <div class="flex shrink-0 flex-col gap-1.5 border-t border-border bg-panel px-3 py-[9px]">
            <div class="relative">
              <textarea
                v-model="git.commitMsg"
                class="gp-commit-input box-border w-full resize-none rounded border border-border bg-[color-mix(in_srgb,var(--border)_15%,var(--bg-panel))] px-2 py-1.5 pr-6 font-sans text-xs leading-normal text-foreground outline-none transition-colors placeholder:text-muted-foreground placeholder:opacity-60 focus:border-accent/55"
                placeholder="Commit message…"
                rows="3"
                @keydown.ctrl.enter="git.commit()"
                @keydown.meta.enter="git.commit()"
              />
              <button
                class="absolute right-1 top-1 rounded p-[3px] text-muted-foreground transition-colors hover:bg-hover hover:text-accent disabled:cursor-default disabled:opacity-30"
                :disabled="git.staged.length === 0 || git.generating"
                title="Generate commit message from staged diff"
                @click="git.generateCommitMessage()"
              >
                <PhSparkle :size="12" :class="git.generating && 'animate-pulse'" />
              </button>
            </div>
            <div v-if="git.generateError" class="text-[10px] text-destructive">{{ git.generateError }}</div>
            <div class="flex flex-wrap gap-[3px]">
              <button
                v-for="t in COMMIT_TYPES"
                :key="t"
                class="rounded-[3px] border border-border bg-transparent px-[5px] py-0.5 font-mono text-[9.5px] text-muted-foreground transition-colors hover:bg-hover hover:text-secondary-foreground"
                :class="activeType === t && '!border-accent/45 !bg-accent/[0.14] !text-accent'"
                @click="applyType(t)"
              >{{ t }}</button>
            </div>
            <div class="flex gap-1.5">
              <button
                class="flex flex-1 items-center justify-center gap-1 rounded border border-border bg-hover px-2 py-[5px] font-sans text-[11px] font-medium text-secondary-foreground transition-colors hover:bg-[color-mix(in_srgb,var(--border)_60%,var(--bg-hover))] hover:text-foreground disabled:cursor-default disabled:opacity-30"
                :disabled="!git.commitMsg.trim() || git.staged.length === 0"
                @click="git.commit()"
                title="⌘↵"
              >
                <PhGitCommit :size="12" /> Commit
              </button>
              <button
                class="flex flex-1 items-center justify-center gap-1 rounded border border-transparent bg-accent/80 px-2 py-[5px] font-sans text-[11px] font-medium text-white transition-colors hover:bg-accent disabled:cursor-default disabled:opacity-30"
                :disabled="!git.commitMsg.trim() || git.staged.length === 0 || git.pushing"
                @click="commitAndPush()"
              >
                <PhArrowUp :size="12" /> Commit & Push
              </button>
            </div>
          </div>
        </template>
      </div>

      <!-- RIGHT: diff + history -->
      <div class="flex min-w-0 flex-1 flex-col overflow-hidden">

        <!-- Diff view: file diff -->
        <div v-if="git.diffFile" class="flex min-h-0 flex-1 flex-col overflow-hidden">
          <div class="flex shrink-0 items-center gap-2 border-b border-border bg-[color-mix(in_srgb,var(--border)_18%,var(--bg-panel))] px-3 py-1.5">
            <span class="flex-1 overflow-hidden text-ellipsis whitespace-nowrap font-mono text-[11px] text-secondary-foreground">{{ git.diffFile }}</span>
            <span class="shrink-0 text-[10px] text-muted-foreground">{{ git.diffStaged ? "staged" : "unstaged" }}</span>
            <button class="flex items-center rounded p-1 text-muted-foreground transition-colors hover:bg-hover hover:text-foreground" @click="git.clearDiff(); activeDiff = null" title="Close diff"><PhX :size="11" /></button>
          </div>
          <DiffView :diff="git.diff" :diff-key="git.diffFile" />
        </div>

        <!-- Diff view: commit diff -->
        <div v-else-if="commitDiff" class="flex min-h-0 flex-1 flex-col overflow-hidden">
          <div class="flex shrink-0 items-center gap-2 border-b border-border bg-[color-mix(in_srgb,var(--border)_18%,var(--bg-panel))] px-3 py-1.5">
            <span class="flex-1 overflow-hidden text-ellipsis whitespace-nowrap font-mono text-[11px] text-secondary-foreground">{{ commitDiff.subject }}</span>
            <button class="flex items-center rounded p-1 text-muted-foreground transition-colors hover:bg-hover hover:text-foreground" @click="commitDiff = null" title="Close diff"><PhX :size="11" /></button>
          </div>
          <DiffView :diff="commitDiff.text" :diff-key="commitDiff.hash + (commitDiff.filePath ?? '')" />
        </div>

        <div v-else class="flex flex-1 items-center justify-center gap-2 text-xs text-muted-foreground opacity-50">
          <PhArrowLeft :size="16" />
          Click a file to view diff
        </div>

        <!-- History -->
        <div class="flex max-h-[220px] shrink-0 flex-col border-t border-border bg-panel">
          <div class="flex shrink-0 cursor-pointer select-none items-center gap-[5px] px-3 py-[5px]" @click="showHistory = !showHistory">
            <PhCaretRight :size="10" class="text-muted-foreground transition-transform" :class="showHistory && 'rotate-90'" />
            <span class="flex items-center gap-[5px] text-[10px] font-semibold uppercase tracking-[0.06em] text-muted-foreground opacity-70">History</span>
            <span v-if="git.ahead > 0" class="ml-1 rounded-lg bg-accent/[0.14] px-[5px] text-[10px] font-medium normal-case tracking-normal text-accent">{{ git.ahead }} unpushed</span>
          </div>
          <div v-if="showHistory" class="flex-1 overflow-y-auto">
            <div v-if="git.log.length === 0" class="px-3 py-1.5 text-[11px] text-muted-foreground opacity-55">No commits</div>
            <div
              v-for="(c, i) in git.log"
              :key="c.hash"
              class="flex cursor-pointer items-center gap-2 px-3 py-[3px] transition-colors hover:bg-hover"
              :class="[i < git.ahead && 'bg-accent/[0.05] hover:bg-accent/10', selectedCommit?.hash === c.hash && '!bg-accent/[0.09]']"
              :title="c.subject + '\n' + c.author + (i < git.ahead ? '\n↑ Not pushed' : '')"
              @click="openCommitDiff(c)"
            >
              <span class="shrink-0 font-mono text-[10px] text-warning" :class="i < git.ahead && '!text-accent'">{{ c.shortHash }}</span>
              <span v-if="i < git.ahead" class="shrink-0 text-[9px] font-bold text-accent">↑</span>
              <span class="flex-1 overflow-hidden text-ellipsis whitespace-nowrap text-[11px] text-secondary-foreground transition-colors hover:text-foreground">{{ c.subject }}</span>
              <span class="max-w-[100px] shrink-0 overflow-hidden text-ellipsis whitespace-nowrap text-[10px] text-muted-foreground">{{ c.author }}</span>
              <span class="shrink-0 text-[10px] text-muted-foreground">{{ c.relTime }}</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick } from "vue";
import { invoke } from "@tauri-apps/api/core";
import {
  PhGitBranch, PhGitCommit, PhFolder,
  PhArrowUp, PhArrowDown, PhArrowLeft, PhArrowClockwise, PhArrowsClockwise,
  PhArrowSquareOut,
  PhCaretDown, PhCaretRight,
  PhWarning, PhX, PhCheck, PhPlus, PhSparkle,
} from "@phosphor-icons/vue";
import { useGitStore, type GitCommit } from "@/stores/git";
import { useWorkspaceStore } from "@/stores/workspace";
import DiffView from "./DiffView.vue";
import { Select } from "@/components/ui/select";

// "lg" is the full-screen Git dialog; the default is the narrow right panel.
const props = withDefaults(defineProps<{ size?: "sm" | "lg" }>(), { size: "sm" });

const git = useGitStore();
const wsStore = useWorkspaceStore();
const isPopout = (window as any).__TAURI_INTERNALS__?.metadata?.currentWindow?.label === "gitpanel";

async function popout() {
  await invoke("open_git_panel_window");
}
const selectedWsId = ref<number | null>(wsStore.active?.id ?? null);
const selectedWsIdModel = computed<string | undefined>({ get: () => selectedWsId.value?.toString(), set: (id) => { selectedWsId.value = id ? Number(id) : null; } });
const workspaceOptions = computed(() => wsStore.topLevel.map((workspace) => ({ value: String(workspace.id), label: workspace.name })));
const showBranchDropdown = ref(false);
const newBranchMode = ref(false);
const newBranchName = ref("");
const newBranchInputRef = ref<HTMLInputElement | null>(null);
const showHistory = ref(true);
const activeDiff = ref<{ path: string; staged: boolean } | null>(null);
const commitDiff = ref<{ hash: string; subject: string; text: string; filePath?: string } | null>(null);

interface CommitFile { path: string; status: string; added: number; deleted: number }
const selectedCommit = ref<GitCommit | null>(null);
const commitFiles = ref<CommitFile[]>([]);
const commitFilesLoading = ref(false);

const COMMIT_TYPES = ["feat", "fix", "docs", "chore", "refactor", "test", "style"] as const;

const changesCount = computed(() => git.unstaged.length + git.untracked.length);

const activeType = computed(() => {
  const m = git.commitMsg.match(/^([a-z]+)(\([^)]+\))?:\s/);
  return m ? m[1] : null;
});

// When workspace selection changes, point git store at that workspace
watch(selectedWsId, (id) => {
  const w = wsStore.topLevel.find((w) => w.id === id);
  if (w) git.setCwd(w.path);
}, { immediate: true });

// When active workspace changes externally, follow it
watch(() => wsStore.active?.id, (id) => {
  if (id != null) selectedWsId.value = id;
});

function toggleBranchDropdown() {
  showBranchDropdown.value = !showBranchDropdown.value;
  if (showBranchDropdown.value) newBranchMode.value = false;
}

async function selectBranch(name: string) {
  showBranchDropdown.value = false;
  if (name === git.branch) return;
  await git.switchBranch(name);
}

async function startNewBranch() {
  newBranchMode.value = true;
  newBranchName.value = "";
  await nextTick();
  newBranchInputRef.value?.focus();
}

async function confirmNewBranch() {
  const name = newBranchName.value.trim();
  if (!name) return;
  newBranchMode.value = false;
  showBranchDropdown.value = false;
  await git.createBranch(name);
}

function toggleDiff(path: string, staged: boolean) {
  commitDiff.value = null;
  if (activeDiff.value?.path === path && activeDiff.value?.staged === staged) {
    activeDiff.value = null;
    git.clearDiff();
  } else {
    activeDiff.value = { path, staged };
    git.showDiff(path, staged);
  }
}

function applyType(t: string) {
  const current = git.commitMsg;
  const typePrefix = /^[a-z]+(\([^)]+\))?:\s/;
  if (typePrefix.test(current)) {
    git.commitMsg = current.replace(typePrefix, `${t}: `);
  } else {
    git.commitMsg = `${t}: ${current}`;
  }
}

async function commitAndPush() {
  await git.commit();
  await git.push();
}

async function openCommitDiff(c: GitCommit) {
  const w = wsStore.topLevel.find((w) => w.id === selectedWsId.value);
  if (!w) return;

  if (selectedCommit.value?.hash === c.hash) {
    selectedCommit.value = null;
    commitFiles.value = [];
    commitDiff.value = null;
    return;
  }

  selectedCommit.value = c;
  commitDiff.value = null;
  activeDiff.value = null;
  git.clearDiff();
  commitFilesLoading.value = true;
  try {
    const [nsOut, numOut] = await Promise.all([
      invoke<{ stdout: string; code: number }>("run_git", {
        cwd: w.path, args: ["show", "--name-status", "--format=", c.hash],
      }),
      invoke<{ stdout: string; code: number }>("run_git", {
        cwd: w.path, args: ["show", "--numstat", "--format=", c.hash],
      }),
    ]);
    const nsLines = nsOut.stdout.split("\n").filter((l) => l.trim() && l.includes("\t"));
    const numLines = numOut.stdout.split("\n").filter((l) => l.trim() && l.includes("\t"));
    commitFiles.value = nsLines.map((line, i) => {
      const nsParts = line.split("\t");
      const status = nsParts[0][0];
      const path = nsParts[nsParts.length - 1];
      const numLine = numLines[i] || "";
      const numParts = numLine.split("\t");
      const added = parseInt(numParts[0]) || 0;
      const deleted = parseInt(numParts[1]) || 0;
      return { path, status, added, deleted };
    });
  } finally {
    commitFilesLoading.value = false;
  }
}

async function openCommitFileDiff(f: CommitFile) {
  if (!selectedCommit.value) return;
  const w = wsStore.topLevel.find((w) => w.id === selectedWsId.value);
  if (!w) return;
  if (commitDiff.value?.filePath === f.path) { commitDiff.value = null; return; }
  const out = await invoke<{ stdout: string; code: number }>("run_git", {
    cwd: w.path, args: ["show", selectedCommit.value.hash, "--", f.path],
  });
  commitDiff.value = { hash: selectedCommit.value.hash, subject: f.path, text: out.stdout, filePath: f.path };
}

function clearSelectedCommit() {
  selectedCommit.value = null;
  commitFiles.value = [];
  commitDiff.value = null;
}

function commitFileStatusClass(s: string) {
  if (s === "A") return "cf-added";
  if (s === "D") return "cf-deleted";
  if (s === "R" || s === "C") return "cf-renamed";
  return "cf-modified";
}
</script>

<style>
/* Global resets — needed when GitPanel is mounted as the root component in the
   popout window (where App.vue's :root block is not loaded). Harmless when
   embedded in the main app since the values are identical. */
:root {
  --bg-base: #0d0d0d;
  --bg-panel: #111111;
  --bg-hover: #1a1a1a;
  --border: #2a2a2a;
  --text-primary: #e2e8f0;
  --text-secondary: #94a3b8;
  --text-muted: #64748b;
  --accent: #3b82f6;
  --green: #22c55e;
  --yellow: #eab308;
  --red: #ef4444;
  --font-mono: "JetBrains Mono", "Fira Code", "Cascadia Code", monospace;
  --font-ui: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
}
body {
  background-color: var(--bg-base);
  color: var(--text-primary);
  font-family: var(--font-ui);
  margin: 0;
  overflow: hidden;
  user-select: none;
  -webkit-font-smoothing: antialiased;
}
</style>

<style scoped>
/* Every type size in this panel is a hard-coded px utility, tuned for the ~300px
   right panel. `size="lg"` (the full-screen Git dialog) re-maps those same
   utilities one step up instead of duplicating 80 class attributes. */
[data-size="lg"] { font-size: 13px; }
[data-size="lg"] :deep(:is(.text-\[9px\], .text-\[9\.5px\])) { font-size: 11px; }
[data-size="lg"] :deep(.text-\[10px\]) { font-size: 12px; }
[data-size="lg"] :deep(:is(.text-\[11px\], .text-\[11\.5px\])) { font-size: 13px; }
[data-size="lg"] :deep(.text-\[13px\]) { font-size: 15px; }
[data-size="lg"] :deep(.text-xs) { font-size: 13px; }

.gp-progress-bar::after {
  content: "";
  position: absolute;
  inset-block: 0;
  left: -40%;
  width: 40%;
  background: var(--accent);
  animation: progress-slide 1.1s ease-in-out infinite;
}
@keyframes progress-slide { to { left: 100%; } }

.gp-commit-input::-webkit-scrollbar { display: none; }

.cf-added    { color: var(--green); }
.cf-deleted  { color: var(--red); }
.cf-renamed  { color: var(--accent); }
.cf-modified { color: var(--yellow); }
</style>
