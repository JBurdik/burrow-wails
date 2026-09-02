<template>
  <aside
    ref="panelEl"
    class="flex h-full shrink-0 grow-0 overflow-hidden bg-panel text-xs [backdrop-filter:var(--blur-panels,none)]"
    :class="props.open ? 'w-[var(--right-panel-width,300px)] basis-[var(--right-panel-width,300px)] border-l border-border' : 'w-0 basis-0'"
  >
    <div v-if="props.open" class="flex min-w-0 flex-1 flex-col overflow-hidden">
      <div class="flex h-10 shrink-0 items-center gap-1 overflow-x-auto border-b border-border px-2 hide-scrollbar">
        <button
          v-for="tab in openedTabs"
          :key="tab.id"
          class="group flex h-7 shrink-0 items-center gap-1.5 rounded-md px-2 text-[11px] text-muted-foreground transition-colors hover:bg-hover hover:text-secondary-foreground"
          :class="activeTab === tab.id && 'bg-accent/15 text-foreground'"
          @click="activeTab = tab.id"
        >
          <span class="relative flex h-[13px] w-[13px] shrink-0 items-center justify-center">
            <component :is="tab.icon" :size="13" class="group-hover:opacity-0" />
            <span class="absolute inset-0 hidden items-center justify-center rounded text-muted-foreground hover:bg-black/10 hover:text-foreground group-hover:flex" role="button" :aria-label="`Close ${tab.label}`" @click.stop="closeSurface(tab.id)"><PhX :size="12" /></span>
          </span>
          <span class="font-medium">{{ tab.label }}</span>
        </button>
        <button class="flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-muted-foreground hover:bg-hover hover:text-foreground" title="Open a surface" aria-label="Open a surface" @click="showSurfacePicker"><PhPlus :size="15" /></button>
      </div>

    <div v-if="!activeTab" class="flex flex-1 items-center justify-center overflow-y-auto p-5">
      <div class="w-full max-w-[420px]">
        <div class="mb-5 text-center">
          <h2 class="m-0 text-sm font-semibold text-foreground">Open a surface</h2>
          <p class="mt-1 text-[11px] text-muted-foreground">Choose what to show in the right panel.</p>
        </div>
        <div class="grid gap-2" :class="cq.is.md ? 'grid-cols-2' : 'grid-cols-1'">
          <button
            v-for="tab in tabs"
            :key="tab.id"
            class="group flex min-h-[66px] items-start gap-3 rounded-lg border border-border/70 bg-transparent px-3 py-3 text-left transition-colors hover:border-accent/45 hover:bg-hover focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
            @click="openSurface(tab.id)"
          >
            <component :is="tab.icon" :size="16" class="mt-0.5 shrink-0 text-secondary-foreground group-hover:text-accent" />
            <span class="flex min-w-0 flex-1 flex-col gap-0.5">
              <span class="text-xs font-semibold text-foreground">{{ tab.label }}</span>
              <span class="text-[10.5px] leading-relaxed text-muted-foreground">{{ tab.description }}</span>
            </span>
          </button>
        </div>
      </div>
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
            class="flex items-center rounded p-[3px] text-muted-foreground transition-colors hover:bg-hover hover:text-foreground"
            @click="activeTerm()?.openGitTab()"
            title="Open the full git manager as a tab"
          >
            <PhArrowsOutSimple :size="13" />
          </button>
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
            <div class="flex gap-[5px]">
              <button
                class="flex flex-1 items-center justify-center gap-[5px] rounded border-0 bg-accent/85 px-2.5 py-[5px] font-sans text-[11px] font-semibold text-white transition-colors hover:bg-accent disabled:cursor-default disabled:opacity-35"
                :disabled="!git.commitMsg.trim() || git.staged.length === 0"
                @click="git.commit()"
              >
                <PhGitCommit :size="12" />
                Commit
              </button>
              <CommitPushMenu />
            </div>
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
            <DiffView :diff="git.diff" :diff-key="git.diffFile" />
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

    <div v-else-if="activeTab === 'diff'" class="flex min-h-0 flex-1 flex-col overflow-hidden">
      <div class="flex shrink-0 items-center justify-between border-b border-border px-2 py-1.5">
        <div class="flex items-center gap-1.5">
          <PhGitCommit :size="13" class="text-secondary-foreground" />
          <span class="text-[11px] font-semibold text-secondary-foreground">Workspace diff</span>
        </div>
        <button class="rounded p-1 text-muted-foreground hover:bg-hover hover:text-foreground" title="Refresh diff" :disabled="workspaceDiffLoading" @click="loadWorkspaceDiff"><PhArrowClockwise :size="12" :class="workspaceDiffLoading && 'animate-spin'" /></button>
      </div>
      <div v-if="workspaceDiffLoading" class="p-4 text-center text-[11px] text-muted-foreground">Loading changes…</div>
      <div v-else-if="!workspaceDiff" class="p-4 text-center text-[11px] leading-relaxed text-muted-foreground">No unstaged or staged changes.</div>
      <DiffView v-else :diff="workspaceDiff" diff-key="workspace" />
    </div>

    <ManagerPanel
      v-else-if="activeTab === 'manager' && props.workspaceId"
      :cwd="props.cwd"
      :ws-id="props.workspaceId"
    />
    <div v-else-if="activeTab === 'manager'" class="flex flex-1 items-center justify-center p-6 text-center text-[11px] leading-relaxed text-muted-foreground">
      Open a project to start a Manager thread.
    </div>

    <!-- History tab: pre-turn worktree snapshots, newest first -->
    <div v-else-if="activeTab === 'history'" class="flex flex-1 flex-col overflow-y-auto">
      <div class="flex shrink-0 items-center gap-1.5 border-b border-border px-2 py-1.5">
        <span class="flex-1 text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">Checkpoints</span>
        <button class="rounded p-1 text-muted-foreground hover:bg-hover hover:text-foreground" title="Refresh" @click="loadCheckpoints">
          <PhArrowClockwise :size="12" />
        </button>
      </div>

      <div v-if="!checkpoints.length" class="m-2 rounded-lg border border-dashed border-border/60 px-4 py-6 text-center text-[11px] leading-[1.7] text-muted-foreground">
        No checkpoints yet.<br />One is taken before every agent turn.
      </div>

      <div
        v-for="cp in checkpoints"
        :key="cp.id"
        class="group/cp flex cursor-pointer items-center gap-1.5 border-b border-border/40 px-2 py-[6px] transition-colors hover:bg-hover"
        @click="openCheckpointDiff(cp)"
      >
        <PhClockCounterClockwise :size="11" class="shrink-0 text-muted-foreground" />
        <div class="flex min-w-0 flex-1 flex-col">
          <span class="overflow-hidden text-ellipsis whitespace-nowrap text-[11.5px] text-secondary-foreground">{{ cp.label || "Checkpoint" }}</span>
          <span class="font-mono text-[9.5px] text-muted-foreground">{{ cpTime(cp.createdAt) }} · {{ cp.commit.slice(0, 7) }}</span>
        </div>
        <button
          class="shrink-0 rounded p-1 text-muted-foreground opacity-0 transition-opacity hover:bg-hover hover:text-foreground group-hover/cp:opacity-100"
          title="Restore the working tree to this checkpoint"
          @click.stop="restoreTarget = cp"
        >
          <PhArrowUUpLeft :size="12" />
        </button>
      </div>
    </div>
    <!-- Browser: outside the v-if chain and kept mounted while its tab is open, so
         switching to Changes and back doesn't reload the page being previewed. -->
    <BrowserPane v-if="browserOpened" v-show="activeTab === 'browser'" class="min-h-0 flex-1" />

    <!-- Terminal: same kept-mounted treatment so the shell survives switching surfaces. -->
    <XTerm v-if="terminalOpened && props.cwd" v-show="activeTab === 'terminal'" :pty-id="terminalPtyId" :cwd="props.cwd" class="min-h-0 flex-1" />
    <div v-else-if="activeTab === 'terminal'" class="p-4 text-center text-[11px] text-muted-foreground">No workspace open</div>

    <!-- Restore confirm — overwrites files on disk, so it always asks first -->
    <Teleport to="body">
      <div class="fixed inset-0 z-[100] flex items-center justify-center bg-black/60" v-if="restoreTarget" @click.self="restoreTarget = null">
        <div class="flex w-[430px] flex-col gap-3 rounded-[10px] border border-border bg-panel p-6">
          <h3 class="text-sm font-semibold text-foreground">Restore checkpoint “{{ restoreTarget!.label || restoreTarget!.commit.slice(0, 7) }}”?</h3>
          <p class="text-[11.5px] leading-[1.7] text-secondary-foreground">
            Every file in this workspace goes back to how it looked at
            <strong>{{ cpTime(restoreTarget!.createdAt) }}</strong> — later edits are overwritten and files
            created since are deleted. Your commits and the staging area are untouched.
          </p>
          <p class="text-[11.5px] leading-[1.7] text-secondary-foreground">
            The current state is saved as a new checkpoint first, so this is undoable.
          </p>
          <p v-if="restoreError" class="whitespace-pre-wrap break-words text-[11px] text-destructive">{{ restoreError }}</p>
          <div class="flex justify-end gap-2">
            <button class="flex items-center gap-[5px] rounded-md border border-border bg-hover px-3.5 py-1.5 text-xs text-secondary-foreground hover:border-[#444] hover:text-foreground" @click="restoreTarget = null">Cancel</button>
            <button class="flex items-center gap-[5px] rounded-md border-0 bg-accent px-3.5 py-1.5 text-xs font-semibold text-white hover:bg-accent-dim disabled:cursor-default disabled:opacity-50" :disabled="restoreBusy" @click="confirmRestore">
              {{ restoreBusy ? "Restoring…" : "Restore" }}
            </button>
          </div>
        </div>
      </div>
    </Teleport>
    </div>
  </aside>
