<template>
  <div class="kb-page">
    <div class="kb-header">
      <div class="kb-head-title">
        <PhKanban :size="15" class="kb-head-icon" />
        <span class="kb-title">Board</span>
        <span class="kb-sub" :title="repoPath">{{ repoName }}</span>
      </div>
      <div class="kb-head-actions">
        <button class="kb-icon-btn" title="Refresh" @click="refresh">
          <PhArrowClockwise :size="14" />
        </button>
        <button class="kb-close" title="Close (Esc)" @click="emit('close')">
          <PhX :size="15" />
        </button>
      </div>
    </div>

    <div class="kb-columns">
      <div
        v-for="col in BOARD_COLUMNS"
        :key="col.id"
        class="kb-col"
        :class="{ 'kb-col-over': overCol === col.id }"
        @dragover.prevent="overCol = col.id"
        @dragleave="onColLeave(col.id)"
        @drop="onDrop(col.id, $event)"
      >
        <div class="kb-col-head">
          <span class="kb-col-title">{{ col.label }}</span>
          <span class="kb-col-count">{{ tasksInCol(col.id).length }}</span>
          <button v-if="col.id === 'backlog'" class="kb-col-add" title="New task" @click="createTask">
            <PhPlus :size="12" weight="bold" />
          </button>
        </div>
        <div class="kb-col-body">
          <div
            v-for="task in tasksInCol(col.id)"
            :key="task.id"
            draggable="true"
            @dragstart="onDragStart(task, $event)"
            @dragend="dragTaskId = null"
          >
            <TaskCard
              :task="task"
              :attachments="board.attachmentsByTask[task.id] || []"
              :dragging="dragTaskId === task.id"
              @open="openTask(task)"
            />
          </div>
          <div v-if="!tasksInCol(col.id).length" class="kb-col-empty">No tasks</div>
        </div>
      </div>
    </div>

    <TaskDetail
      v-if="selectedTask"
      :task="selectedTask"
      :repo-id="repoId"
      @close="selectedTask = null"
      @deleted="selectedTask = null"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { PhKanban, PhX, PhPlus, PhArrowClockwise } from "@phosphor-icons/vue";
import { useWorkspaceStore } from "@/stores/workspace";
import { useBoardTasksStore, BOARD_COLUMNS, newTaskId, type BoardColumn, type MissionTask } from "@/stores/boardTasks";
import TaskCard from "@/components/TaskCard.vue";
import TaskDetail from "@/components/TaskDetail.vue";

const props = defineProps<{ repoId: number }>();
const emit = defineEmits<{ close: [] }>();

const wsStore = useWorkspaceStore();
const board = useBoardTasksStore();

const repo = computed(() => wsStore.workspaces.find((w) => w.id === props.repoId));
const repoName = computed(() => repo.value?.name ?? "this repo");
const repoPath = computed(() => repo.value?.path ?? "");

const tasks = computed(() => board.tasksByRepo[props.repoId] ?? []);
function tasksInCol(col: BoardColumn): MissionTask[] {
  return tasks.value
    .filter((t) => t.board_column === col)
    .sort((a, b) => a.board_order - b.board_order);
}

async function refresh() {
  await board.load(props.repoId);
  await Promise.all(tasks.value.map((t) => board.loadAttachments(t.id)));
}

onMounted(() => {
  board.init();
  refresh();
});

const selectedTask = ref<MissionTask | null>(null);
function openTask(t: MissionTask) {
  selectedTask.value = t;
}

async function createTask() {
  const task: MissionTask = {
    id: newTaskId(),
    workspace_id: props.repoId,
    repo_workspace_id: props.repoId,
    board_column: "backlog",
    title: "New task",
    description: "",
    use_worktree: 1,
    board_order: Date.now(),
    created_at: Date.now(),
  };
  const saved = await board.upsert(task);
  selectedTask.value = saved;
}

