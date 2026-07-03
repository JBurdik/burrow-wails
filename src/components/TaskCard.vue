<template>
  <div class="tc-card" :class="{ 'tc-dragging': dragging }" @click="emit('open')">
    <div class="tc-head">
      <span class="tc-status status-dot" :class="`status-${status}`">{{ status === 'running' ? spinnerFrame : '' }}</span>
      <span class="tc-title">{{ task.title || 'Untitled task' }}</span>
    </div>
    <p v-if="task.description" class="tc-desc">{{ truncatedDesc }}</p>
    <div class="tc-thumbs" v-if="attachments.length">
      <img v-for="a in attachments.slice(0, 4)" :key="a.id" :src="thumbs[a.id] ?? ''" class="tc-thumb" />
      <span v-if="attachments.length > 4" class="tc-thumb-more">+{{ attachments.length - 4 }}</span>
    </div>
    <div class="tc-badges">
      <span v-if="task.agent_kind" class="tc-badge tc-badge-agent">{{ task.agent_kind }}</span>
      <span v-if="task.model" class="tc-badge">{{ shortModel(task.model) }}</span>
      <span v-if="task.worktree_branch" class="tc-badge tc-badge-branch" :title="task.worktree_branch">
        <PhGitBranch :size="10" weight="bold" />{{ shortBranch(task.worktree_branch) }}
      </span>
      <span v-else-if="task.board_column !== 'backlog' && !task.use_worktree" class="tc-badge tc-badge-branch">
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

<style scoped>
.tc-card {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 10px 11px;
  border-radius: 9px;
  background: var(--bg-panel, #16161a);
  border: 1px solid var(--border, rgba(255, 255, 255, 0.09));
  cursor: pointer;
  transition: border-color 0.12s, background 0.12s;
}
.tc-card:hover {
  border-color: var(--accent, #3b82f6);
  background: var(--bg-hover, rgba(255, 255, 255, 0.04));
}
.tc-dragging {
  opacity: 0.4;
}
.tc-head {
  display: flex;
  align-items: center;
  gap: 7px;
  min-width: 0;
}
.tc-status {
  flex-shrink: 0;
}
.tc-status.status-running {
  font-size: 11px;
}
.tc-title {
  font-size: 12.5px;
  font-weight: 600;
  color: var(--text-primary, #e2e8f0);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
}
.tc-desc {
  margin: 0;
  font-size: 11px;
  line-height: 1.4;
  color: var(--text-muted, #8b93a5);
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.tc-thumbs {
  display: flex;
  gap: 4px;
  align-items: center;
}
.tc-thumb {
  width: 28px;
  height: 28px;
  object-fit: cover;
  border-radius: 4px;
  border: 1px solid var(--border, rgba(255, 255, 255, 0.1));
}
.tc-thumb-more {
  font-size: 10px;
  color: var(--text-muted, #64748b);
}
.tc-badges {
  display: flex;
  gap: 5px;
  flex-wrap: wrap;
}
.tc-badge {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  font-size: 9.5px;
  font-weight: 600;
  padding: 2px 6px;
  border-radius: 5px;
  background: rgba(255, 255, 255, 0.06);
  color: var(--text-secondary, #94a3b8);
  text-transform: uppercase;
  letter-spacing: 0.03em;
}
.tc-badge-agent {
  color: #a78bfa;
  background: rgba(167, 139, 250, 0.12);
}
.tc-badge-branch {
  text-transform: none;
  color: #4ade80;
  background: rgba(74, 222, 128, 0.1);
}
</style>