</template>

<script setup lang="ts">
import { ref, computed, watch, inject, onMounted, onBeforeUnmount } from "vue";
import { invoke } from "@tauri-apps/api/core";
import {
  PhFiles, PhGitBranch, PhGitCommit,
  PhArrowClockwise, PhWarning, PhX, PhArrowUpRight,
  PhArrowUp, PhArrowDown, PhCaretRight,
  PhClockCounterClockwise, PhArrowUUpLeft, PhArrowsOutSimple, PhSparkle, PhPlus, PhGlobe, PhTerminal,
} from "@phosphor-icons/vue";
import { useGitStore, type GitCommit } from "@/stores/git";
import { useFileTreeStore } from "@/stores/fileTree";
import FileTreeNode from "./FileTreeNode.vue";
import { useAutoRefresh } from "@/composables/useAutoRefresh";
import { useContainerQuery } from "@/composables/useContainerQuery";
import AutoRefreshButton from "./AutoRefreshButton.vue";
import PullRequestsPanel from "./PullRequestsPanel.vue";
import ManagerPanel from "./ManagerPanel.vue";
import DiffView from "./DiffView.vue";
import CommitPushMenu from "./CommitPushMenu.vue";
import BrowserPane from "./BrowserPane.vue";
import XTerm from "./XTerm.vue";
import { nextPtyId } from "@/lib/ptyId";

