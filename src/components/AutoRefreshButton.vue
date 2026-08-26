<template>
  <div class="relative flex" ref="wrapRef">
    <button
      class="flex items-center gap-1 rounded px-1 py-0.5 font-sans text-[10px] text-muted-foreground transition-colors hover:bg-hover hover:text-foreground"
      :class="{ 'text-accent hover:text-accent': isRunning }"
      :title="isRunning ? `Auto-refresh every ${currentInterval}s (right-click to change)` : 'Auto-refresh off (right-click to change)'"
      @click="toggle()"
      @contextmenu.prevent="menuOpen = !menuOpen"
    >
      <PhArrowClockwise :size="13" :class="{ 'animate-spin': isRunning && nextRefreshIn <= 1 }" />
      <span class="min-w-[20px] text-left">{{ isRunning ? `${nextRefreshIn}s` : "Off" }}</span>
    </button>

    <Transition name="ar-menu">
      <div v-if="menuOpen" class="absolute right-0 top-[calc(100%+4px)] z-[200] flex min-w-[56px] flex-col gap-px rounded-md border border-border bg-panel p-1 shadow-[0_4px_12px_rgba(0,0,0,0.25)]">
        <button
          v-for="n in AUTO_REFRESH_INTERVALS"
          :key="n"
          class="rounded px-2 py-0.5 text-left font-sans text-[11px] text-secondary-foreground transition-colors hover:bg-hover hover:text-foreground"
          :class="{ 'font-semibold text-accent hover:text-accent': currentInterval === n }"
          @click="pick(n)"
        >
          {{ n === 0 ? "Off" : `${n}s` }}
        </button>
      </div>
    </Transition>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount } from "vue";
import { PhArrowClockwise } from "@phosphor-icons/vue";
import { AUTO_REFRESH_INTERVALS } from "@/composables/useAutoRefresh";

const props = defineProps<{
  currentInterval: number;
  isRunning: boolean;
  nextRefreshIn: number;
  toggle: () => void;
  setRefreshInterval: (n: number) => void;
}>();

const menuOpen = ref(false);
const wrapRef = ref<HTMLElement | null>(null);

function pick(n: number) {
  props.setRefreshInterval(n);
  menuOpen.value = false;
}

function onOutsideClick(e: MouseEvent) {
  if (wrapRef.value && !wrapRef.value.contains(e.target as Node)) {
    menuOpen.value = false;
  }
}

onMounted(() => document.addEventListener("mousedown", onOutsideClick));
onBeforeUnmount(() => document.removeEventListener("mousedown", onOutsideClick));
</script>

<style scoped>
.ar-menu-enter-active, .ar-menu-leave-active { transition: opacity 0.1s, transform 0.1s; }
.ar-menu-enter-from, .ar-menu-leave-to { opacity: 0; transform: translateY(-4px); }
</style>
