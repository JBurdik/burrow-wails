<template>
  <div class="fixed bottom-0 left-0 right-0 top-[var(--titlebar-height)] z-[950] flex items-center justify-center bg-black/55" @click.self="emit('close')">
    <div class="flex max-h-[86vh] w-[720px] max-w-[92vw] flex-col overflow-hidden rounded-xl border border-border bg-panel shadow-[0_20px_60px_rgba(0,0,0,0.5)]">
      <div class="flex shrink-0 items-center gap-2 border-b border-border px-3.5 py-3">
        <span class="status-dot shrink-0" :class="`status-${status}`">{{ status === 'running' ? spinnerFrame : '' }}</span>
        <input
          v-model="local.title"
          class="min-w-0 flex-1 border-0 bg-transparent font-sans text-sm font-semibold text-foreground outline-none"
          placeholder="Task title"
          @blur="persistMeta"
        />
        <span v-if="local.board_column !== 'backlog'" class="rounded-[5px] bg-blue-400/14 px-1.5 py-0.5 text-[9.5px] font-bold uppercase tracking-[0.04em] text-blue-400">{{ colLabel }}</span>
        <button class="flex h-6 w-6 items-center justify-center rounded-md bg-transparent text-muted-foreground hover:bg-hover hover:text-foreground" title="Close (Esc)" @click="emit('close')">
          <PhX :size="15" />
        </button>
      </div>

      <div class="flex min-h-0 flex-1 flex-col gap-3.5 overflow-y-auto p-3.5">
        <!-- Description -->
        <div class="flex flex-col gap-1.5">
          <label class="text-[10px] font-bold uppercase tracking-[0.05em] text-muted-foreground">Description</label>
          <textarea
            v-model="local.description"
            class="min-h-[80px] resize-y rounded-lg border border-border bg-white/4 px-2.5 py-2 font-sans text-[12.5px] leading-relaxed text-foreground outline-none focus:border-accent read-only:opacity-75"
            :readonly="local.board_column !== 'backlog'"
            rows="5"
            placeholder="What should the agent do? Paste or drop screenshots below."
            @blur="persistMeta"
            @paste="onPaste"
          />
        </div>

        <!-- Attachments -->
        <div
          class="flex flex-col gap-1.5"
          @dragover.prevent
          @drop.prevent="onDrop"
        >
          <label class="text-[10px] font-bold uppercase tracking-[0.05em] text-muted-foreground">Attachments</label>
          <div class="flex flex-wrap items-center gap-2">
            <div v-for="a in attachments" :key="a.id" class="relative">
              <img :src="thumbs[a.id] ?? ''" class="h-14 w-14 rounded-md border border-border object-cover" />
              <button v-if="local.board_column === 'backlog'" class="absolute -right-1.5 -top-1.5 flex h-4 w-4 items-center justify-center rounded-full border-0 bg-black/70 text-[11px] leading-none text-white" title="Remove" @click="removeAttachment(a.id)">×</button>
            </div>
            <label v-if="local.board_column === 'backlog'" class="flex h-14 w-14 cursor-pointer items-center justify-center rounded-md border border-dashed border-border text-muted-foreground hover:border-accent hover:text-accent">
              <PhImage :size="16" />
              <input type="file" accept="image/*" multiple class="hidden" @change="onFilePick" />
            </label>
            <span v-if="!attachments.length && local.board_column === 'backlog'" class="text-[11px] text-muted-foreground">Paste, drop, or pick images</span>
          </div>
        </div>

        <!-- Config (Backlog only) -->
        <div v-if="local.board_column === 'backlog'" class="flex flex-row flex-wrap gap-3">
          <div class="flex flex-col gap-1">
            <label class="text-[10px] font-bold uppercase tracking-[0.05em] text-muted-foreground">Model</label>
            <select v-model="local.model" class="rounded-lg border border-border bg-white/5 px-2 py-1.5 text-xs text-foreground">
              <option v-for="m in MODELS" :key="m.id" :value="m.id">{{ m.label }}</option>
            </select>
          </div>
          <div class="flex flex-col justify-end gap-1">
            <label class="text-[10px] font-bold uppercase tracking-[0.05em] text-muted-foreground">Worktree</label>
            <button
              class="inline-flex items-center gap-1.5 rounded-lg border border-border bg-transparent px-2.5 py-1.5 text-[11px] text-secondary-foreground"
              :class="{ 'text-green-400 border-green-400/40': !!local.use_worktree }"
              @click="local.use_worktree = local.use_worktree ? 0 : 1"
            >
              <PhTree v-if="local.use_worktree" :size="12" weight="bold" />
              <PhGitBranch v-else :size="12" weight="bold" />
              {{ local.use_worktree ? 'New worktree' : 'Current branch' }}
            </button>
          </div>
        </div>
        <div v-else class="flex flex-row flex-wrap items-center gap-3">
          <span class="inline-flex items-center gap-1 rounded-md bg-white/6 px-2 py-1 text-[10px] font-semibold text-secondary-foreground">{{ shortModel(local.model) }}</span>
          <span v-if="local.worktree_branch" class="inline-flex items-center gap-1 rounded-md bg-green-400/10 px-2 py-1 text-[10px] font-semibold text-green-400"><PhGitBranch :size="10" weight="bold" />{{ local.worktree_branch }}</span>
          <span v-else class="inline-flex items-center gap-1 rounded-md bg-green-400/10 px-2 py-1 text-[10px] font-semibold text-green-400"><PhGitBranch :size="10" weight="bold" />current branch</span>
        </div>

        <p v-if="startError" class="m-0 text-[11.5px] text-red-400">{{ startError }}</p>

        <!-- Live view (post-spawn only) -->
        <div v-if="local.board_column !== 'backlog' && livePtyId != null" class="flex flex-col gap-1.5 border-t border-border pt-3">
          <div class="flex items-center gap-2.5">
            <label class="text-[10px] font-bold uppercase tracking-[0.05em] text-muted-foreground">Live view</label>
          </div>
          <div class="td-chat-embed h-[440px] overflow-hidden rounded-lg border border-border">
            <TaskLiveTerm :pty-id="livePtyId" />
          </div>
        </div>
        <p v-else-if="local.board_column !== 'backlog'" class="m-0 text-[11.5px] text-muted-foreground">Waiting for terminal to spawn…</p>

        <!-- Per-turn Git trees, never the workspace-wide working diff. -->
        <div v-if="local.board_column !== 'backlog'" class="flex flex-col gap-1.5 border-t border-border">
          <label class="text-[10px] font-bold uppercase tracking-[0.05em] text-muted-foreground">Changes in turns</label>
          <p v-if="turnsLoading" class="my-1 text-xs text-muted-foreground">Loading changes…</p>
          <p v-else-if="!turns.length" class="my-1 text-xs text-muted-foreground">No completed turns yet.</p>
          <div v-for="turn in turns" :key="turn.id" class="flex items-start justify-between gap-2.5 border-b border-border/[.06] py-1.5 text-xs">
            <div>
              <strong>Turn {{ turn.id }}</strong>
              <span v-if="turn.changesAvailable"> · {{ turn.files.length }} files · <b class="text-[#55c48a]">+{{ turn.additions }}</b> <b class="text-[#ef7272]">-{{ turn.deletions }}</b></span>
              <span v-else> · changes unavailable</span>
              <details v-if="turn.changesAvailable && turn.files.length" class="mt-1 text-muted-foreground">
                <summary>Files</summary>
                <div v-for="file in turn.files" :key="file" class="pl-2 font-mono text-[11px]">{{ file }}</div>
              </details>
            </div>
            <button v-if="turn.changesAvailable && turn.completedAt" class="rounded-lg border-0 px-3.5 py-2 text-[12.5px] font-semibold" @click="openTurnDiff(turn.id)">Review</button>
          </div>
          <p v-if="turnError" class="m-0 text-[11.5px] text-red-400">{{ turnError }}</p>
        </div>

        <!-- Actions -->
        <div class="flex gap-2 pt-1">
          <button v-if="local.board_column === 'backlog'" class="ml-auto rounded-lg border-0 bg-accent px-3.5 py-2 text-[12.5px] font-semibold text-white disabled:cursor-default disabled:opacity-50" :disabled="starting || !local.title.trim()" @click="start">
            {{ starting ? 'Starting…' : 'Start' }}
          </button>
          <button
            v-if="local.board_column === 'for_review'"
            class="ml-auto rounded-lg border-0 bg-accent px-3.5 py-2 text-[12.5px] font-semibold text-white"
            @click="moveTo('done')"
          >
            Mark Done
          </button>
          <button class="rounded-lg border-0 bg-destructive/12 px-3.5 py-2 text-[12.5px] font-semibold text-red-400 hover:bg-destructive/20" @click="remove">Delete</button>
        </div>
      </div>
    </div>
    <div v-if="turnDiff" class="fixed inset-[8vh_8vw] z-[960] flex flex-col overflow-hidden rounded-[10px] border border-border bg-base shadow-[0_20px_60px_rgba(0,0,0,0.6)]">
      <div class="flex items-center justify-between border-b border-border px-3 py-2 text-xs">
        <span>{{ turnDiff.title }}</span>
        <button class="flex h-6 w-6 items-center justify-center rounded-md bg-transparent text-muted-foreground hover:bg-hover hover:text-foreground" @click="turnDiff = null"><PhX :size="15" /></button>
      </div>
      <DiffTab diff-file="turn changes" :diff-staged="false" :diff="turnDiff.diff" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from "vue";
