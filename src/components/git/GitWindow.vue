<template>
  <div class="flex h-full overflow-hidden bg-base text-foreground">
    <GitSidebar ref="sidebar" v-model:ws-id="selectedWsId" />

    <div class="flex min-w-0 flex-1 flex-col">
      <header class="flex h-[46px] shrink-0 items-center gap-3 border-b border-border bg-panel px-3">
        <div class="flex rounded-lg bg-base p-0.5">
          <button
            v-for="v in TABS"
            :key="v"
            class="relative flex h-7 items-center gap-1.5 rounded-md px-3 text-[12.5px] font-medium transition-colors"
            :class="tab === v ? 'bg-hover text-foreground shadow-sm' : 'text-muted-foreground hover:text-secondary-foreground'"
            :title="v === 'history' ? 'History (⌘1)' : 'Changes (⌘2)'"
            @click="tab = v"
          >
            {{ v === "history" ? "History" : "Changes" }}
            <span
              v-if="v === 'changes' && changes > 0"
              class="rounded-full px-1.5 text-[10.5px] leading-4 tabular-nums"
              :class="git.conflicted.length ? 'bg-destructive text-white' : 'bg-accent text-[var(--bg-base)]'"
            >{{ changes }}</span>
          </button>
        </div>
        <div class="min-w-0 flex-1" />
        <GitToolbar @new-branch="sidebar?.startNewBranch()" />
      </header>

      <div v-if="!cwd" class="grid flex-1 place-items-center text-[12.5px] text-muted-foreground">Select a workspace.</div>
      <div v-else-if="git.error && !git.branch" class="grid flex-1 place-items-center text-[12.5px] text-muted-foreground">
        <div class="text-center">
          <div>Not a git repository.</div>
          <button class="mt-2 rounded-md px-2.5 py-1 text-foreground hover:bg-hover" @click="git.gitInit()">git init</button>
        </div>
      </div>
      <template v-else>
        <GitHistory v-show="tab === 'history'" :cwd="cwd" class="min-h-0 flex-1" @open-changes="tab = 'changes'" />
        <GitChanges v-show="tab === 'changes'" :cwd="cwd" class="min-h-0 flex-1" />
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useGitStore } from "@/stores/git";
import { useWorkspaceStore } from "@/stores/workspace";
import GitSidebar from "./GitSidebar.vue";
import GitToolbar from "./GitToolbar.vue";
import GitHistory from "./GitHistory.vue";
import GitChanges from "./GitChanges.vue";

const props = defineProps<{ initialWsId?: number }>();

const TABS = ["history", "changes"] as const;
const git = useGitStore();
const wsStore = useWorkspaceStore();
const sidebar = ref<InstanceType<typeof GitSidebar> | null>(null);
const tab = ref<(typeof TABS)[number]>("history");

// Same selection mechanism as GitPanel: the store holds one active cwd.
const selectedWsId = ref<number | null>(props.initialWsId ?? wsStore.active?.id ?? null);
const selectedWs = computed(() => wsStore.workspaces.find((w) => w.id === selectedWsId.value) ?? null);
const cwd = computed(() => git.cwd);
watch(selectedWs, (w) => { if (w) git.setCwd(w.path); }, { immediate: true });
watch(() => wsStore.active?.id, (id) => { if (id != null) selectedWsId.value = id; });

const changes = computed(() => git.staged.length + git.unstaged.length + git.untracked.length + git.conflicted.length);

function onKey(e: KeyboardEvent) {
  if (!e.metaKey || e.shiftKey || e.altKey) return;
  if (e.key === "1") tab.value = "history";
  else if (e.key === "2") tab.value = "changes";
  else if (e.key.toLowerCase() === "r") void git.refresh();
  else return;
  e.preventDefault();
}
// Coming back to the window after working in a terminal: pick up what changed.
const onFocus = () => { if (git.cwd) void git.refresh(true); };
onMounted(() => { window.addEventListener("keydown", onKey); window.addEventListener("focus", onFocus); });
onBeforeUnmount(() => { window.removeEventListener("keydown", onKey); window.removeEventListener("focus", onFocus); });
</script>
