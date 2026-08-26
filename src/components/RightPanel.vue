<template>
  <aside class="right-panel" ref="panelEl">
    <!-- Tab bar -->
    <div class="panel-tabs">
      <button
        v-for="tab in tabs"
        :key="tab.id"
        class="panel-tab"
        :class="{ active: activeTab === tab.id }"
        @click="activeTab = tab.id"
      >
        <component :is="tab.icon" :size="12" />
        <span v-if="cq.is.sm">{{ tab.label }}</span>
      </button>
    </div>

    <!-- Explorer tab -->
    <div v-if="activeTab === 'explorer'" class="panel-content">
      <div v-if="!props.cwd" class="hint">No workspace open</div>
      <div v-else-if="fileTree.rootError" class="hint error">{{ fileTree.rootError }}</div>
      <div v-else class="file-tree">
        <FileTreeNode v-for="node in fileTree.tree" :key="node.id" :node="node" :depth="0" />
      </div>
    </div>

    <!-- Git tab -->
    <div v-else-if="activeTab === 'git'" class="panel-content git-panel">
      <!-- Header -->
      <div class="git-header">
        <div class="branch-tag">
          <PhGitBranch :size="12" />
          <span>{{ git.branch || "—" }}</span>
          <span v-if="git.ahead > 0" class="ahead-tag" title="Commits ahead of upstream">↑{{ git.ahead }}</span>
          <span v-if="git.behind > 0" class="behind-tag" title="Commits behind upstream">↓{{ git.behind }}</span>
        </div>
        <div class="header-actions">
          <button
            v-if="!git.error && git.hasUpstream && git.behind > 0"
            class="push-btn"
            :disabled="git.pulling || git.pushing || git.loading"
            @click="git.pull()"
            title="git pull --ff-only"
          >
            <PhArrowDown :size="11" :class="{ spin: git.pulling }" />
            Pull
            <span>({{ git.behind }})</span>
          </button>
          <button
            v-if="!git.error"
            class="push-btn"
            :disabled="git.pushing || git.loading || (git.hasUpstream && git.ahead === 0)"
            @click="git.push()"
            :title="git.hasUpstream ? 'git push' : 'git push -u origin ' + git.branch"
          >
            <PhArrowUp :size="11" :class="{ spin: git.pushing }" />
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
          <button class="icon-btn" :disabled="git.loading" @click="git.refresh()" title="Refresh">
            <PhArrowClockwise :size="13" :class="{ spin: git.loading }" />
          </button>
        </div>
      </div>

      <!-- Push/pull loader -->
      <div v-if="git.pushing || git.pulling" class="git-progress">
        <div class="git-progress-bar"></div>
        <span class="git-progress-label">{{ git.pushing ? "Pushing…" : "Pulling…" }}</span>
      </div>

      <div class="git-body">
        <!-- Error -->
        <div v-if="git.error" class="git-error">
          <PhWarning :size="13" />
          Not a git repository
          <button class="git-init-btn" :disabled="git.loading" @click="git.gitInit()">
            <PhGitBranch :size="12" />
            Git Init
          </button>
        </div>

        <template v-else>
          <!-- Staged -->
          <div class="section-label section-label-row">
            Staged
            <button
              v-if="git.staged.length > 0"
              class="stage-all-btn"
              @click="openAllDiffInTab(true)"
              title="Open all staged diffs in new tab"
            ><PhArrowUpRight :size="10" /> View</button>
          </div>
          <div v-if="git.staged.length === 0" class="empty-hint">Nothing staged</div>
          <div
            v-for="f in git.staged"
            :key="'s:' + f.path"
            class="git-file staged"
            @click="git.showDiff(f.path, true)"
          >
            <span class="file-status">{{ f.x }}</span>
            <span class="file-path" :title="f.path">{{ f.path }}</span>
            <button class="file-btn" @click.stop="git.unstageFile(f.path)" title="Unstage">−</button>
          </div>

          <!-- Unstaged + untracked -->
          <div class="section-label section-label-row" style="margin-top: 8px;">
            Changes
            <div class="section-actions">
              <button
                v-if="git.unstaged.length > 0"
                class="stage-all-btn"
                @click="openAllDiffInTab(false)"
                title="Open all unstaged diffs in new tab"
              ><PhArrowUpRight :size="10" /> View</button>
              <button
                v-if="git.unstaged.length > 0 || git.untracked.length > 0"
                class="stage-all-btn"
                :disabled="git.loading"
                @click="git.stageAll()"
                title="Stage all"
              >+ All</button>
            </div>
          </div>
          <div v-if="git.unstaged.length === 0 && git.untracked.length === 0" class="empty-hint">
            Working tree clean
          </div>
          <div
            v-for="f in git.unstaged"
            :key="'u:' + f.path"
            class="git-file unstaged"
            @click="git.showDiff(f.path, false)"
          >
            <span class="file-status">{{ f.y }}</span>
            <span class="file-path" :title="f.path">{{ f.path }}</span>
            <button class="file-btn add" @click.stop="git.stageFile(f.path)" title="Stage">+</button>
          </div>
          <div
            v-for="f in git.untracked"
            :key="'t:' + f.path"
            class="git-file untracked"
          >
            <span class="file-status">?</span>
            <span class="file-path" :title="f.path">{{ f.path }}</span>
            <button class="file-btn add" @click.stop="git.stageFile(f.path)" title="Stage">+</button>
          </div>

          <!-- Commit -->
          <div class="commit-section">
            <textarea
              v-model="git.commitMsg"
              class="commit-input"
              placeholder="Commit message…"
              rows="3"
              @keydown.ctrl.enter="git.commit()"
              @keydown.meta.enter="git.commit()"
            />
            <button
              class="commit-btn"
              :disabled="!git.commitMsg.trim() || git.staged.length === 0"
              @click="git.commit()"
            >
              <PhGitCommit :size="12" />
              Commit
            </button>
          </div>

          <!-- Diff (hidden when panel is too narrow — use cq.is.md = ≥320px) -->
          <div v-if="git.diffFile && cq.is.md" class="diff-section">
            <div class="diff-header">
              <span class="diff-title">{{ git.diffFile }}</span>
              <span class="diff-mode">{{ git.diffStaged ? "staged" : "unstaged" }}</span>
              <button class="icon-btn" @click="git.clearDiff()" title="Close">
                <PhX :size="11" />
              </button>
            </div>
            <pre class="diff-view"><span
              v-for="(line, i) in git.diff.split('\n')"
              :key="i"
              :class="diffLineClass(line)"
            >{{ line }}
