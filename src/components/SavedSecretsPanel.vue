<template>
  <div class="flex flex-col gap-2.5">
    <span class="s-label">Saved secrets</span>
    <span class="text-[11px] text-muted-foreground">
      Values agents asked for and you chose to remember for a project. Only names are shown; values never leave Burrow's core.
    </span>
    <div v-if="!rows.length" class="py-3 text-center text-xs text-muted-foreground/50">No saved secrets.</div>
    <div v-for="row in rows" :key="row.ws.id" class="flex flex-col gap-1 rounded-[var(--radius-card)] border border-border bg-hover px-2.5 py-2">
      <span class="text-[13px] font-medium text-foreground">{{ row.ws.name }}</span>
      <div v-for="s in row.secrets" :key="s.name" class="flex items-center gap-3">
        <code class="min-w-0 flex-1 truncate font-mono text-[12px] text-foreground">{{ s.name }}</code>
        <span class="shrink-0 text-[11px] text-muted-foreground" :title="`Created ${fmt(s.created)}`">Updated {{ fmt(s.updated) }}</span>
        <button class="h-[26px] shrink-0 rounded-[var(--radius-nav)] px-2 text-[12px] text-muted-foreground hover:bg-base hover:text-[var(--red)]" @click="remove(row.ws.id, s.name)">Delete</button>
      </div>
    </div>
    <span v-if="error" class="text-[12px] text-[var(--red)]">{{ error }}</span>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { useWorkspaceStore } from "@/stores/workspace";
import { deleteSavedSecret, listSavedSecrets, type SavedSecret } from "@/stores/secretRequests";

const wsStore = useWorkspaceStore();
const byRepo = ref<Record<number, SavedSecret[]>>({});
const error = ref("");

// A project is the repo root workspace (parent_id ?? id).
const roots = computed(() => wsStore.workspaces.filter((w) => !w.parent_id));
const rows = computed(() =>
  roots.value.map((ws) => ({ ws, secrets: byRepo.value[ws.id] ?? [] })).filter((r) => r.secrets.length),
);

async function load() {
  const next: Record<number, SavedSecret[]> = {};
  for (const ws of roots.value) {
    try {
      next[ws.id] = await listSavedSecrets(ws.id);
    } catch (e) {
      error.value = String(e);
    }
  }
  byRepo.value = next;
}

async function remove(repoId: number, name: string) {
  error.value = "";
  try {
    await deleteSavedSecret(repoId, name);
  } catch (e) {
    error.value = String(e);
  }
  await load();
}

const fmt = (ms: number) => (ms ? new Date(ms).toLocaleDateString() : "—");

onMounted(load);
watch(() => roots.value.map((w) => w.id).join(","), load);
</script>
