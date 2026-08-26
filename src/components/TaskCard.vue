<template>
  <div
    class="flex cursor-pointer flex-col gap-1.5 rounded-[9px] border border-border bg-panel p-2.5 transition-colors hover:border-accent hover:bg-hover"
    :class="{ 'opacity-40': dragging }"
    @click="emit('open')"
  >
    <div class="flex min-w-0 items-center gap-1.5">
      <span class="status-dot shrink-0" :class="`status-${status}`">{{ status === 'running' ? spinnerFrame : '' }}</span>
      <span class="min-w-0 truncate text-[12.5px] font-semibold text-foreground">{{ task.title || 'Untitled task' }}</span>
    </div>
    <p v-if="task.description" class="m-0 line-clamp-3 text-[11px] leading-snug text-muted-foreground">{{ truncatedDesc }}</p>
    <div class="flex items-center gap-1" v-if="attachments.length">
      <img v-for="a in attachments.slice(0, 4)" :key="a.id" :src="thumbs[a.id] ?? ''" class="h-7 w-7 rounded border border-border object-cover" />
      <span v-if="attachments.length > 4" class="text-[10px] text-muted-foreground">+{{ attachments.length - 4 }}</span>
    </div>
    <div class="flex flex-wrap gap-1">
      <span v-if="task.agent_kind" class="inline-flex items-center gap-0.5 rounded-[5px] bg-purple-400/12 px-1.5 py-0.5 text-[9.5px] font-semibold uppercase tracking-wide text-purple-400">{{ task.agent_kind }}</span>
      <span v-if="task.model" class="inline-flex items-center gap-0.5 rounded-[5px] bg-white/6 px-1.5 py-0.5 text-[9.5px] font-semibold uppercase tracking-wide text-secondary-foreground">{{ shortModel(task.model) }}</span>
      <span v-if="task.worktree_branch" class="inline-flex items-center gap-0.5 rounded-[5px] bg-green-400/10 px-1.5 py-0.5 text-[9.5px] font-semibold text-green-400" :title="task.worktree_branch">
        <PhGitBranch :size="10" weight="bold" />{{ shortBranch(task.worktree_branch) }}
      </span>
      <span v-else-if="task.board_column !== 'backlog' && !task.use_worktree" class="inline-flex items-center gap-0.5 rounded-[5px] bg-green-400/10 px-1.5 py-0.5 text-[9.5px] font-semibold text-green-400">
        <PhGitBranch :size="10" weight="bold" />current branch
      </span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, reactive, watch } from "vue";
import { PhGitBranch } from "@phosphor-icons/vue";
import { spinnerFrame } from "@/lib/spinner";
import { liveStatusForTask, useBoardTasksStore, type MissionTask, type TaskAttachment } from "@/stores/boardTasks";

const props = defineProps<{
  task: MissionTask;
  attachments: TaskAttachment[];
  dragging?: boolean;
}>();
const emit = defineEmits<{ open: [] }>();

const board = useBoardTasksStore();
const status = computed(() => liveStatusForTask(props.task));
const truncatedDesc = computed(() => {
  const d = (props.task.description || "").trim();
  return d.length > 140 ? d.slice(0, 140) + "…" : d;
});

function shortModel(m: string): string {
  if (m.includes("opus")) return "Opus";
  if (m.includes("sonnet")) return "Sonnet";
  if (m.includes("haiku")) return "Haiku";
  return m;
}
function shortBranch(b: string): string {
  return b.length > 22 ? b.slice(0, 22) + "…" : b;
}

// Thumbnails are staged as plain files on disk (§1.2 of the board plan) with no
// asset:// protocol scope configured — fetch + base64-encode via the existing
// read_task_attachment_base64 command instead, cached per attachment id.
const thumbs = reactive<Record<number, string>>({});
async function loadThumbs() {
  for (const a of props.attachments.slice(0, 4)) {
    if (thumbs[a.id]) continue;
    try {
      const { base64, mime } = await board.readAttachmentBase64(a.id);
      thumbs[a.id] = `data:${mime};base64,${base64}`;
    } catch { /* best-effort thumbnail */ }
  }
}
watch(() => props.attachments, loadThumbs, { immediate: true });
</script>
