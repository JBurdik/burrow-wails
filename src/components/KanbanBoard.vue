<template>
  <div class="fixed bottom-0 left-0 right-0 top-[var(--titlebar-height)] z-[900] flex flex-col overflow-hidden bg-base [backdrop-filter:var(--blur-overlay,none)]">
    <div class="flex shrink-0 items-center justify-between border-b border-border px-4 py-3">
      <div class="flex items-center gap-2">
        <PhKanban :size="15" class="text-accent" />
        <span class="text-[13px] font-semibold text-foreground">Board</span>
        <span class="ml-1 text-[11px] text-muted-foreground" :title="repoPath">{{ repoName }}</span>
      </div>
      <div class="flex items-center gap-1">
        <button class="flex h-[26px] w-[26px] items-center justify-center rounded-md bg-transparent text-muted-foreground hover:bg-hover hover:text-foreground" title="Refresh" @click="refresh">
          <PhArrowClockwise :size="14" />
        </button>
        <button class="flex h-[26px] w-[26px] items-center justify-center rounded-md bg-transparent text-muted-foreground hover:bg-hover hover:text-foreground" title="Close (Esc)" @click="emit('close')">
          <PhX :size="15" />
        </button>
      </div>
    </div>

    <div class="flex min-h-0 flex-1 gap-3 overflow-x-auto px-4 py-3.5">
      <div
        v-for="col in BOARD_COLUMNS"
        :key="col.id"
        class="flex min-h-0 min-w-[220px] max-w-[320px] flex-1 basis-[240px] flex-col rounded-[10px] border border-border bg-panel"
        :class="{ 'border-accent bg-accent/5': overCol === col.id }"
        @dragover.prevent="overCol = col.id"
        @dragleave="onColLeave(col.id)"
        @drop="onDrop(col.id, $event)"
      >
        <div class="flex shrink-0 items-center gap-1.5 px-2.5 pb-2 pt-2.5">
          <span class="text-[11.5px] font-bold uppercase tracking-wide text-secondary-foreground">{{ col.label }}</span>
          <span class="rounded-lg bg-white/6 px-1.5 py-0.5 text-[10px] text-muted-foreground">{{ tasksInCol(col.id).length }}</span>
          <button v-if="col.id === 'backlog'" class="ml-auto flex h-[18px] w-[18px] items-center justify-center rounded bg-transparent text-muted-foreground hover:bg-hover hover:text-foreground" title="New task" @click="createTask">
            <PhPlus :size="12" weight="bold" />
          </button>
        </div>
        <div class="flex min-h-0 flex-1 flex-col gap-2 overflow-y-auto px-2 pb-2.5">
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
          <div v-if="!tasksInCol(col.id).length" class="py-4 text-center text-[11px] text-muted-foreground">No tasks</div>
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
