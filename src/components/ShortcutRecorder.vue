<!-- A button that swallows one keystroke and reports it as a shortcut string. -->
<template>
  <div class="flex items-center gap-1.5">
    <button
      class="inline-flex min-w-[64px] items-center justify-center rounded-lg border border-border bg-base px-2.5 py-1.5 text-[11px]"
      :class="[
        disabled ? 'opacity-40' : 'hover:text-secondary-foreground',
        recording ? 'border-accent/40 bg-accent/8 text-accent' : 'text-muted-foreground',
      ]"
      :disabled="disabled"
      :title="recording ? 'Press keys… (Esc to cancel)' : 'Click to set a shortcut'"
      @click="start"
      @keydown="onKey"
      @blur="recording = false"
    >
      {{ recording ? "Press…" : (modelValue || "—") }}
    </button>
    <button
      v-if="modelValue && !recording"
      class="rounded p-0.5 text-muted-foreground/60 hover:bg-destructive/12 hover:text-destructive"
      title="Clear shortcut"
      @click="emit('update:modelValue', '')"
    >
      <PhX :size="11" />
    </button>
  </div>
</template>

<script setup lang="ts">
import { ref } from "vue";
import { PhX } from "@phosphor-icons/vue";
import { eventToShortcut } from "@/lib/shortcuts";

defineProps<{ modelValue: string; disabled?: boolean }>();
const emit = defineEmits<{ "update:modelValue": [value: string] }>();

const recording = ref(false);

function start(e: MouseEvent) {
  recording.value = !recording.value;
  if (recording.value) (e.currentTarget as HTMLElement).focus();
}

function onKey(e: KeyboardEvent) {
  if (!recording.value) return;
  e.preventDefault();
  e.stopPropagation();
  if (e.key === "Escape") { recording.value = false; return; }
  const sc = eventToShortcut(e);
  if (!sc) return;
  emit("update:modelValue", sc);
  recording.value = false;
}
</script>
