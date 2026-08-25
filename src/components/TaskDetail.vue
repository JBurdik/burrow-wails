<template>
  <div class="td-backdrop" @click.self="emit('close')">
    <div class="td-panel">
      <div class="td-header">
        <span class="td-status status-dot" :class="`status-${status}`">{{ status === 'running' ? spinnerFrame : '' }}</span>
        <input
          v-model="local.title"
          class="td-title-input"
          placeholder="Task title"
          @blur="persistMeta"
        />
        <span v-if="local.board_column !== 'backlog'" class="td-col-badge">{{ colLabel }}</span>
        <button class="td-close" title="Close (Esc)" @click="emit('close')">
          <PhX :size="15" />
        </button>
      </div>

      <div class="td-body">
        <!-- Description -->
        <div class="td-section">
          <label class="td-label">Description</label>
          <textarea
            v-model="local.description"
            class="td-desc"
            :readonly="local.board_column !== 'backlog'"
            rows="5"
            placeholder="What should the agent do? Paste or drop screenshots below."
            @blur="persistMeta"
            @paste="onPaste"
          />
        </div>

        <!-- Attachments -->
        <div
          class="td-section"
          @dragover.prevent
          @drop.prevent="onDrop"
        >
          <label class="td-label">Attachments</label>
          <div class="td-attachments">
            <div v-for="a in attachments" :key="a.id" class="td-attach-wrap">
              <img :src="thumbs[a.id] ?? ''" class="td-attach-img" />
              <button v-if="local.board_column === 'backlog'" class="td-attach-rm" title="Remove" @click="removeAttachment(a.id)">×</button>
            </div>
            <label v-if="local.board_column === 'backlog'" class="td-attach-add">
              <PhImage :size="16" />
              <input type="file" accept="image/*" multiple class="td-attach-input" @change="onFilePick" />
            </label>
            <span v-if="!attachments.length && local.board_column === 'backlog'" class="td-attach-hint">Paste, drop, or pick images</span>
          </div>
        </div>

        <!-- Config (Backlog only) -->
        <div v-if="local.board_column === 'backlog'" class="td-section td-config">
          <div class="td-field">
            <label class="td-label">Model</label>
            <select v-model="local.model" class="td-select">
              <option v-for="m in MODELS" :key="m.id" :value="m.id">{{ m.label }}</option>
            </select>
          </div>
          <div class="td-field td-field-toggle">
            <label class="td-label">Worktree</label>
            <button class="td-toggle" :class="{ 'td-toggle-on': !!local.use_worktree }" @click="local.use_worktree = local.use_worktree ? 0 : 1">
              <PhTree v-if="local.use_worktree" :size="12" weight="bold" />
              <PhGitBranch v-else :size="12" weight="bold" />
              {{ local.use_worktree ? 'New worktree' : 'Current branch' }}
            </button>
          </div>
        </div>
        <div v-else class="td-section td-config td-config-readonly">
          <span class="td-badge">{{ shortModel(local.model) }}</span>
          <span v-if="local.worktree_branch" class="td-badge td-badge-branch"><PhGitBranch :size="10" weight="bold" />{{ local.worktree_branch }}</span>
          <span v-else class="td-badge td-badge-branch"><PhGitBranch :size="10" weight="bold" />current branch</span>
        </div>

        <p v-if="startError" class="td-error">{{ startError }}</p>

        <!-- Live view (post-spawn only) -->
        <div v-if="local.board_column !== 'backlog' && livePtyId != null" class="td-section td-live">
          <div class="td-live-head">
            <label class="td-label">Live view</label>
          </div>
          <div class="td-chat-embed">
            <TaskLiveTerm :pty-id="livePtyId" />
          </div>
        </div>
        <p v-else-if="local.board_column !== 'backlog'" class="td-live-pending">Waiting for terminal to spawn…</p>

        <!-- Per-turn Git trees, never the workspace-wide working diff. -->
        <div v-if="local.board_column !== 'backlog'" class="td-section td-turns">
          <label class="td-label">Changes in turns</label>
          <p v-if="turnsLoading" class="td-turn-hint">Loading changes…</p>
          <p v-else-if="!turns.length" class="td-turn-hint">No completed turns yet.</p>
          <div v-for="turn in turns" :key="turn.id" class="td-turn-row">
            <div>
              <strong>Turn {{ turn.id }}</strong>
              <span v-if="turn.changesAvailable"> · {{ turn.files.length }} files · <b class="add">+{{ turn.additions }}</b> <b class="del">-{{ turn.deletions }}</b></span>
              <span v-else> · changes unavailable</span>
              <details v-if="turn.changesAvailable && turn.files.length">
                <summary>Files</summary>
                <div v-for="file in turn.files" :key="file" class="td-turn-file">{{ file }}</div>
              </details>
            </div>
            <button v-if="turn.changesAvailable && turn.completedAt" class="td-btn" @click="openTurnDiff(turn.id)">Review</button>
          </div>
          <p v-if="turnError" class="td-error">{{ turnError }}</p>
        </div>

        <!-- Actions -->
        <div class="td-actions">
          <button v-if="local.board_column === 'backlog'" class="td-btn td-btn-primary" :disabled="starting || !local.title.trim()" @click="start">
            {{ starting ? 'Starting…' : 'Start' }}
          </button>
          <button
            v-if="local.board_column === 'for_review'"
            class="td-btn td-btn-primary"
            @click="moveTo('done')"
          >
            Mark Done
          </button>
          <button class="td-btn td-btn-danger" @click="remove">Delete</button>
        </div>
      </div>
    </div>
    <div v-if="turnDiff" class="td-diff-modal">
      <div class="td-diff-head"><span>{{ turnDiff.title }}</span><button class="td-close" @click="turnDiff = null"><PhX :size="15" /></button></div>
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
.td-backdrop {
  position: fixed;
  top: var(--titlebar-height);
  right: 0;
  bottom: 0;
  left: 0;
  background: rgba(0, 0, 0, 0.55);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 950;
}
.td-panel {
  width: 720px;
  max-width: 92vw;
  max-height: 86vh;
  display: flex;
  flex-direction: column;
  background: var(--bg-panel, #16161a);
  border: 1px solid var(--border, rgba(255, 255, 255, 0.12));
  border-radius: 12px;
  box-shadow: 0 20px 60px rgba(0, 0, 0, 0.5);
  overflow: hidden;
}
.td-turns { border-top: 1px solid var(--border, rgba(255,255,255,.08)); }
.td-turn-hint { margin: 4px 0; color: var(--text-muted); font-size: 12px; }
.td-turn-row { display: flex; justify-content: space-between; align-items: flex-start; gap: 10px; padding: 7px 0; font-size: 12px; border-bottom: 1px solid var(--border, rgba(255,255,255,.06)); }
.td-turn-row details { margin-top: 4px; color: var(--text-muted); }
.td-turn-file { font-family: var(--font-mono); font-size: 11px; padding-left: 8px; }
.add { color: #55c48a; }.del { color: #ef7272; }
.td-diff-modal { position: fixed; z-index: 960; inset: 8vh 8vw; display: flex; flex-direction: column; background: var(--bg-base); border: 1px solid var(--border); border-radius: 10px; overflow: hidden; box-shadow: 0 20px 60px rgba(0,0,0,.6); }
.td-diff-head { display: flex; justify-content: space-between; align-items: center; padding: 8px 12px; font-size: 12px; border-bottom: 1px solid var(--border); }
.td-header {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 12px 14px;
  border-bottom: 1px solid var(--border, rgba(255, 255, 255, 0.08));
  flex-shrink: 0;
}
.td-status { flex-shrink: 0; }
.td-status.status-running { font-size: 12px; }
.td-title-input {
  flex: 1;
  min-width: 0;
  background: transparent;
  border: none;
  outline: none;
  color: var(--text-primary, #e2e8f0);
  font-size: 14px;
  font-weight: 600;
  font-family: var(--font-ui);
}
.td-col-badge {
  font-size: 9.5px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  padding: 2px 7px;
  border-radius: 5px;
  background: rgba(59, 130, 246, 0.14);
  color: #60a5fa;
}
.td-close {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  border: none;
  border-radius: 6px;
  background: transparent;
  color: var(--text-muted, #94a3b8);
  cursor: pointer;
}
.td-close:hover { background: var(--bg-hover, rgba(255, 255, 255, 0.08)); color: var(--text-primary, #e2e8f0); }

.td-body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 14px;
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.td-section { display: flex; flex-direction: column; gap: 6px; }
.td-label {
  font-size: 10px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--text-muted, #64748b);
}
.td-desc {
  resize: vertical;
  min-height: 80px;
  background: rgba(255, 255, 255, 0.04);
  border: 1px solid var(--border, rgba(255, 255, 255, 0.1));
  border-radius: 8px;
  padding: 8px 10px;
  color: var(--text-primary, #e2e8f0);
  font-family: var(--font-ui);
  font-size: 12.5px;
  line-height: 1.5;
  outline: none;
}
.td-desc:focus { border-color: var(--accent, #3b82f6); }
.td-desc[readonly] { opacity: 0.75; }

.td-attachments { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; }
.td-attach-wrap { position: relative; }
.td-attach-img {
  width: 56px; height: 56px; object-fit: cover; border-radius: 6px;
  border: 1px solid var(--border, rgba(255, 255, 255, 0.12));
}
.td-attach-rm {
  position: absolute; top: -5px; right: -5px; width: 16px; height: 16px;
  border-radius: 50%; background: rgba(0,0,0,0.7); border: none; color: #fff;
  font-size: 11px; line-height: 1; cursor: pointer;
}
.td-attach-add {
  width: 56px; height: 56px; display: flex; align-items: center; justify-content: center;
  border: 1px dashed var(--border, rgba(255, 255, 255, 0.2)); border-radius: 6px;
  color: var(--text-muted, #64748b); cursor: pointer;
}
.td-attach-add:hover { border-color: var(--accent, #3b82f6); color: var(--accent, #3b82f6); }
.td-attach-input { display: none; }
.td-attach-hint { font-size: 11px; color: var(--text-muted, #64748b); }

.td-config { flex-direction: row; gap: 12px; flex-wrap: wrap; }
.td-config-readonly { align-items: center; }
.td-field { display: flex; flex-direction: column; gap: 4px; }
.td-field-toggle { justify-content: flex-end; }
.td-select {
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid var(--border, rgba(255, 255, 255, 0.12));
  border-radius: 7px;
  color: var(--text-primary, #e2e8f0);
  font-size: 12px;
  padding: 5px 8px;
}
.td-toggle {
  display: inline-flex; align-items: center; gap: 5px;
  border: 1px solid var(--border, rgba(255, 255, 255, 0.12));
  border-radius: 7px; background: transparent; color: var(--text-secondary, #94a3b8);
  font-size: 11px; padding: 5px 9px; cursor: pointer;
}
.td-toggle-on { color: #4ade80; border-color: rgba(74, 222, 128, 0.4); }

.td-badge {
  font-size: 10px; font-weight: 600; padding: 3px 8px; border-radius: 6px;
  background: rgba(255, 255, 255, 0.06); color: var(--text-secondary, #94a3b8);
  display: inline-flex; align-items: center; gap: 4px;
}
.td-badge-branch { color: #4ade80; background: rgba(74, 222, 128, 0.1); }

.td-error { color: #f87171; font-size: 11.5px; margin: 0; }

.td-live { border-top: 1px solid var(--border, rgba(255, 255, 255, 0.08)); padding-top: 12px; }
.td-live-pending { font-size: 11.5px; color: var(--text-muted, #64748b); margin: 0; }
.td-live-head { display: flex; align-items: center; gap: 10px; }
.td-chat-embed { height: 440px; border: 1px solid var(--border, rgba(255, 255, 255, 0.08)); border-radius: 8px; overflow: hidden; }
.td-chat-embed :deep(.claude-chat) { background: transparent; }

.td-actions { display: flex; gap: 8px; padding-top: 4px; }
.td-btn {
  border: none; border-radius: 8px; padding: 8px 14px; font-size: 12.5px; font-weight: 600;
  cursor: pointer;
}
.td-btn-primary { background: var(--accent, #3b82f6); color: #fff; margin-left: auto; }
.td-btn-primary:disabled { opacity: 0.5; cursor: default; }
.td-btn-danger { background: rgba(239, 68, 68, 0.12); color: #f87171; }
.td-btn-danger:hover { background: rgba(239, 68, 68, 0.2); }
</style>
