<template>
  <div class="diff-tab">
    <div class="diff-tab-header">
      <span class="diff-tab-title">{{ title }}</span>
      <span class="diff-tab-mode">{{ diffStaged ? "staged" : "unstaged" }}</span>
      <button v-if="instances.length > 1" class="header-btn" @click="toggleAll">
        {{ allCollapsed ? "Expand all" : "Collapse all" }}
      </button>
      <button class="header-btn" :disabled="!feedbackTargetPtyId" @click="showFeedback = !showFeedback">
        {{ showFeedback ? "Close comment" : "Comment" }}
      </button>
    </div>
    <DiffFeedbackComposer
      v-if="showFeedback"
      :selection="selectedDiffText"
      :target-available="!!feedbackTargetPtyId"
      :sending="sendingFeedback"
      :status="feedbackStatus"
      @submit="sendFeedback"
      @cancel="showFeedback = false"
    />
    <div v-if="!diff" class="diff-empty">No changes</div>
    <div v-else-if="parseError" class="diff-empty">Could not parse diff</div>
    <div v-else ref="containerRef" class="diff-tab-body" />
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, watch } from "vue";
import {
  DIFFS_TAG_NAME,
  FileDiff,
  parsePatchFiles,
} from "@pierre/diffs";
import { useUIStore } from "@/stores/ui";
import DiffFeedbackComposer from "./DiffFeedbackComposer.vue";

const ui = useUIStore();

const props = defineProps<{
  diffFile: string;
  diffStaged: boolean;
  diff: string;
  feedbackTargetPtyId?: number;
  sendFeedback?: (payload: { comment: string; selectedDiff: string }) => Promise<boolean>;
}>();

const containerRef = ref<HTMLElement | null>(null);
const parseError = ref(false);
const title = ref(props.diffFile);
const allCollapsed = ref(false);
const showFeedback = ref(false);
const selectedDiffText = ref("");
const sendingFeedback = ref(false);
const feedbackStatus = ref(props.feedbackTargetPtyId ? "" : "This diff is not associated with an agent terminal.");

const instances = ref<FileDiff[]>([]);

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

function captureDiffSelection() {
  const selection = window.getSelection();
  if (!selection || selection.isCollapsed || !containerRef.value) return;
  const anchor = selection.anchorNode;
  const focus = selection.focusNode;
  if (!anchor || !focus || !containerRef.value.contains(anchor) || !containerRef.value.contains(focus)) return;
  selectedDiffText.value = selection.toString().trim().slice(0, 4_000);
}

async function sendFeedback(comment: string) {
  if (!props.feedbackTargetPtyId || !props.sendFeedback) return;
  sendingFeedback.value = true;
  feedbackStatus.value = "";
  let success = false;
  try {
    success = await props.sendFeedback({ comment, selectedDiff: selectedDiffText.value });
  } catch {
    success = false;
  }
  sendingFeedback.value = false;
  feedbackStatus.value = success ? "Feedback sent to the originating agent." : "Could not send feedback to the agent.";
  if (success) showFeedback.value = false;
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

      let instance!: FileDiff;
      instance = new FileDiff({
        theme: ui.activeTheme.shiki,
        diffStyle: "unified",
        expansionLineCount: 5,
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
}

onMounted(() => {
  render();
  document.addEventListener("selectionchange", captureDiffSelection);
});
watch(() => props.diff, () => render());
// Re-render with the new syntax theme when the app theme changes.
watch(() => ui.activeTheme.shiki, () => render());
onBeforeUnmount(() => {
  document.removeEventListener("selectionchange", captureDiffSelection);
  cleanUp();
});

</script>

<style scoped>
.diff-tab {
  display: flex;
  flex-direction: column;
  overflow: hidden;
  width: 100%;
  height: 100%;
  background: var(--bg-base);
}

.diff-tab-header {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 12px;
  background: var(--bg-hover);
  flex-shrink: 0;
  border-bottom: 1px solid var(--border);
  font-size: 11px;
}

.diff-tab-title {
  font-family: var(--font-mono);
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
}

.diff-tab-mode {
  color: var(--text-muted);
  flex-shrink: 0;
}

.header-btn {
  background: none;
  border: 1px solid var(--border);
  border-radius: 3px;
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 10px;
  padding: 2px 7px;
  flex-shrink: 0;
}
.header-btn:hover { color: var(--text-primary); background: var(--bg-hover); }

.diff-tab-body {
  flex: 1;
  overflow: auto;
  min-height: 0;
}

.diff-empty {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--text-muted);
  font-size: 12px;
}
</style>
