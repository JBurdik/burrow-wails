<template>
  <div class="flex flex-1 flex-col items-center justify-center gap-10 overflow-y-auto bg-base p-10">
    <div class="flex flex-col items-center gap-2.5 text-center">
      <div class="flex items-center gap-2.5">
        <PhTerminalWindow :size="28" weight="duotone" class="text-accent" />
        <span class="text-[22px] font-semibold tracking-[-0.02em] text-foreground">Burrow</span>
      </div>
      <p class="text-[13px] text-secondary-foreground">Select or create a workspace to start</p>
    </div>

    <div class="flex w-[560px] flex-col gap-3">
      <div class="flex items-center justify-between">
        <span class="text-[11px] font-semibold uppercase tracking-[0.08em] text-muted-foreground">Recent Workspaces</span>
        <Button size="sm" @click="pickFolder"><PhFolderPlus :size="13" />New Workspace</Button>
      </div>

      <div class="flex max-h-[360px] flex-col gap-0.5 overflow-y-auto" v-if="store.workspaces.length">
        <div
          v-for="ws in store.workspaces"
          :key="ws.id"
          class="group flex cursor-pointer items-center gap-3 rounded-md border border-transparent px-3 py-2.5 transition-colors hover:border-border hover:bg-hover"
          @click="openWorkspace(ws)"
        >
          <PhFolder :size="20" weight="fill" class="shrink-0 text-blue-400" />
          <div class="min-w-0 flex-1">
            <div class="text-[13px] font-medium text-foreground">{{ ws.name }}</div>
            <div class="truncate font-mono text-[11px] text-secondary-foreground">{{ ws.path }}</div>
          </div>
          <div class="flex shrink-0 items-center gap-2">
            <span class="text-[11px] text-muted-foreground">{{ formatTime(ws.last_opened ?? ws.created_at) }}</span>
            <button class="flex items-center rounded p-1 text-muted-foreground opacity-0 transition-opacity hover:bg-destructive/10 hover:text-destructive group-hover:opacity-100" title="Remove" @click.stop="store.remove(ws.id)">
              <PhX :size="11" />
            </button>
          </div>
        </div>
      </div>

      <div class="flex flex-col items-center gap-3 rounded-lg border border-dashed border-border p-12 text-[13px] text-secondary-foreground" v-else>
        <PhFolderOpen :size="40" weight="thin" class="text-muted-foreground" />
        <p>No workspaces yet.</p>
        <Button size="sm" @click="pickFolder"><PhFolderPlus :size="13" />Open a folder</Button>
      </div>

      <div class="flex justify-end gap-2 pt-1">
        <Button variant="secondary" size="sm" @click="pickFolder"><PhFolderOpen :size="13" />Open Folder…</Button>
      </div>
    </div>

    <!-- Name dialog -->
    <div class="fixed inset-0 z-[100] flex items-center justify-center bg-black/60" v-if="pendingPath" @click.self="pendingPath = ''">
      <div class="flex w-[420px] flex-col gap-3.5 rounded-[10px] border border-border bg-panel p-6">
        <h3 class="text-[15px] font-semibold text-foreground">Name this workspace</h3>
        <p class="truncate rounded border border-border bg-base px-2.5 py-2 font-mono text-[11px] text-secondary-foreground">{{ pendingPath }}</p>
        <input
          v-model="pendingName"
          class="w-full rounded-md border border-border bg-base px-2.5 py-2 text-[13px] text-foreground outline-none focus:border-accent"
          placeholder="Workspace name"
          @keydown.enter="confirmCreate"
          @keydown.esc="pendingPath = ''"
          ref="nameInputEl"
        />
        <div class="flex justify-end gap-2">
          <Button variant="secondary" size="sm" @click="pendingPath = ''">Cancel</Button>
          <Button size="sm" @click="confirmCreate" :disabled="!pendingName.trim()">Create</Button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, nextTick, onMounted } from "vue";
import { PhTerminalWindow, PhFolder, PhFolderOpen, PhFolderPlus, PhX } from "@phosphor-icons/vue";
import { pickDir } from "@/lib/pickPath";
import { useWorkspaceStore, type Workspace } from "@/stores/workspace";
import { Button } from "@/components/ui/button";

const emit = defineEmits<{ open: [ws: Workspace] }>();
const store = useWorkspaceStore();

const pendingPath = ref("");
const pendingName = ref("");
const nameInputEl = ref<HTMLInputElement>();

onMounted(() => store.load());

async function pickFolder() {
  // In-app picker (PathPicker.vue) instead of the native panel — same browse UI
  // everywhere a folder is chosen, and it can create the folder too.
  const selected = await pickDir({ title: "Add project", start: "~/" });
  if (!selected) return;
  pendingPath.value = selected;
  pendingName.value = selected.split("/").pop() || selected;
  await nextTick();
  nameInputEl.value?.focus();
  nameInputEl.value?.select();
}

async function confirmCreate() {
  if (!pendingName.value.trim()) return;
  const ws = await store.create(pendingName.value.trim(), pendingPath.value);
  pendingPath.value = "";
  pendingName.value = "";
  openWorkspace(ws);
}

async function openWorkspace(ws: Workspace) {
  await store.open(ws);
  emit("open", ws);
}

function formatTime(ts: number): string {
  const diff = Date.now() - ts * 1000;
  if (diff < 60_000) return "just now";
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`;
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h ago`;
  return new Date(ts * 1000).toLocaleDateString();
}
</script>