</span></pre>
          </div>

          <!-- History -->
          <div class="history-section">
            <div class="section-label section-label-row history-toggle" @click="showHistory = !showHistory">
              <span class="history-title"><PhCaretRight :size="9" :class="{ open: showHistory }" /> History</span>
            </div>
            <template v-if="showHistory">
              <div v-if="git.log.length === 0" class="empty-hint">No commits</div>
              <div
                v-for="(c, i) in git.log"
                :key="c.hash"
                class="log-row"
                :class="{ unpushed: i < git.ahead }"
                :title="c.subject + '\n' + c.author + (i < git.ahead ? '\n↑ Not pushed' : '')"
                @click="openCommitDiff(c)"
              >
                <span class="log-hash">{{ c.shortHash }}</span>
                <span v-if="i < git.ahead" class="log-unpushed-dot" title="Not pushed">↑</span>
                <span class="log-subject">{{ c.subject }}</span>
                <span class="log-meta">{{ c.relTime }}</span>
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
.right-panel {
  width: var(--right-panel-width, 300px);
  flex: 0 0 var(--right-panel-width, 300px);
  background: var(--bg-panel);
  backdrop-filter: var(--blur-panels, none);
  -webkit-backdrop-filter: var(--blur-panels, none);
  border-left: 1px solid var(--border);
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
  overflow: hidden;
  font-size: 12px;
}

.panel-tabs {
  display: flex;
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
  height: 32px;
}

.panel-tab {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
  background: none;
  border: none;
  border-bottom: 2px solid transparent;
  color: var(--text-muted);
  cursor: pointer;
  font-size: 11px;
  font-family: var(--font-ui);
  padding: 0 4px;
  transition: color 0.1s;
}
.panel-tab:hover { color: var(--text-secondary); }
.panel-tab.active {
  color: var(--text-primary);
  border-bottom-color: var(--accent);
  background: color-mix(in srgb, var(--accent) 5%, transparent);
}

.panel-content {
  flex: 1;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
}

.hint {
  font-size: 11px;
  color: var(--text-muted);
  padding: 16px;
  text-align: center;
}
.hint.error { color: var(--red); }