import {
  PhX, PhImage, PhGitBranch, PhTree,
} from "@phosphor-icons/vue";
import { spinnerFrame } from "@/lib/spinner";
import { useTerminalTabsStore } from "@/stores/terminalTabs";
import {
  useBoardTasksStore, liveStatusForTask, BOARD_COLUMNS,
  type MissionTask, type BoardColumn, type AgentTurnChange,
} from "@/stores/boardTasks";
import TaskLiveTerm from "@/components/TaskLiveTerm.vue";
import DiffTab from "@/components/DiffTab.vue";

const props = defineProps<{ task: MissionTask; repoId: number }>();
const emit = defineEmits<{ close: []; deleted: [] }>();

const tabsStore = useTerminalTabsStore();
const board = useBoardTasksStore();

const MODELS = [
  { id: "claude-haiku-4-5-20251001", label: "Haiku 4.5" },
  { id: "claude-sonnet-5", label: "Sonnet 5" },
  { id: "claude-opus-4-8", label: "Opus 4.8" },
];

const local = reactive<MissionTask>({ ...props.task, model: props.task.model || "claude-sonnet-5" });
watch(() => props.task, (t) => Object.assign(local, t));

const status = computed(() => liveStatusForTask(local as MissionTask));
const colLabel = computed(() => BOARD_COLUMNS.find((c) => c.id === local.board_column)?.label ?? local.board_column);
// The tab spawned for this task, found by the taskId stamp on its leaf
// (tabsStore.tabsByWs mirrors every mounted workspace's tabs).
const livePtyId = computed(() => {
  const list = tabsStore.tabsByWs[local.task_workspace_id ?? -1] ?? [];
  return list.find((t) => t.taskId === local.id)?.id ?? null;
});

