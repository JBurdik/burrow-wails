<template>
  <div class="flex items-center gap-0.5">
    <button :class="btn" :disabled="!git.cwd || git.fetching" title="Fetch (⌘⇧F)" @click="git.fetch()">
      <PhCircleNotch v-if="git.fetching" :size="14" class="animate-spin" />
      <PhArrowsClockwise v-else :size="14" />
      Fetch
    </button>
    <button
      :class="btn"
      :disabled="!git.hasUpstream || git.pulling || git.pushing"
      :title="git.hasUpstream ? 'Pull, fast-forward only (⌘⇧L)' : 'No upstream to pull from'"
      @click="git.pull()"
    >
      <PhCircleNotch v-if="git.pulling" :size="14" class="animate-spin" />
      <PhArrowLineDown v-else :size="14" />
      Pull
      <span v-if="git.behind" class="tabular-nums text-warning">{{ git.behind }}</span>
    </button>
    <button
      :class="btn"
      :disabled="!git.branch || git.pushing || git.pulling || (git.hasUpstream && !git.ahead)"
      :title="`${git.hasUpstream ? 'Push' : 'Push and set upstream'} (⌘⇧P)`"
      @click="git.push()"
    >
      <PhCircleNotch v-if="git.pushing" :size="14" class="animate-spin" />
      <PhArrowLineUp v-else :size="14" />
      Push
      <span v-if="git.ahead" class="tabular-nums text-success">{{ git.ahead }}</span>
    </button>
    <span class="mx-1.5 h-4 w-px bg-border" />
    <button :class="btn" :disabled="!git.hasWorkingTreeChanges || stashing" title="Stash all changes, incl. untracked (⌘⇧S)" @click="stash">
      <PhCircleNotch v-if="stashing" :size="14" class="animate-spin" />
      <PhArchiveBox v-else :size="14" />
      Stash
    </button>
    <button :class="btn" :disabled="!git.cwd" title="New branch (⌘⇧B)" @click="emit('new-branch')">
      <PhGitBranch :size="14" />
      Branch
    </button>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from "vue";
import {
  PhArchiveBox, PhArrowLineDown, PhArrowLineUp, PhArrowsClockwise, PhCircleNotch, PhGitBranch,
} from "@phosphor-icons/vue";
import { useGitStore } from "@/stores/git";

const emit = defineEmits<{ "new-branch": [] }>();
const git = useGitStore();

const btn =
  "inline-flex h-7 items-center justify-center gap-1.5 whitespace-nowrap rounded-md px-2.5 text-[12.5px] font-medium text-secondary-foreground transition-colors duration-100 hover:bg-hover hover:text-foreground disabled:pointer-events-none disabled:opacity-40";

const stashing = ref(false);
async function stash() {
  stashing.value = true;
  try { await git.stashPush(); } finally { stashing.value = false; }
}

function onKey(e: KeyboardEvent) {
  if (!e.metaKey || !e.shiftKey || e.altKey) return;
  const act: Record<string, () => unknown> = {
    f: () => git.fetch(),
    l: () => git.hasUpstream && git.pull(),
    p: () => git.push(),
    s: () => git.hasWorkingTreeChanges && stash(),
    b: () => emit("new-branch"),
  };
  const fn = act[e.key.toLowerCase()];
  if (fn) { e.preventDefault(); fn(); }
}
onMounted(() => window.addEventListener("keydown", onKey));
onBeforeUnmount(() => window.removeEventListener("keydown", onKey));
</script>