.file-tree {
  flex: 1;
  padding: 4px 0;
}

/* Git panel */
.git-panel { overflow: hidden; }

.git-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 5px 8px;
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
}

.branch-tag {
  display: flex;
  align-items: center;
  gap: 4px;
  color: var(--text-secondary);
  font-size: 11px;
  font-family: var(--font-mono);
}
.branch-tag :deep(svg) { color: var(--yellow); flex-shrink: 0; }

.ahead-tag { color: var(--green); font-size: 10px; }
.behind-tag { color: var(--yellow); font-size: 10px; }

.header-actions {
  display: flex;
  align-items: center;
  gap: 3px;
}

.push-btn {
  display: flex;
  align-items: center;
  gap: 3px;
  background: none;
  border: 1px solid var(--border);
  border-radius: 4px;
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 10px;
  font-weight: 500;
  font-family: var(--font-ui);
  padding: 2px 7px;
  transition: background 0.1s, color 0.1s, border-color 0.1s;
}
.push-btn:hover:not(:disabled) {
  background: var(--bg-hover);
  color: var(--text-primary);
  border-color: color-mix(in srgb, var(--accent) 40%, var(--border));
}
.push-btn:disabled { opacity: 0.35; cursor: default; }

.icon-btn {
  background: none;
  border: none;
  color: var(--text-muted);
  cursor: pointer;
  padding: 3px;
  border-radius: 3px;
  display: flex;
  align-items: center;
  transition: color 0.1s, background 0.1s;
}
.icon-btn:hover { color: var(--text-primary); background: var(--bg-hover); }
.icon-btn:disabled { opacity: 0.35; cursor: default; }

@keyframes spin { to { transform: rotate(360deg); } }
.spin { animation: spin 1s linear infinite; }

.git-progress {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 8px;
  border-bottom: 1px solid var(--border);
}
.git-progress-bar {
  position: relative;
  flex: 1;
  height: 2px;
  border-radius: 2px;
  background: var(--border);
  overflow: hidden;
}
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
.git-progress-label { font-size: 10px; color: var(--text-muted); }
@keyframes git-indeterminate {
  0%   { left: -40%; }
  100% { left: 100%; }
}

.git-body {
  flex: 1;
  overflow-y: auto;
  padding: 6px 0;
  display: flex;
  flex-direction: column;
}

.git-error {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  color: var(--text-secondary);
  padding: 16px 10px;
  font-size: 11px;
}

.git-init-btn {
  display: flex;
  align-items: center;
  gap: 4px;
  margin-left: auto;
  padding: 3px 8px;
  border-radius: 4px;
  border: 1px solid var(--border);
  background: var(--bg-hover);
  color: var(--text-primary);
  font-size: 11px;
  cursor: pointer;
}
.git-init-btn:hover { background: var(--yellow); color: #000; border-color: var(--yellow); }
.git-init-btn:disabled { opacity: 0.35; cursor: default; }

.section-label {
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--text-muted);
  opacity: 0.65;
  padding: 4px 8px 3px;
}

.section-label-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.stage-all-btn {
  font-size: 10px;
  font-weight: 500;
  padding: 1px 5px;
  border-radius: 3px;
  border: 1px solid var(--border);
  background: none;
  color: var(--text-muted);
  cursor: pointer;
  text-transform: none;
  letter-spacing: 0;
  opacity: 0.8;
  transition: background 0.1s, color 0.1s, opacity 0.1s;
}
.stage-all-btn:hover { background: var(--bg-hover); color: var(--text-primary); opacity: 1; }
.stage-all-btn:disabled { opacity: 0.3; cursor: default; }

.section-actions {
  display: flex;
  align-items: center;
  gap: 3px;
}

.empty-hint {
  font-size: 11px;
  color: var(--text-muted);
  opacity: 0.6;
  padding: 2px 8px 6px;
}

.git-file {
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 2px 8px;
  cursor: pointer;
  border-radius: 3px;
  margin: 0 3px;
  transition: background 0.08s;
}
.git-file:hover { background: var(--bg-hover); }

.file-status {
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 700;
  width: 11px;
  flex-shrink: 0;
  text-align: center;
}
.staged   .file-status { color: var(--green); }
.unstaged .file-status { color: var(--yellow); }
.untracked .file-status { color: var(--text-muted); }

