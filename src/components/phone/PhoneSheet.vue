<script setup lang="ts">
import { ref } from "vue";

// Bottom sheet: closes on a backdrop tap or a downward swipe on the sheet.
const props = defineProps<{ title?: string }>();
const emit = defineEmits<{ close: [] }>();

const dragY = ref(0);
let startY: number | null = null;
function onStart(e: TouchEvent) { startY = e.touches[0].clientY; }
function onMove(e: TouchEvent) { if (startY !== null) dragY.value = Math.max(0, e.touches[0].clientY - startY); }
function onEnd() {
  if (dragY.value > 80) emit("close");
  dragY.value = 0;
  startY = null;
}
</script>

<template>
  <Teleport to="body">
    <div class="fixed inset-0 z-[70] flex flex-col justify-end bg-black/45" @click.self="emit('close')">
      <div
        class="phone-sheet max-h-[75dvh] overflow-y-auto rounded-t-2xl border-t border-border bg-panel"
        :style="dragY ? { transform: `translateY(${dragY}px)` } : undefined"
        @touchstart.passive="onStart"
        @touchmove.passive="onMove"
        @touchend="onEnd"
      >
        <div class="mx-auto mb-1 mt-2 h-1 w-9 rounded bg-border" />
        <div v-if="props.title" class="px-4 pb-1 pt-1 text-[12px] font-semibold text-muted-foreground">{{ props.title }}</div>
        <slot />
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.phone-sheet { padding-bottom: calc(8px + env(safe-area-inset-bottom, 0px)); }
</style>
