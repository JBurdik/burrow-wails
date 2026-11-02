<script setup lang="ts">
import { computed, ref } from "vue";
import { PhCaretDown, PhCaretRight, PhCheck, PhGitDiff, PhGitPullRequest, PhPlugsConnected, PhRobot } from "@phosphor-icons/vue";
import { router } from "@/router";
import { useWorkspaceStore, type Workspace } from "@/stores/workspace";
import { useGitStore } from "@/stores/git";
import PhoneHeader from "./PhoneHeader.vue";
import PhoneSheet from "./PhoneSheet.vue";

const ws = useWorkspaceStore();
const git = useGitStore();
const picking = ref(false);

// The git store follows the active workspace (RightPanel stays mounted and sets
// its cwd), so its counts only belong to this header when the paths match.
const ownGit = computed(() => !!ws.active && git.cwd === ws.active.path);
const changes = computed(() => (ownGit.value ? git.staged.length + git.unstaged.length + git.untracked.length : 0));

// Repos with their worktrees right under them.
const choices = computed(() => {
  const out: { w: Workspace; depth: number }[] = [];
  for (const repo of ws.workspaces.filter((w) => !w.parent_id)) {
    out.push({ w: repo, depth: 0 });
    for (const wt of ws.workspaces.filter((w) => w.parent_id === repo.id)) out.push({ w: wt, depth: 1 });
  }
  return out;
});

function pick(w: Workspace) {
  ws.open(w);
  picking.value = false;
}

const rows = computed(() => [
  { surface: "git", label: "Changes", icon: PhGitDiff, detail: changes.value ? String(changes.value) : "" },
  { surface: "pull-requests", label: "Pull requests", icon: PhGitPullRequest, detail: "" },
  { surface: "dev-servers", label: "Dev servers", icon: PhPlugsConnected, detail: "" },
  { surface: "agents", label: "Sub-agents", icon: PhRobot, detail: "" },
]);
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col bg-base">
    <PhoneHeader title="Project" />
    <div class="flex-1 overflow-y-auto">
      <button class="flex min-h-[64px] w-full items-center gap-3 border-b border-border px-4 py-2 text-left active:bg-hover" @click="picking = true">
        <span class="flex min-w-0 flex-1 flex-col">
          <span class="truncate text-[16px] font-semibold text-foreground">{{ ws.active?.name ?? "Pick a project" }}</span>
          <span v-if="ownGit && git.branch" class="truncate font-mono text-[12px] text-muted-foreground">
            {{ git.branch }}<template v-if="git.ahead"> ↑{{ git.ahead }}</template><template v-if="git.behind"> ↓{{ git.behind }}</template>
          </span>
        </span>
        <PhCaretDown :size="16" class="shrink-0 text-muted-foreground" />
      </button>
      <template v-if="ws.active">
        <button
          v-for="r in rows"
          :key="r.surface"
          class="flex min-h-[52px] w-full items-center gap-3 border-b border-border/60 px-4 text-left active:bg-hover"
          @click="router.push(`/project/${r.surface}`)"
        >
          <component :is="r.icon" :size="20" class="shrink-0 text-secondary-foreground" />
          <span class="flex-1 text-[15px] text-foreground">{{ r.label }}</span>
          <span v-if="r.detail" class="text-[13px] text-muted-foreground">{{ r.detail }}</span>
          <PhCaretRight :size="15" class="shrink-0 text-muted-foreground" />
        </button>
      </template>
    </div>
    <PhoneSheet v-if="picking" title="Projects" @close="picking = false">
      <button
        v-for="c in choices"
        :key="c.w.id"
        class="flex min-h-[48px] w-full items-center gap-2 px-4 text-left active:bg-hover"
        :style="{ paddingLeft: `${16 + c.depth * 20}px` }"
        @click="pick(c.w)"
      >
        <span class="min-w-0 flex-1 truncate text-[15px]" :class="c.depth ? 'text-secondary-foreground' : 'text-foreground'">{{ c.w.name }}</span>
        <PhCheck v-if="ws.active?.id === c.w.id" :size="16" class="shrink-0 text-accent" />
      </button>
    </PhoneSheet>
  </div>
</template>
