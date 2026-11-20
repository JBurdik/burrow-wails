<template>
  <Transition name="archive-undo">
    <div v-if="archiveNotice" class="archive-undo" role="status" @mouseenter="pause" @mouseleave="arm">
      <span class="min-w-0 truncate">Archived <span class="font-medium text-foreground">{{ archiveNotice.title }}</span></span>
      <button class="archive-undo-btn" @click="undo">Undo</button>
    </div>
  </Transition>
</template>

<script setup lang="ts">
import { onUnmounted, watch } from "vue";
import { archiveNotice } from "@/lib/archiveFlow";

const SHOW_MS = 5000;
let timer: number | undefined;
function pause() { window.clearTimeout(timer); }
function arm() {
  pause();
  timer = window.setTimeout(() => (archiveNotice.value = null), SHOW_MS);
}
watch(archiveNotice, (n) => (n ? arm() : pause()));
onUnmounted(pause);
function undo() {
  const n = archiveNotice.value;
  archiveNotice.value = null;
  n?.undo();
}
</script>

<style scoped>
.archive-undo {
  position: absolute;
  top: 12px;
  left: 50%;
  transform: translateX(-50%);
  z-index: 40;
  display: flex;
  align-items: center;
  gap: 12px;
  max-width: min(480px, calc(100% - 32px));
  padding: 7px 8px 7px 14px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-dropdown, var(--bg-panel));
  color: var(--text-secondary);
  font-size: 12.5px;
  box-shadow: 0 8px 24px rgb(0 0 0 / 0.3);
}
.archive-undo-btn {
  flex-shrink: 0;
  padding: 3px 10px;
  border-radius: 6px;
  color: var(--accent);
  font-weight: 600;
}
.archive-undo-btn:hover { background: var(--bg-hover); }
.archive-undo-enter-active, .archive-undo-leave-active { transition: opacity 0.16s ease, transform 0.16s ease; }
.archive-undo-enter-from, .archive-undo-leave-to { opacity: 0; transform: translate(-50%, -6px); }
@media (prefers-reduced-motion: reduce) {
  .archive-undo-enter-active, .archive-undo-leave-active { transition: none; }
}
</style>
