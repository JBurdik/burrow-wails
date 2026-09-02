<template>
  <div ref="rootEl" class="relative flex shrink-0 [-webkit-app-region:no-drag]">
    <button
      class="flex items-center gap-1 rounded-l-md border border-r-0 border-border/70 bg-transparent px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground transition-colors hover:border-border hover:bg-hover hover:text-secondary-foreground disabled:cursor-default disabled:opacity-40 disabled:hover:border-border/70 disabled:hover:bg-transparent"
      :disabled="primaryDisabled || busy"
      :title="busyTitle || primaryTitle"
      @click="commitAndPush"
    >
      <PhArrowUp :size="11" :class="busy && 'animate-spin'" />
      {{ busyTitle || "Commit & push" }}
    </button>
    <button
      class="flex items-center rounded-r-md border border-border/70 bg-transparent px-1 py-0.5 text-muted-foreground transition-colors hover:border-border hover:bg-hover hover:text-secondary-foreground disabled:cursor-default disabled:opacity-40"
      :disabled="busy"
      title="More git actions"
      @click.stop="open = !open"
    >
      <PhCaretDown :size="8" />
    </button>
    <div
      v-if="open"
      class="absolute left-0 top-[calc(100%+5px)] z-[1500] min-w-[170px] overflow-hidden rounded-md border border-border bg-panel py-1 shadow-[0_8px_24px_rgba(0,0,0,0.45)]"
      @click.stop
    >
      <button
        class="menu-item"
        :disabled="commitDisabled || busy"
        title="Commit staged changes"
        @click="doCommit"
      >
        <PhGitCommit :size="13" :class="(git.committing || git.generating) && 'animate-spin'" />
        {{ git.generating ? "Generating…" : git.committing ? "Committing…" : "Commit" }}
      </button>
      <button
        class="menu-item"
        :disabled="pushDisabled || busy"
        :title="git.hasUpstream ? 'git push' : 'git push -u origin ' + git.branch"
        @click="doPush"
      >
        <PhArrowUp :size="13" :class="git.pushing && 'animate-spin'" />
        {{ git.pushing ? "Pushing…" : git.hasUpstream ? "Push" : "Publish branch" }}
      </button>
      <button
        class="menu-item"
        :disabled="prDisabled"
        title="gh pr create --fill"
        @click="doCreatePr"
      >
        <PhGitPullRequest :size="13" /> {{ pr.actionLoading.value ? "Creating…" : "Create PR" }}
      </button>
      <div v-if="pr.error.value" class="px-2.5 pt-1 text-[10px] text-destructive">{{ pr.error.value }}</div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from "vue";
import { PhArrowUp, PhCaretDown, PhGitCommit, PhGitPullRequest } from "@phosphor-icons/vue";
import { useGitStore } from "@/stores/git";
import { useUIStore } from "@/stores/ui";
import { usePullRequests } from "@/composables/usePullRequests";

const git = useGitStore();
const ui = useUIStore();
const pr = usePullRequests(() => git.cwd);
const open = ref(false);
const rootEl = ref<HTMLElement | null>(null);

// Like t3code: enabled purely on "anything changed", no message required —
// an empty box gets auto-generated right before the commit (git.commit()).
const commitDisabled = computed(() => !git.hasWorkingTreeChanges);
const pushDisabled = computed(() => git.pushing || (git.hasUpstream && git.ahead === 0));
const primaryDisabled = computed(() => commitDisabled.value || git.pushing);
// ponytail: PR gate is "has upstream" only — doesn't check gh auth/repo host, gh itself reports that on failure via pr.error.
const prDisabled = computed(() => !git.hasUpstream || pr.actionLoading.value);

const primaryTitle = computed(() =>
  commitDisabled.value ? "No changes to commit" : "Stage all changes, commit (auto-message if empty), then push"
);
const busy = computed(() => git.generating || git.committing || git.pushing);
const busyTitle = computed(() =>
  git.generating ? "Generating message…" : git.committing ? "Committing…" : git.pushing ? "Pushing…" : ""
);

async function commitAndPush() {
  if (primaryDisabled.value) return;
  await git.commit(ui.commitMessageModel);
  await git.push();
}

async function doCommit() {
  open.value = false;
  if (!commitDisabled.value) await git.commit(ui.commitMessageModel);
}

async function doPush() {
  open.value = false;
  if (!pushDisabled.value) await git.push();
}

async function doCreatePr() {
  open.value = false;
  if (!prDisabled.value) await pr.create();
}

function onDocClick(e: MouseEvent) {
  if (open.value && rootEl.value && !rootEl.value.contains(e.target as Node)) open.value = false;
}
onMounted(() => window.addEventListener("click", onDocClick));
onBeforeUnmount(() => window.removeEventListener("click", onDocClick));
</script>

<style scoped>
.menu-item {
  display: flex;
  width: 100%;
  align-items: center;
  gap: 6px;
  padding: 6px 10px;
  text-align: left;
  font-family: var(--font-ui, -apple-system, sans-serif);
  font-size: 11px;
  color: var(--text-secondary);
  background: none;
  border: none;
  cursor: pointer;
  transition: background-color 0.15s, color 0.15s;
}
.menu-item:hover { background: var(--bg-hover); color: var(--text-primary); }
.menu-item:disabled { opacity: 0.4; cursor: default; }
.menu-item:disabled:hover { background: none; color: var(--text-secondary); }
</style>
