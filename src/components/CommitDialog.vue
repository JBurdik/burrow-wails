<template>
  <Teleport to="body">
    <div class="fixed inset-0 z-[1500] flex items-center justify-center bg-black/60" @click.self="cancel">
      <div class="flex max-h-[85vh] w-[480px] flex-col overflow-hidden rounded-[10px] border border-border bg-panel shadow-[0_24px_64px_rgba(0,0,0,0.5)]">
        <div class="border-b border-border/70 px-5 py-4">
          <h2 class="text-sm font-semibold text-foreground">Commit changes</h2>
          <p class="mt-1 text-[11px] text-muted-foreground">
            Review and confirm your commit. Leave the message blank to auto-generate one.
          </p>
        </div>

        <div class="flex-1 space-y-4 overflow-auto px-5 py-4">
          <div class="space-y-2 rounded-lg border border-border/70 bg-hover/40 p-3 text-xs">
            <div class="flex items-center justify-between">
              <span class="text-muted-foreground">Branch</span>
              <span class="font-medium text-foreground">{{ git.branch || "(detached HEAD)" }}</span>
            </div>
            <div class="flex items-center justify-between">
              <span class="text-muted-foreground">Files</span>
              <span v-if="excludedCount > 0" class="text-muted-foreground">
                ({{ files.length - excludedCount }} of {{ files.length }})
              </span>
            </div>
            <div v-if="loading" class="py-3 text-center text-muted-foreground">Loading changes…</div>
            <p v-else-if="files.length === 0" class="font-medium text-foreground">none</p>
            <div v-else class="max-h-44 space-y-1 overflow-auto rounded-md border border-border/70 bg-panel p-1">
              <label
                v-for="f in files"
                :key="f.path"
                class="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1 font-mono text-[11px] hover:bg-hover"
              >
                <input type="checkbox" :checked="!excluded.has(f.path)" @change="toggleFile(f.path)" />
                <span class="flex-1 truncate" :class="excluded.has(f.path) && 'text-muted-foreground'">{{ f.path }}</span>
                <span v-if="excluded.has(f.path)" class="shrink-0 text-muted-foreground">Excluded</span>
                <span v-else-if="f.binary" class="shrink-0 text-muted-foreground">binary</span>
                <span v-else class="shrink-0">
                  <span class="text-success">+{{ f.insertions }}</span>
                  <span class="text-muted-foreground"> / </span>
                  <span class="text-destructive">-{{ f.deletions }}</span>
                </span>
              </label>
            </div>
          </div>

          <div class="space-y-1">
            <p class="text-[11px] font-medium text-foreground">Commit message (optional)</p>
            <textarea
              v-model="message"
              rows="3"
              placeholder="Leave empty to auto-generate"
              class="w-full resize-none rounded-md border border-border bg-panel px-2.5 py-1.5 text-xs text-foreground outline-none focus:border-foreground/40"
            />
          </div>
        </div>

        <div class="flex items-center justify-end gap-2 border-t border-border/70 px-5 py-3">
          <button class="btn-outline" @click="cancel">Cancel</button>
          <button class="btn-outline" :disabled="noneSelected || busy" @click="commitOnNewBranch">
            Commit on new branch
          </button>
          <button class="btn-primary" :disabled="noneSelected || busy" @click="doCommit">
            {{ busy ? "Committing…" : "Commit" }}
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from "vue";
import { useGitStore, type GitFileStat } from "@/stores/git";

const emit = defineEmits<{ close: [] }>();
const git = useGitStore();

const files = ref<GitFileStat[]>([]);
const excluded = ref<Set<string>>(new Set());
const message = ref("");
const loading = ref(true);
const busy = ref(false);

const excludedCount = computed(() => excluded.value.size);
const noneSelected = computed(() => files.value.length > 0 && excluded.value.size === files.value.length);

onMounted(async () => {
  window.addEventListener("keydown", onKey);
  await git.stageAllIfNeeded();
  files.value = await git.stagedFileStats();
  loading.value = false;
});
onBeforeUnmount(() => window.removeEventListener("keydown", onKey));

function onKey(e: KeyboardEvent) {
  if (e.key === "Escape") cancel();
}

async function toggleFile(path: string) {
  const next = new Set(excluded.value);
  if (next.has(path)) {
    next.delete(path);
    await git.stageFile(path);
  } else {
    next.add(path);
    await git.unstageFile(path);
  }
  excluded.value = next;
}

function cancel() {
  emit("close");
}

async function runCommit() {
  busy.value = true;
  try {
    if (message.value.trim()) git.commitMsg = message.value.trim();
    await git.commit();
  } finally {
    busy.value = false;
  }
}

async function doCommit() {
  await runCommit();
  emit("close");
}

async function commitOnNewBranch() {
  busy.value = true;
  try {
    git.commitMsg = message.value.trim();
    if (!git.commitMsg) await git.generateCommitMessage();
    const generated = await git.generateBranchName(git.commitMsg);
    const fallback = `wip/${new Date().toISOString().replace(/[:.]/g, "-").slice(0, 19)}`;
    await git.createBranch(generated || fallback);
  } finally {
    busy.value = false;
  }
  await runCommit();
  emit("close");
}
</script>

<style scoped>
.btn-outline {
  border-radius: 6px;
  border: 1px solid var(--border, #333);
  padding: 5px 12px;
  font-size: 11px;
  color: var(--text-secondary);
  background: transparent;
}
.btn-outline:hover:not(:disabled) { background: var(--bg-hover); color: var(--text-primary); }
.btn-outline:disabled { opacity: 0.4; cursor: default; }
.btn-primary {
  border-radius: 6px;
  border: none;
  padding: 5px 14px;
  font-size: 11px;
  font-weight: 500;
  color: #fff;
  background: var(--accent, #d1479e);
}
.btn-primary:hover:not(:disabled) { filter: brightness(1.08); }
.btn-primary:disabled { opacity: 0.4; cursor: default; }
</style>