function shortModel(m?: string | null): string {
  if (!m) return "";
  if (m.includes("opus")) return "Opus";
  if (m.includes("sonnet")) return "Sonnet";
  if (m.includes("haiku")) return "Haiku";
  return m;
}

const attachments = computed(() => board.attachmentsByTask[local.id] || []);
const thumbs = reactive<Record<number, string>>({});
async function loadThumbs() {
  for (const a of attachments.value) {
    if (thumbs[a.id]) continue;
    try {
      const { base64, mime } = await board.readAttachmentBase64(a.id);
      thumbs[a.id] = `data:${mime};base64,${base64}`;
    } catch { /* best-effort */ }
  }
}
onMounted(async () => {
  await board.loadAttachments(local.id);
  await loadThumbs();
});
watch(attachments, loadThumbs);

const turns = ref<AgentTurnChange[]>([]);
const turnsLoading = ref(false);
const turnError = ref("");
const turnDiff = ref<{ title: string; diff: string } | null>(null);
async function loadTurns() {
  if (local.board_column === "backlog") return;
  turnsLoading.value = true;
  try { turns.value = await board.listTurnChanges(local.id); }
  catch { turnError.value = "Could not load turn changes."; }
  finally { turnsLoading.value = false; }
}
async function openTurnDiff(turnId: number) {
  turnError.value = "";
  try { turnDiff.value = { title: `Turn ${turnId} changes`, diff: await board.getTurnDiff(turnId) }; }
  catch { turnError.value = "Turn diff is unavailable."; }
}
watch(() => local.board_column, loadTurns, { immediate: true });

