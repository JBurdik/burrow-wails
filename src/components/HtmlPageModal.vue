<template>
  <Teleport to="body">
    <div class="fixed inset-0 z-[1000] flex items-center justify-center bg-black/60" @click.self="emit('close')">
      <div class="flex h-[85vh] w-[90vw] max-w-[1200px] flex-col overflow-hidden rounded-[14px] border border-border bg-panel shadow-[0_24px_64px_rgba(0,0,0,0.5)]">
        <div class="flex items-center gap-2 border-b border-border px-3 py-2">
          <span class="min-w-0 flex-1 truncate text-[13px] font-semibold text-foreground">{{ title }}</span>
          <span class="truncate font-mono text-[11px] text-muted-foreground" :title="path">{{ path }}</span>
          <button class="flex items-center rounded p-1 text-muted-foreground hover:bg-hover hover:text-foreground" aria-label="Close" @click="emit('close')"><PhX :size="16" /></button>
        </div>
        <!-- Agent-written HTML: scripts run, but in an opaque origin (no
             allow-same-origin) under pageSrcdoc's no-network CSP. -->
        <iframe class="min-h-0 flex-1 bg-white" sandbox="allow-scripts" referrerpolicy="no-referrer" :srcdoc="pageSrcdoc(html)" :title="title" />
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted } from "vue";
import { PhX } from "@phosphor-icons/vue";
import { pageSrcdoc } from "@/lib/chatBlocks";

defineProps<{ title: string; path: string; html: string }>();
const emit = defineEmits<{ close: [] }>();

function onKey(e: KeyboardEvent) {
  if (e.key === "Escape") emit("close");
}
onMounted(() => window.addEventListener("keydown", onKey));
onBeforeUnmount(() => window.removeEventListener("keydown", onKey));
</script>
