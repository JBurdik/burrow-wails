<template>
  <!-- An .html file in the Right Panel, drawn (Source flips to the editor). Same
       sandbox as a chat's HTML preview: scripts run in an opaque origin under
       pageSrcdoc's no-network CSP — it can't reach the internet or your files. -->
  <section class="relative flex min-h-0 flex-1 flex-col overflow-hidden">
    <div class="absolute right-3 top-1.5 z-10 flex items-center gap-1">
      <button class="flex h-6 w-6 items-center justify-center rounded-[var(--radius-nav)] bg-panel text-muted-foreground hover:bg-hover hover:text-foreground" title="Reload" aria-label="Reload" @click="load"><PhArrowClockwise :size="13" /></button>
      <button class="flex h-6 w-6 items-center justify-center rounded-[var(--radius-nav)] bg-panel text-muted-foreground hover:bg-hover hover:text-foreground" title="Source" aria-label="Source" @click="emit('source')"><PhCode :size="13" /></button>
    </div>
    <p v-if="error" class="p-4 text-[12px] text-[var(--red)]">{{ error }}</p>
    <iframe v-else :key="nonce" class="min-h-0 flex-1 bg-white" sandbox="allow-scripts" referrerpolicy="no-referrer" :srcdoc="pageSrcdoc(html)" :title="path" />
  </section>
</template>

<script setup lang="ts">
import { ref, watch } from "vue";
import { invoke } from "@tauri-apps/api/core";
import { PhArrowClockwise, PhCode } from "@phosphor-icons/vue";
import { pageSrcdoc } from "@/lib/chatBlocks";

const props = defineProps<{ path: string; cwd: string }>();
const emit = defineEmits<{ source: [] }>();
const html = ref("");
const error = ref("");
const nonce = ref(0);

function abs(p: string): string {
  return p.startsWith("/") || p.startsWith("~") ? p : `${props.cwd.replace(/\/+$/, "")}/${p}`;
}
async function load() {
  error.value = "";
  try {
    html.value = await invoke<string>("read_text_file", { path: abs(props.path) });
    nonce.value++;
  } catch (e) {
    error.value = `Could not read ${props.path}: ${e}`;
  }
}
watch(() => props.path, load, { immediate: true });
</script>
