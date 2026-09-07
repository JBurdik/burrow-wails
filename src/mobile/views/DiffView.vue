<script setup lang="ts">
// The last piece of the PWA v1 surface (spec §5): read what an agent
// actually changed, from the phone.
//
// Read-only on purpose. Staging, committing and discarding are desktop work
// and spec v1 asks for none of them; a destructive git action behind a
// mis-tap on a phone is a bad trade for a feature nobody asked for.
import { computed, ref, watch } from 'vue';
import { useRemoteStore } from '../store';

const store = useRemoteStore();

interface ChangedFile {
  path: string;
  /** Two-letter porcelain code, e.g. " M", "??", "A ". */
  code: string;
}

// Workspaces only — the synthetic "live sessions" group has no path to run
// git in.
const repos = computed(() => store.workspaces.filter((w) => w.path));
const repoPath = ref(repos.value[0]?.path ?? '');
const files = ref<ChangedFile[]>([]);
const openPath = ref<string | null>(null);
const diff = ref('');
const busy = ref(false);
const err = ref('');

async function git(args: string[]): Promise<string> {
  return await store.getTransport().invoke<string>('run_git', { cwd: repoPath.value, args });
}

async function loadStatus() {
  if (!repoPath.value) return;
  busy.value = true;
  err.value = '';
  openPath.value = null;
  diff.value = '';
  try {
    // porcelain=v1 is the stable machine format; -z would be safer for paths
    // with newlines and is what a desktop UI should use, but a phone list is
    // read-only and a mangled exotic filename is a cosmetic bug here.
    const out = await git(['status', '--porcelain=v1']);
    files.value = out
      .split('\n')
      .filter((line) => line.length > 3)
      .map((line) => ({ code: line.slice(0, 2), path: line.slice(3).trim() }));
  } catch (e: any) {
    err.value = e?.message ?? 'git status selhal';
    files.value = [];
  } finally {
    busy.value = false;
  }
}

async function openFile(file: ChangedFile) {
  if (openPath.value === file.path) {
    openPath.value = null;
    return;
  }
  openPath.value = file.path;
  diff.value = '';
  busy.value = true;
  try {
    // An untracked file has no diff against the index, so `git diff` prints
    // nothing at all — which reads as "no changes" for a file that is
    // entirely new. --no-index against /dev/null is what shows its contents.
    diff.value = file.code.trim() === '??'
      ? await git(['diff', '--no-index', '--', '/dev/null', file.path]).catch((e) => String(e?.message ?? e))
      : await git(['diff', '--', file.path]);
    if (!diff.value.trim()) {
      // Staged changes do not appear in a plain `git diff`.
      diff.value = await git(['diff', '--cached', '--', file.path]);
    }
  } catch (e: any) {
    diff.value = `git diff selhal: ${e?.message ?? e}`;
  } finally {
    busy.value = false;
  }
}

function label(code: string): string {
  const c = code.trim();
  if (c === '??') return 'nový';
  if (c.startsWith('A')) return 'přidán';
  if (c.startsWith('D') || c.endsWith('D')) return 'smazán';
  if (c.startsWith('R')) return 'přesunut';
  return 'změněn';
}

function lineClass(line: string): string {
  if (line.startsWith('+++') || line.startsWith('---')) return 'dl-meta';
  if (line.startsWith('@@')) return 'dl-hunk';
  if (line.startsWith('+')) return 'dl-add';
  if (line.startsWith('-')) return 'dl-del';
  if (line.startsWith('diff ') || line.startsWith('index ')) return 'dl-meta';
  return '';
}

watch(repoPath, loadStatus, { immediate: true });
watch(repos, () => {
  if (!repoPath.value && repos.value.length) repoPath.value = repos.value[0].path;
});
</script>

<template>
  <header class="m-nav">
    <button class="m-nav-back" type="button" @click="store.showDashboard">‹ Přehled</button>
    <span class="m-nav-title">Změny</span>
    <button class="m-btn-ghost" type="button" :disabled="busy" @click="loadStatus">
      {{ busy ? '…' : 'Obnovit' }}
    </button>
  </header>

  <main class="m-body">
    <div v-if="repos.length > 1" class="repo-picker">
      <select v-model="repoPath" class="m-input">
        <option v-for="r in repos" :key="r.id" :value="r.path">{{ r.name }}</option>
      </select>
    </div>

    <div v-if="!repos.length" class="m-state">
      <span class="m-state-icon" aria-hidden="true">□</span>
      <span class="m-state-msg">Žádný workspace</span>
    </div>
    <div v-else-if="err" class="m-state" role="alert">
      <span class="m-state-msg">Nepodařilo se přečíst změny</span>
      <span class="m-state-detail">{{ err }}</span>
    </div>
    <div v-else-if="busy && !files.length" class="m-state">
      <span class="m-state-icon" aria-hidden="true">···</span>
      <span class="m-state-msg">Čtu git status</span>
    </div>
    <div v-else-if="!files.length" class="m-state">
      <span class="m-state-icon" aria-hidden="true">✓</span>
      <span class="m-state-msg">Pracovní strom je čistý</span>
    </div>

    <ul v-else class="m-list">
      <li v-for="f in files" :key="f.path">
        <button class="m-row" type="button" @click="openFile(f)">
          <code class="d-code">{{ f.code.trim() || '·' }}</code>
          <span class="d-main">
            <span class="d-path">{{ f.path }}</span>
            <span class="d-label">{{ label(f.code) }}</span>
          </span>
          <span class="d-chevron" aria-hidden="true">{{ openPath === f.path ? '⌄' : '›' }}</span>
        </button>
        <pre v-if="openPath === f.path" class="d-diff"><code
          v-for="(line, i) in diff.split('\n')"
          :key="i"
          :class="lineClass(line)"
        >{{ line || ' ' }}</code></pre>
      </li>
    </ul>
  </main>
</template>

<style scoped>
.repo-picker { padding: 12px 16px; }
.d-code {
  flex-shrink: 0;
  width: 26px;
  color: var(--yellow);
  font: 700 11px/1 var(--font-mono);
  text-align: center;
}
.d-main { flex: 1; min-width: 0; display: grid; gap: 2px; }
.d-path {
  overflow: hidden;
  color: var(--text-primary);
  font: 12px/1.3 var(--font-mono);
  text-overflow: ellipsis;
  /* The interesting end of a path is the FILE, so overflow eats the left. */
  direction: rtl;
  text-align: left;
}
.d-label { color: var(--text-muted); font-size: 11px; }
.d-chevron { color: var(--text-muted); font-size: 18px; }
.d-diff {
  margin: 0;
  padding: 8px 12px;
  overflow-x: auto;
  background: var(--bg-panel);
  border-bottom: 1px solid var(--border);
  font: 11px/1.5 var(--font-mono);
  white-space: pre;
}
.d-diff code { display: block; color: var(--text-secondary); }
.dl-add { color: var(--green); }
.dl-del { color: var(--red); }
.dl-hunk { color: var(--accent); }
.dl-meta { color: var(--text-muted); }
</style>
