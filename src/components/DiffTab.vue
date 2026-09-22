<template>
  <div class="flex h-full w-full flex-col overflow-hidden bg-base">
    <div class="flex shrink-0 items-center gap-1.5 border-b border-border bg-hover px-3 py-1.5 text-[11px]">
      <span class="flex-1 truncate font-mono text-foreground">{{ title }}</span>
      <span class="shrink-0 text-muted-foreground">{{ diffStaged ? "staged" : "unstaged" }}</span>
      <button v-if="instances.length > 1" class="shrink-0 rounded border border-border bg-transparent px-1.5 py-0.5 text-[10px] text-secondary-foreground hover:bg-hover hover:text-foreground" @click="toggleAll">
        {{ allCollapsed ? "Expand all" : "Collapse all" }}
      </button>
      <button
        class="shrink-0 rounded border border-border bg-transparent px-1.5 py-0.5 text-[10px] text-secondary-foreground hover:bg-hover hover:text-foreground disabled:opacity-40"
        :disabled="pendingNotes.length === 0 || sendingBatch"
        @click="sendNotes"
      >
        {{ sendingBatch ? "Sending…" : `Send ${pendingNotes.length} note${pendingNotes.length === 1 ? "" : "s"}` }}
      </button>
    </div>
    <DiffFeedbackComposer
      v-if="showComposer"
      :selection="pendingLabel"
      :target-available="!!pendingSelection"
      :sending="addingNote"
      :status="composerStatus"
      @submit="addNote"
      @cancel="cancelComposer"
    />
    <div v-if="!diff" class="flex flex-1 items-center justify-center text-xs text-muted-foreground">No changes</div>
    <div v-else-if="parseError" class="flex flex-1 items-center justify-center text-xs text-muted-foreground">Could not parse diff</div>
    <div v-else ref="containerRef" class="min-h-0 flex-1 overflow-auto" />
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, watch } from "vue";
import {
  DIFFS_TAG_NAME,
  FileDiff,
  parsePatchFiles,
  type DiffLineAnnotation,
  type FileDiffMetadata,
  type SelectedLineRange,
} from "@pierre/diffs";
import { invoke } from "@tauri-apps/api/core";
import { useUIStore } from "@/stores/ui";
import DiffFeedbackComposer from "./DiffFeedbackComposer.vue";

interface DiffComment {
  id: number;
  ws_id: number;
  file: string;
  line: number;
  side: string;
  body: string;
  created_at: number;
  sent_at: number;
}

const ui = useUIStore();

const props = defineProps<{
  diffFile: string;
  diffStaged: boolean;
  diff: string;
  workspaceId?: number;
  sendBatchNotes?: (markdown: string) => Promise<boolean>;
}>();

const containerRef = ref<HTMLElement | null>(null);
const parseError = ref(false);
const title = ref(props.diffFile);
const allCollapsed = ref(false);

const notes = ref<DiffComment[]>([]);
const pendingNotes = computed(() => notes.value.filter((n) => !n.sent_at));
const sendingBatch = ref(false);
const batchStatus = ref("");

const showComposer = ref(false);
const addingNote = ref(false);
const pendingSelection = ref<{ fileDiff: FileDiffMetadata; range: SelectedLineRange } | null>(null);
const pendingLabel = computed(() => {
  const p = pendingSelection.value;
  if (!p) return "Select diff lines to add a note.";
  const lines = p.range.start === p.range.end ? `line ${p.range.start}` : `lines ${p.range.start}-${p.range.end}`;
  return `${p.fileDiff.name}:${lines}`;
});
const composerStatus = computed(() => batchStatus.value);

const instances = ref<FileDiff<DiffComment>[]>([]);

function toggleAll() {
  const next = !allCollapsed.value;
  allCollapsed.value = next;
  for (const inst of instances.value) {
    inst.setOptions({ ...inst.options, collapsed: next });
    void inst.rerender();
  }
}

function cleanUp() {
  for (const inst of instances.value) inst.cleanUp();
  instances.value = [];
  if (containerRef.value) containerRef.value.textContent = "";
}

async function loadNotes() {
  if (!props.workspaceId) {
    notes.value = [];
    return;
  }
  const rows = await invoke<DiffComment[] | null>("list_diff_comments", { wsId: props.workspaceId }).catch(() => []);
  notes.value = rows ?? [];
  applyAnnotations();
}

// Renders a gutter marker that expands to the note's body on click — the
// inline "gutter marker + expandable block" the plan asks for, using
// @pierre/diffs' own annotation slot instead of reimplementing line overlays.
function renderNoteAnnotation(annotation: DiffLineAnnotation<DiffComment>): HTMLElement {
  const note = annotation.metadata!;
  const wrap = document.createElement("div");
  wrap.className = "diff-note" + (note.sent_at ? " diff-note-sent" : "");

  const marker = document.createElement("button");
  marker.type = "button";
  marker.className = "diff-note-marker";
  marker.textContent = note.sent_at ? "✓" : "●";
  marker.title = note.sent_at ? "Sent" : "Pending note — click to view";

  const body = document.createElement("div");
  body.className = "diff-note-body";
  body.textContent = note.body;
  body.hidden = true;

  marker.addEventListener("click", (e) => {
    e.stopPropagation();
    body.hidden = !body.hidden;
  });

  wrap.appendChild(marker);
  wrap.appendChild(body);
  return wrap;
}