const props = withDefaults(defineProps<{ cwd: string; workspaceId?: number; isGit?: boolean; open?: boolean }>(), { isGit: true, open: true });
const emit = defineEmits<{ openPanel: []; closePanel: []; openProjectConfig: []; managerOpen: [] }>();
const git = useGitStore();
const fileTree = useFileTreeStore();
const activeTab = ref<string | null>(null);
const openedTabIds = ref<string[]>([]);
const workspaceDiff = ref("");
const workspaceDiffLoading = ref(false);
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

// Keep Git surfaces visible even before a repository exists. Changes already
// explains that state and offers Git Init; hiding Diff made the surface picker
// look incomplete for newly opened folders.
const tabs = computed(() => {
  const all = [
    { id: "git", label: "Changes", icon: PhGitBranch, description: "Inspect, stage and commit workspace changes." },
    { id: "pull-requests", label: "Pull requests", icon: PhGitBranch, description: "Check pull requests for this workspace." },
    { id: "explorer", label: "Files", icon: PhFiles, description: "Browse the current workspace." },
    { id: "diff", label: "Diff", icon: PhGitCommit, description: "Review the complete workspace diff." },
    { id: "history", label: "Checkpoints", icon: PhClockCounterClockwise, description: "Review and restore agent-turn snapshots." },
    { id: "manager", label: "Manager", icon: PhSparkle, description: "Plan and coordinate agent work for this project." },
    { id: "browser", label: "Browser", icon: PhGlobe, description: "Preview a dev server without leaving Burrow." },
    { id: "terminal", label: "Terminal", icon: PhTerminal, description: "Run shell commands next to your changes." },
  ];
  return all;
});

