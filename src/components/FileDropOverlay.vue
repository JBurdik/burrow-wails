<template>
  <Transition name="drop-fade">
    <div v-if="active" class="file-drop-overlay">
      <div class="file-drop-card">Drop files to attach</div>
    </div>
  </Transition>
</template>

<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount } from "vue";

// Fullscreen hint while a file is dragged over the window. dragover fires
// continuously, so "gone quiet for 120 ms" (or a drop) is what hides it.
const active = ref(false);
let hideTimer: ReturnType<typeof setTimeout> | undefined;

function onDragOver(e: DragEvent) {
  if (!e.dataTransfer?.types.includes("Files")) return;
  active.value = true;
  clearTimeout(hideTimer);
  hideTimer = setTimeout(() => (active.value = false), 120);
}
function hide() {
  clearTimeout(hideTimer);
  active.value = false;
}

onMounted(() => {
  window.addEventListener("dragover", onDragOver);
  window.addEventListener("drop", hide);
});
onBeforeUnmount(() => {
  window.removeEventListener("dragover", onDragOver);
  window.removeEventListener("drop", hide);
  clearTimeout(hideTimer);
});
</script>

<style scoped>
.file-drop-overlay {
  position: fixed;
  inset: 0;
  z-index: 9999;
  display: flex;
  align-items: center;
  justify-content: center;
  background: color-mix(in srgb, var(--accent) 14%, rgb(0 0 0 / 0.6));
  backdrop-filter: blur(3px);
  pointer-events: none;
}
.file-drop-card {
  padding: 28px 44px;
  border: 2px dashed var(--accent);
  border-radius: 16px;
  color: var(--accent);
  font-size: 18px;
  font-weight: 600;
}
.drop-fade-enter-active, .drop-fade-leave-active { transition: opacity 0.12s; }
.drop-fade-enter-from, .drop-fade-leave-to { opacity: 0; }
</style>