.file-path {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 11px;
  font-family: var(--font-mono);
  color: var(--text-secondary);
  transition: color 0.08s;
}
.git-file:hover .file-path { color: var(--text-primary); }

.file-btn {
  background: none;
  border: none;
  cursor: pointer;
  color: var(--text-muted);
  font-size: 13px;
  line-height: 1;
  padding: 0 2px;
  flex-shrink: 0;
  display: none;
}
.git-file:hover .file-btn { display: block; }
.file-btn:hover { color: var(--text-primary); }
.file-btn.add:hover { color: var(--green); }

/* Commit */
.commit-section {
  margin-top: 6px;
  padding: 8px;
  display: flex;
  flex-direction: column;
  gap: 5px;
  border-top: 1px solid var(--border);
  flex-shrink: 0;
}

.commit-input {
  background: color-mix(in srgb, var(--border) 15%, var(--bg-panel));
  border: 1px solid var(--border);
  border-radius: 4px;
  color: var(--text-primary);
  font-family: var(--font-ui);
  font-size: 11px;
  line-height: 1.5;
  outline: none;
  padding: 6px 8px;
  resize: none;
  width: 100%;
  min-height: 52px;
  max-height: 100px;
  box-sizing: border-box;
  transition: border-color 0.1s;
}
.commit-input::placeholder { color: var(--text-muted); opacity: 0.6; }
.commit-input:focus { border-color: color-mix(in srgb, var(--accent) 60%, var(--border)); }
.commit-input::-webkit-scrollbar { display: none; }

.commit-btn {
  width: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 5px;
  background: color-mix(in srgb, var(--accent) 85%, transparent);
  border: none;
  border-radius: 4px;
  color: #fff;
  cursor: pointer;
  font-size: 11px;
  font-weight: 600;
  font-family: var(--font-ui);
  padding: 5px 10px;
  transition: background 0.1s;
}
.commit-btn:hover:not(:disabled) { background: var(--accent); }
.commit-btn:disabled { opacity: 0.35; cursor: default; }

/* Diff */
.diff-section {
  border-top: 1px solid var(--border);
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  max-height: 220px;
}

.diff-header {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 4px 8px;
  background: color-mix(in srgb, var(--border) 20%, var(--bg-panel));
  flex-shrink: 0;
}

.diff-title {
  font-family: var(--font-mono);
  font-size: 10px;
  color: var(--text-secondary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
}

.diff-mode {
  font-size: 10px;
  color: var(--text-muted);
  flex-shrink: 0;
}

.diff-view {
  overflow: auto;
  font-family: var(--font-mono);
  font-size: 10px;
  line-height: 1.5;
  white-space: pre;
  margin: 0;
  padding: 5px 0;
  flex: 1;
}

.diff-add  { color: var(--green); display: block; }
.diff-del  { color: var(--red); display: block; }
.diff-hunk { color: var(--accent); display: block; }
.diff-ctx  { color: var(--text-secondary); display: block; }

/* History */
.history-section {
  border-top: 1px solid var(--border);
  margin-top: 6px;
  padding-top: 4px;
  flex-shrink: 0;
}

.history-toggle { cursor: pointer; user-select: none; }
.history-title { display: flex; align-items: center; gap: 4px; }
.history-title :deep(svg) { transition: transform 0.12s; }
.history-title .open { transform: rotate(90deg); }

.log-row {
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 2px 8px;
  cursor: pointer;
  margin: 0 3px;
  border-radius: 3px;
  transition: background 0.08s;
}
.log-row:hover { background: var(--bg-hover); }
.log-row.unpushed { background: color-mix(in srgb, var(--accent) 6%, transparent); }
.log-row.unpushed:hover { background: color-mix(in srgb, var(--accent) 12%, transparent); }
.log-row.unpushed .log-hash { color: var(--accent); }

.log-unpushed-dot {
  font-size: 9px;
  font-weight: 700;
  color: var(--accent);
  flex-shrink: 0;
}

.log-hash {
  font-family: var(--font-mono);
  font-size: 10px;
  color: var(--yellow);
  flex-shrink: 0;
}
.log-subject {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 11px;
  color: var(--text-secondary);
  transition: color 0.08s;
}
.log-row:hover .log-subject { color: var(--text-primary); }
.log-meta {
  font-size: 10px;
  color: var(--text-muted);
  flex-shrink: 0;
}
</style>