const browserOpened = computed(() => openedTabIds.value.includes("browser"));
const terminalOpened = computed(() => openedTabIds.value.includes("terminal"));
const terminalPtyId = nextPtyId();

const openedTabs = computed(() => openedTabIds.value
  .map((id) => tabs.value.find((tab) => tab.id === id))
  .filter((tab): tab is NonNullable<typeof tab> => Boolean(tab)));

function openSurface(id: string) {
  if (!openedTabIds.value.includes(id)) openedTabIds.value.push(id);
  activeTab.value = id;
  if (!props.open) emit("openPanel");
  if (id === "manager") emit("managerOpen");
}

function closeSurface(id: string) {
  openedTabIds.value = openedTabIds.value.filter((openedId) => openedId !== id);
  if (activeTab.value === id) activeTab.value = openedTabs.value[0]?.id ?? null;
}

function showSurfacePicker() {
  activeTab.value = null;
}

function openManager() {
  openSurface("manager");
}

defineExpose({ openManager });

// --- Checkpoints (History tab) ---
interface Checkpoint {
  id: number;
  cwd: string;
  ptyId: string;
  label: string;
  commit: string;
  tree: string;
  createdAt: number;
}

const checkpoints = ref<Checkpoint[]>([]);
const restoreTarget = ref<Checkpoint | null>(null);
const restoreError = ref("");
const restoreBusy = ref(false);

async function loadCheckpoints() {
  if (!props.cwd) return (checkpoints.value = []);
  checkpoints.value = await invoke<Checkpoint[]>("list_checkpoints", { cwd: props.cwd, limit: 50 });
}

function cpTime(ms: number): string {
  return new Date(ms).toLocaleString(undefined, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
}

// What has changed in the workspace since this checkpoint was taken.
async function openCheckpointDiff(cp: Checkpoint) {
  const diff = await invoke<string>("checkpoint_diff", { cwd: props.cwd, commit: cp.commit });
  if (!diff.trim()) return;
  activeTerm()?.openDiffInTab(`Since “${cp.label || cp.commit.slice(0, 7)}”`, false, diff);
}

async function confirmRestore() {
  if (!restoreTarget.value) return;
  restoreBusy.value = true;
  restoreError.value = "";
  try {
    await invoke("restore_checkpoint", { cwd: props.cwd, commit: restoreTarget.value.commit });
    restoreTarget.value = null;
    await loadCheckpoints();
    git.refresh(true);
  } catch (e) {
    restoreError.value = String(e);
  } finally {
    restoreBusy.value = false;
  }
}

watch([activeTab, () => props.cwd], () => { if (activeTab.value === "history") loadCheckpoints(); });
watch([activeTab, () => props.cwd], () => { if (activeTab.value === "diff") loadWorkspaceDiff(); });

async function loadWorkspaceDiff() {
  if (!props.cwd) { workspaceDiff.value = ""; return; }
  workspaceDiffLoading.value = true;
  try {
    const [unstaged, staged] = await Promise.all([git.fetchAllDiff(false), git.fetchAllDiff(true)]);
    workspaceDiff.value = [
      unstaged && "# Unstaged changes\n" + unstaged,
      staged && "# Staged changes\n" + staged,
    ].filter(Boolean).join("\n\n");
  } finally {
    workspaceDiffLoading.value = false;
  }
}

watch(() => props.cwd, (p) => {
  if (p) {
    git.setCwd(p);
    fileTree.loadRoot(p);
  } else {
    fileTree.clearTree();
  }
}, { immediate: true });

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