// ── Drag & drop (native HTML5 DnD — no extra dependency) ──
const dragTaskId = ref<string | null>(null);
const overCol = ref<BoardColumn | null>(null);

function onDragStart(task: MissionTask, e: DragEvent) {
  dragTaskId.value = task.id;
  e.dataTransfer?.setData("text/plain", task.id);
  if (e.dataTransfer) e.dataTransfer.effectAllowed = "move";
}
function onColLeave(col: BoardColumn) {
  if (overCol.value === col) overCol.value = null;
}
async function onDrop(col: BoardColumn, e: DragEvent) {
  overCol.value = null;
  const taskId = e.dataTransfer?.getData("text/plain") || dragTaskId.value;
  dragTaskId.value = null;
  if (!taskId) return;
  const t = tasks.value.find((x) => x.id === taskId);
  if (!t) return;
  // Drop appends to the end of the target column — simple, predictable ordering.
  const siblings = tasksInCol(col).filter((x) => x.id !== taskId);
  const order = siblings.length ? siblings[siblings.length - 1].board_order + 1 : 0;
  await board.move(taskId, col, order);
}
</script>

<style scoped>
.kb-page {
  position: fixed;
  inset: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: var(--bg-base);
  backdrop-filter: var(--blur-overlay, none);
  -webkit-backdrop-filter: var(--blur-overlay, none);
  z-index: 900;
}

.kb-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  border-bottom: 1px solid var(--border, rgba(255, 255, 255, 0.08));
  flex-shrink: 0;
}
.kb-head-title {
  display: flex;
  align-items: center;
  gap: 8px;
}
.kb-head-icon { color: var(--accent, #3b82f6); }
.kb-title { font-size: 13px; font-weight: 600; color: var(--text-primary, #e2e8f0); }
.kb-sub { font-size: 11px; color: var(--text-muted, #64748b); margin-left: 4px; }
.kb-head-actions { display: flex; align-items: center; gap: 4px; }
.kb-icon-btn, .kb-close {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 26px;
  height: 26px;
  border: none;
  border-radius: 6px;
  background: transparent;
  color: var(--text-muted, #94a3b8);
  cursor: pointer;
}
.kb-icon-btn:hover, .kb-close:hover {
  background: var(--bg-hover, rgba(255, 255, 255, 0.08));
  color: var(--text-primary, #e2e8f0);
}

.kb-columns {
  flex: 1;
  min-height: 0;
  display: flex;
  gap: 12px;
  padding: 14px 16px;
  overflow-x: auto;
}
.kb-col {
  flex: 1 0 240px;
  min-width: 220px;
  max-width: 320px;
  display: flex;
  flex-direction: column;
  min-height: 0;
  border-radius: 10px;
  background: var(--bg-panel, rgba(255, 255, 255, 0.02));
  border: 1px solid var(--border, rgba(255, 255, 255, 0.06));
}
.kb-col-over {
  border-color: var(--accent, #3b82f6);
  background: rgba(59, 130, 246, 0.05);
}
.kb-col-head {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 10px 10px 8px;
  flex-shrink: 0;
}
.kb-col-title {
  font-size: 11.5px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--text-secondary, #94a3b8);
}
.kb-col-count {
  font-size: 10px;
  color: var(--text-muted, #64748b);
  background: rgba(255, 255, 255, 0.06);
  border-radius: 8px;
  padding: 1px 6px;
}
.kb-col-add {
  margin-left: auto;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 18px;
  height: 18px;
  border: none;
  border-radius: 5px;
  background: transparent;
  color: var(--text-muted, #94a3b8);
  cursor: pointer;
}
.kb-col-add:hover { background: var(--bg-hover, rgba(255, 255, 255, 0.08)); color: var(--text-primary, #e2e8f0); }
.kb-col-body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 0 8px 10px;
}
.kb-col-empty {
  font-size: 11px;
  color: var(--text-muted, #4b5563);
  text-align: center;
  padding: 16px 0;
}
</style>