async function persistMeta() {
  if (local.board_column !== "backlog") return; // read-only editor once spawned
  await board.upsert({ ...local } as MissionTask);
}

// ── Attachments ──
function dataUrlToParts(dataUrl: string): { base64: string; mime: string } | null {
  const m = dataUrl.match(/^data:([^;]+);base64,(.*)$/);
  if (!m) return null;
  return { mime: m[1], base64: m[2] };
}
async function addImageDataUrl(dataUrl: string) {
  const parts = dataUrlToParts(dataUrl);
  if (!parts) return;
  await board.addAttachment(local.id, parts.base64, parts.mime);
}
async function addImageFile(file: File) {
  const dataUrl: string = await new Promise((resolve, reject) => {
    const r = new FileReader();
    r.onload = () => resolve(r.result as string);
    r.onerror = reject;
    r.readAsDataURL(file);
  });
  await addImageDataUrl(dataUrl);
}
function onPaste(e: ClipboardEvent) {
  if (local.board_column !== "backlog") return;
  const items = e.clipboardData?.items;
  if (!items) return;
  for (const item of Array.from(items)) {
    if (item.type.startsWith("image/")) {
      e.preventDefault();
      const file = item.getAsFile();
      if (file) addImageFile(file);
    }
  }
}
function onDrop(e: DragEvent) {
  if (local.board_column !== "backlog") return;
  const files = e.dataTransfer?.files;
  if (!files) return;
  for (const f of Array.from(files)) if (f.type.startsWith("image/")) addImageFile(f);
}
function onFilePick(e: Event) {
  const input = e.target as HTMLInputElement;
  for (const f of Array.from(input.files ?? [])) if (f.type.startsWith("image/")) addImageFile(f);
  input.value = "";
}
async function removeAttachment(id: number) {
  await board.removeAttachment(id, local.id);
}

// ── Backlog → Todo transition — shared with the `burrow board-move <id> todo`
// frontend handler in Terminal.vue via boardTasks.startTask() (docs/plans/
// mission-control-kanban.md §7), so a Manager-driven start behaves identically
// to clicking Start here. ──
const starting = ref(false);
const startError = ref("");

async function start() {
  if (starting.value || local.board_column !== "backlog") return;
  starting.value = true;
  startError.value = "";
  try {
    const saved = await board.startTask({ ...local } as MissionTask);
    Object.assign(local, saved);
  } catch (e) {
    startError.value = `Failed to start: ${e}`;
  } finally {
    starting.value = false;
  }
}

async function moveTo(col: BoardColumn) {
  await board.move(local.id, col, Date.now());
  local.board_column = col;
}

async function remove() {
  await board.remove(local.id);
  emit("deleted");
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === "Escape") emit("close");
}
onMounted(() => window.addEventListener("keydown", onKeydown));
</script>

<style scoped>
.td-chat-embed :deep(.claude-chat) { background: transparent; }
</style>
