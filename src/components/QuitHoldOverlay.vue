<script setup lang="ts">
// Hold-to-quit hint (src-wails/quithold.go): Go decides when to quit; this
// only shows the hold. "down" starts the fill, "quitting" = held long enough
// (quits on release), "up" = released early.
import { onMounted, onUnmounted, ref } from "vue";
import { listen, type UnlistenFn } from "@tauri-apps/api/event";

const state = ref<"down" | "quitting" | null>(null);
let unlisten: UnlistenFn | null = null;

onMounted(async () => {
  unlisten = await listen<string>("menu-quit-hold", (e) => {
    state.value = e.payload === "down" || e.payload === "quitting" ? e.payload : null;
  });
});
onUnmounted(() => unlisten?.());
</script>

<template>
  <div v-if="state" role="status" class="pointer-events-none fixed inset-x-0 top-[22%] z-[1000] flex justify-center">
    <div class="relative overflow-hidden rounded-lg border border-border bg-popover px-8 py-4 shadow-xl">
      <div class="qh-fill absolute inset-y-0 left-0 bg-primary/20" :class="{ full: state === 'quitting' }" />
      <span class="relative text-lg font-semibold text-popover-foreground">
        {{ state === "quitting" ? "Release to quit" : "Hold ⌘Q to quit" }}
      </span>
    </div>
  </div>
</template>

<style scoped>
.qh-fill { width: 0; animation: qh-fill 1s linear forwards; }
.qh-fill.full { width: 100%; animation: none; }
@keyframes qh-fill { to { width: 100%; } }
</style>
