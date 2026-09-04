<script setup lang="ts">
import { computed, watch } from "vue";
import { PhArrowClockwise, PhGitBranch, PhWarning, PhCheckCircle } from "@phosphor-icons/vue";
import { useGitStore } from "@/stores/git";

const props = defineProps<{ cwd: string; title: string; description: string }>();
const git = useGitStore();

const changedFiles = computed(() => git.staged.length + git.unstaged.length + git.untracked.length);
const latestCommit = computed(() => git.log[0]);

function refresh() {
  if (props.cwd) git.setCwd(props.cwd);
  git.refresh();
}

watch(() => props.cwd, refresh, { immediate: true });
</script>

<template>
  <section class="pulse-surface">
    <header class="pulse-header">
      <div>
        <p class="pulse-kicker">EXTENSION SURFACE</p>
        <h2 class="pulse-title">{{ title }}</h2>
        <p class="pulse-description">{{ description }}</p>
      </div>
      <button class="pulse-refresh" :disabled="git.loading || !cwd" title="Refresh workspace status" @click="refresh">
        <PhArrowClockwise :size="14" :class="git.loading && 'animate-spin'" />
      </button>
    </header>

    <div v-if="!cwd" class="pulse-empty">Open a workspace to see its status.</div>
    <div v-else-if="git.error" class="pulse-empty"><PhWarning :size="14" /> This folder is not a Git repository.</div>
    <div v-else class="pulse-body">
      <div class="pulse-branch"><PhGitBranch :size="13" /><code>{{ git.branch || "detached HEAD" }}</code></div>
      <div class="pulse-status"><PhCheckCircle v-if="changedFiles === 0" :size="14" class="text-success" /><span>{{ changedFiles === 0 ? "Working tree clean" : `${changedFiles} changed file${changedFiles === 1 ? "" : "s"}` }}</span></div>
      <dl class="pulse-details"><div><dt>Staged</dt><dd>{{ git.staged.length }}</dd></div><div><dt>Modified</dt><dd>{{ git.unstaged.length }}</dd></div><div><dt>Untracked</dt><dd>{{ git.untracked.length }}</dd></div></dl>
      <div v-if="latestCommit" class="pulse-commit"><span>Latest commit</span><code>{{ latestCommit.shortHash }}</code><p>{{ latestCommit.subject }}</p><small>{{ latestCommit.relTime }}</small></div>
    </div>
  </section>
</template>

<style scoped>
.pulse-surface { display: flex; min-height: 0; flex: 1; flex-direction: column; overflow: auto; color: var(--text-secondary); }
.pulse-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; border-bottom: 1px solid var(--border); padding: 14px 12px 12px; }.pulse-kicker { margin: 0 0 4px; color: var(--text-muted); font-size: 9px; font-weight: 700; letter-spacing: .08em; }.pulse-title { margin: 0; color: var(--text-primary); font-size: 13px; font-weight: 650; }.pulse-description { margin: 4px 0 0; font-size: 10.5px; line-height: 1.45; }
.pulse-refresh { display: inline-flex; align-items: center; justify-content: center; border: 0; border-radius: 4px; background: transparent; color: var(--text-muted); cursor: pointer; padding: 5px; }.pulse-refresh:hover { background: var(--bg-hover); color: var(--text-primary); }.pulse-refresh:disabled { cursor: default; opacity: .45; }
.pulse-empty { display: flex; align-items: center; justify-content: center; gap: 7px; padding: 30px 16px; color: var(--text-muted); font-size: 11px; text-align: center; }.pulse-body { display: flex; flex-direction: column; gap: 12px; padding: 12px; }.pulse-branch, .pulse-status { display: flex; align-items: center; gap: 7px; font-size: 11px; }.pulse-branch { color: var(--text-primary); }.pulse-branch code, .pulse-commit code { font-family: var(--font-mono, ui-monospace, monospace); }
.pulse-details { display: grid; grid-template-columns: repeat(3, 1fr); margin: 0; border-bottom: 1px solid var(--border); border-top: 1px solid var(--border); }.pulse-details div { padding: 8px 0; text-align: center; }.pulse-details div + div { border-left: 1px solid var(--border); }.pulse-details dt { color: var(--text-muted); font-size: 9px; text-transform: uppercase; }.pulse-details dd { margin: 3px 0 0; color: var(--text-primary); font-size: 14px; font-weight: 650; }
.pulse-commit { display: grid; grid-template-columns: auto 1fr; column-gap: 7px; font-size: 10px; }.pulse-commit span { color: var(--text-muted); }.pulse-commit code { color: var(--text-secondary); }.pulse-commit p { grid-column: 1 / -1; margin: 6px 0 2px; color: var(--text-primary); font-size: 11px; line-height: 1.4; }.pulse-commit small { grid-column: 1 / -1; color: var(--text-muted); font-size: 10px; }
</style>
