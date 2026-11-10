<template>
  <div class="gd select-text">
    <div v-if="message" class="grid min-h-16 place-items-center px-6 text-center text-[12.5px] text-muted-foreground">{{ message }}</div>
    <div v-else-if="held" class="flex items-center justify-center gap-2 py-3 text-[12.5px] text-muted-foreground">
      Large diff · {{ lineCount }} lines
      <button class="gd-btn" @click="showAll()">Show</button>
    </div>
    <div v-show="!message && !held" ref="host" />
  </div>
</template>

<script setup lang="ts">
// Unified diff text → @pierre/diffs (Shiki-highlighted, rendered outside Vue). Hunk indices are
// in order of `@@` headers, so for a one-file diff they line up with `splitHunks(text)[i]`
// from @/lib/gitHunks — the per-hunk actions ride on a line annotation under each hunk's last line.
import { onBeforeUnmount, onMounted, ref, watch } from "vue";
import { DIFFS_TAG_NAME, FileDiff, parsePatchFiles, type DiffLineAnnotation, type FileDiffMetadata } from "@pierre/diffs";
import { useUIStore } from "@/stores/ui";

const props = defineProps<{
  text: string;
  path?: string;
  hunkActions?: boolean;
  /** with hunkActions: the diff is of the index (`--cached`) → offer Unstage instead of Stage/Discard */
  staged?: boolean;
}>();
const emit = defineEmits<{
  "stage-hunk": [i: number];
  "unstage-hunk": [i: number];
  "discard-hunk": [i: number];
}>();

const ui = useUIStore();
const host = ref<HTMLElement | null>(null);
const message = ref("");
// ponytail: a diff over LINE_CAP lines waits for a click instead of rendering; VirtualizedFileDiff if huge single files matter
const LINE_CAP = 3000;
const held = ref(false);
let released = false;
const lineCount = ref(0);
let instances: FileDiff<number>[] = [];

function cleanUp() {
  for (const inst of instances) inst.cleanUp();
  instances = [];
  if (host.value) host.value.textContent = "";
}

/** one annotation per hunk, under its last line; metadata = global hunk index */
function hunkAnnotations(f: FileDiffMetadata, offset: number): DiffLineAnnotation<number>[] {
  return f.hunks.map((h, i) =>
    h.additionCount > 0
      ? { side: "additions", lineNumber: h.additionStart + h.additionCount - 1, metadata: offset + i }
      : { side: "deletions", lineNumber: h.deletionStart + h.deletionCount - 1, metadata: offset + i });
}

function button(label: string, onClick: () => void): HTMLButtonElement {
  const b = document.createElement("button");
  b.type = "button";
  b.textContent = label;
  b.className = "gd-btn";
  b.addEventListener("click", (e) => { e.stopPropagation(); onClick(); });
  return b;
}

function renderHunkActions(a: DiffLineAnnotation<number>): HTMLElement {
  const i = a.metadata!;
  const bar = document.createElement("div");
  bar.style.cssText = "display:flex;justify-content:flex-end;gap:6px;padding:4px 8px;font-family:var(--font-sans,system-ui)";
  if (props.staged) bar.append(button("− Unstage hunk", () => emit("unstage-hunk", i)));
  else bar.append(button("↺ Discard", () => emit("discard-hunk", i)), button("+ Stage hunk", () => emit("stage-hunk", i)));
  return bar;
}

function render() {
  cleanUp();
  message.value = "";
  if (!host.value) return;
  if (!props.text.trim()) { message.value = "Empty diff."; return; }

  let files: FileDiffMetadata[];
  try {
    files = parsePatchFiles(props.text, `gitdiff-${props.path ?? "multi"}`).flatMap((p) => p.files);
  } catch {
    message.value = "Could not parse diff.";
    return;
  }
  if (!files.length || files.every((f) => !f.hunks.length)) {
    message.value = /^Binary files /m.test(props.text) ? "Binary file, no preview."
      : files[0]?.prevName ? "Renamed only, content unchanged." : "No content changes.";
    return;
  }

  lineCount.value = files.reduce((n, f) => n + f.hunks.reduce((m, h) => m + h.unifiedLineCount, 0), 0);
  held.value = !released && lineCount.value > LINE_CAP;
  if (held.value) return;

  let offset = 0;
  for (const fileDiff of files) {
    const fileContainer = document.createElement(DIFFS_TAG_NAME);
    host.value.appendChild(fileContainer);
    const inst = new FileDiff<number>({
      theme: ui.activeTheme.shiki,
      diffStyle: "unified",
      overflow: "wrap",
      expansionLineCount: 10,
      // minified bundles: skip highlighting absurd lines instead of stalling Shiki
      tokenizeMaxLineLength: 1000,
      disableFileHeader: files.length === 1 && !!props.path,
      renderAnnotation: props.hunkActions ? renderHunkActions : undefined,
    });
    inst.render({ fileDiff, fileContainer, lineAnnotations: props.hunkActions ? hunkAnnotations(fileDiff, offset) : [] });
    instances.push(inst);
    offset += fileDiff.hunks.length;
  }
}

function showAll() { released = true; render(); }
onMounted(render);
watch(() => props.path, () => (released = false));
watch(() => [props.text, ui.activeTheme.shiki, props.staged, props.hunkActions], render, { flush: "post" });
onBeforeUnmount(cleanUp);
</script>

<style>
/* not scoped: the hunk buttons are created imperatively inside the diff element */
.gd-btn {
  display: inline-flex; align-items: center; gap: 4px; height: 24px; flex-shrink: 0; cursor: pointer;
  border-radius: 6px; padding: 0 8px; font-size: 11.5px; color: var(--text-secondary);
  background: color-mix(in srgb, var(--border) 30%, transparent);
}
.gd-btn:hover { background: var(--bg-hover); color: var(--text-primary); }
</style>
