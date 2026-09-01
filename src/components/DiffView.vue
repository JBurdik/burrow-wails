<template>
  <div v-if="!diff" class="flex flex-1 items-center justify-center text-xs text-muted-foreground">No changes</div>
  <div v-else-if="parseError" class="flex flex-1 items-center justify-center text-xs text-muted-foreground">Could not parse diff</div>
  <div v-else ref="containerRef" class="min-h-0 flex-1 overflow-auto" />
</template>

<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, watch } from "vue";
import { DIFFS_TAG_NAME, FileDiff, parsePatchFiles } from "@pierre/diffs";
import { useUIStore } from "@/stores/ui";

const ui = useUIStore();

const props = defineProps<{
  diff: string;
  diffKey?: string;
}>();

const containerRef = ref<HTMLElement | null>(null);
const parseError = ref(false);
const instances: FileDiff[] = [];

function cleanUp() {
  for (const inst of instances) inst.cleanUp();
  instances.length = 0;
  if (containerRef.value) containerRef.value.textContent = "";
}

function render() {
  parseError.value = false;
  cleanUp();
  if (!containerRef.value || !props.diff) return;

  let patches;
  try {
    patches = parsePatchFiles(props.diff, `diffview-${props.diffKey ?? "inline"}`);
  } catch {
    parseError.value = true;
    return;
  }

  for (const patch of patches) {
    for (const fileDiff of patch.files) {
      const fileContainer = document.createElement(DIFFS_TAG_NAME);
      containerRef.value.appendChild(fileContainer);
      const instance = new FileDiff({ theme: ui.activeTheme.shiki, diffStyle: "unified", expansionLineCount: 5 });
      instance.render({ fileDiff, fileContainer });
      instances.push(instance);
    }
  }
}

onMounted(render);
watch(() => props.diff, render);
// Re-render with the new syntax theme when the app theme changes.
watch(() => ui.activeTheme.shiki, render);
onBeforeUnmount(cleanUp);
</script>