function applyAnnotations() {
  for (const inst of instances.value) {
    const name = inst.fileDiff?.name;
    if (!name) continue;
    const forFile: DiffLineAnnotation<DiffComment>[] = notes.value
      .filter((n) => n.file === name)
      .map((n) => ({ side: (n.side || "additions") as DiffLineAnnotation<DiffComment>["side"], lineNumber: n.line, metadata: n }));
    inst.setLineAnnotations(forFile);
    inst.rerender();
  }
}

function quoteFromRange(fileDiff: FileDiffMetadata, range: SelectedLineRange): string {
  const side = range.side ?? "additions";
  const endSide = range.endSide ?? side;
  if (side !== endSide) {
    // Cross-side ranges are rare (dragging across the unified gutter) — just
    // quote the start line rather than reconciling two different arrays.
    return sourceLine(fileDiff, side, range.start);
  }
  const source = side === "deletions" ? fileDiff.deletionLines : fileDiff.additionLines;
  const start = Math.max(0, range.start - 1);
  const end = Math.max(start, range.end - 1);
  return source.slice(start, end + 1).join("\n");
}

function sourceLine(fileDiff: FileDiffMetadata, side: "additions" | "deletions", lineNumber: number): string {
  const source = side === "deletions" ? fileDiff.deletionLines : fileDiff.additionLines;
  return source[Math.max(0, lineNumber - 1)] ?? "";
}

function cancelComposer() {
  showComposer.value = false;
  pendingSelection.value = null;
  batchStatus.value = "";
}

async function addNote(comment: string) {
  if (!pendingSelection.value || !props.workspaceId) return;
  addingNote.value = true;
  batchStatus.value = "";
  const { fileDiff, range } = pendingSelection.value;
  const quote = quoteFromRange(fileDiff, range);
  const body = quote ? "```\n" + quote + "\n```\n\n" + comment : comment;
  try {
    await invoke("add_diff_comment", {
      wsId: props.workspaceId,
      file: fileDiff.name,
      line: range.start,
      side: range.side ?? "additions",
      body,
    });
    await loadNotes();
    cancelComposer();
  } catch {
    batchStatus.value = "Could not add note.";
  } finally {
    addingNote.value = false;
  }
}

async function sendNotes() {
  if (pendingNotes.value.length === 0 || !props.sendBatchNotes) return;
  sendingBatch.value = true;
  batchStatus.value = "";
  const ids = pendingNotes.value.map((n) => n.id);
  try {
    const markdown = await invoke<string>("compose_diff_notes", { ids });
    const ok = await props.sendBatchNotes(markdown);
    if (ok) {
      await invoke("mark_diff_notes_sent", { ids });
      await loadNotes();
    }
    batchStatus.value = ok ? `Sent ${ids.length} note${ids.length === 1 ? "" : "s"}.` : "Could not send notes — no chat or terminal available.";
  } catch {
    batchStatus.value = "Could not send notes.";
  } finally {
    sendingBatch.value = false;
  }
}

function render() {
  parseError.value = false;
  cleanUp();
  if (!containerRef.value || !props.diff) return;

  let patches;
  try {
    patches = parsePatchFiles(props.diff, `diff-${props.diffFile}`);
  } catch {
    parseError.value = true;
    return;
  }

  const fileCount = patches.reduce((n, p) => n + p.files.length, 0);
  title.value = fileCount === 1
    ? (patches[0]?.files[0]?.name ?? props.diffFile)
    : props.diffFile;

  for (const patch of patches) {
    for (const fileDiff of patch.files) {
      const fileContainer = document.createElement(DIFFS_TAG_NAME);
      containerRef.value.appendChild(fileContainer);

      let instance!: FileDiff<DiffComment>;
      instance = new FileDiff<DiffComment>({
        theme: ui.activeTheme.shiki,
        diffStyle: "unified",
        expansionLineCount: 5,
        enableLineSelection: true,
        renderAnnotation: renderNoteAnnotation,
        onLineSelected: (range) => {
          if (!range) return;
          pendingSelection.value = { fileDiff, range };
          showComposer.value = true;
        },
        renderHeaderMetadata() {
          const btn = document.createElement("button");
          btn.className = "collapse-btn";
          btn.textContent = instance?.options.collapsed ? "▶" : "▼";
          btn.addEventListener("click", () => {
            const next = !instance.options.collapsed;
            instance.setOptions({ ...instance.options, collapsed: next });
            void instance.rerender();
          });
          return btn;
        },
      });
      instance.render({ fileDiff, fileContainer });
      instances.value.push(instance);
    }
  }

  applyAnnotations();
}

onMounted(() => {
  render();
  loadNotes();
});
watch(() => props.diff, () => render());
watch(() => props.workspaceId, () => loadNotes());
// Re-render with the new syntax theme when the app theme changes.
watch(() => ui.activeTheme.shiki, () => render());
onBeforeUnmount(() => {
  cleanUp();
});
</script>

<style scoped>
.diff-note {
  display: inline-flex;
  align-items: flex-start;
  gap: 4px;
}
.diff-note-marker {
  border: none;
  background: transparent;
  color: var(--accent);
  cursor: pointer;
  font-size: 10px;
  line-height: 1;
  padding: 0 2px;
}
.diff-note-sent .diff-note-marker {
  color: var(--muted-foreground);
  opacity: 0.6;
}
.diff-note-body {
  white-space: pre-wrap;
  font-family: var(--font-mono, monospace);
  font-size: 11px;
  padding: 4px 6px;
  margin: 2px 0;
  border: 1px solid var(--border);
  border-radius: 4px;
  background: var(--hover);
  color: var(--foreground);
}
.diff-note-sent .diff-note-body {
  opacity: 0.55;
}
</style>
