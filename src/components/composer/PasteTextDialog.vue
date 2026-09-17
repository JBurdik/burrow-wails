<template>
  <Teleport to="body">
    <div v-if="modelValue !== null" class="fixed inset-0 z-[1000] flex items-center justify-center bg-black/70" @click.self="close">
      <div
        class="flex max-h-[90vh] w-[90vw] max-w-[900px] flex-col overflow-hidden rounded-lg shadow-[0_24px_64px_rgba(0,0,0,0.5)]"
        style="background: var(--bg-panel, #18181c); border: 1px solid var(--border, rgba(255,255,255,0.08));"
      >
        <div class="flex shrink-0 items-center justify-between border-b px-3 py-2" style="border-color: var(--border, rgba(255,255,255,0.08));">
          <span class="text-sm" style="color: var(--text-secondary, rgba(255,255,255,0.6));">Pasted text</span>
          <button class="flex items-center rounded p-1 hover:bg-white/10" style="color: var(--text-secondary, rgba(255,255,255,0.6));" @click="close">
            <PhX :size="16" />
          </button>
        </div>
        <pre
          class="m-0 flex-1 overflow-auto whitespace-pre-wrap p-3 font-mono text-sm"
          style="color: var(--text-primary, rgba(255,255,255,0.88));"
        >{{ modelValue }}</pre>
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
// A fullscreen "here's the whole pasted block" viewer, shared by the composer's
// own paste chip (ComposerTextInput.vue, still typing) and the transcript's
// read-only one (PasteChip.vue, already sent) — one dialog rather than two
// so a change to either only has one place to land.
import { onBeforeUnmount, onMounted } from "vue";
import { PhX } from "@phosphor-icons/vue";

const modelValue = defineModel<string | null>({ default: null });

function close() {
  modelValue.value = null;
}

function onEscape(e: KeyboardEvent) {
  if (e.key === "Escape" && modelValue.value !== null) close();
}

onMounted(() => window.addEventListener("keydown", onEscape));
onBeforeUnmount(() => window.removeEventListener("keydown", onEscape));
</script>
