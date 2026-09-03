<template>
  <div class="flex flex-col overflow-hidden border-t border-border bg-[var(--terminal-bg)]">
    <div class="flex h-8 shrink-0 items-center gap-1 overflow-x-auto border-b border-border px-1.5 hide-scrollbar">
      <button
        v-for="id in state.ptyIds"
        :key="id"
        class="group flex h-6 shrink-0 items-center gap-1.5 rounded px-2 text-[11px] text-muted-foreground transition-colors hover:bg-hover hover:text-secondary-foreground"
        :class="state.activeId === id && 'bg-accent/15 text-foreground'"
        @click="$emit('select', id)"
      >
        <PhTerminal :size="12" class="shrink-0" />
        <span>Terminal</span>
        <span
          class="ml-0.5 flex h-3.5 w-3.5 items-center justify-center rounded text-muted-foreground opacity-0 hover:bg-black/10 hover:text-foreground group-hover:opacity-100"
          role="button"
          aria-label="Close terminal"
          @click.stop="$emit('close', id)"
        ><PhX :size="10" /></span>
      </button>
      <button class="flex h-6 w-6 shrink-0 items-center justify-center rounded text-muted-foreground hover:bg-hover hover:text-foreground" title="New terminal" @click="$emit('add')">
        <PhPlus :size="13" />
      </button>
    </div>
    <XTerm
      v-for="id in state.ptyIds"
      :key="id"
      v-show="state.activeId === id"
      :pty-id="id"
      :cwd="cwd"
      class="min-h-0 flex-1"
    />
  </div>
</template>

<script setup lang="ts">
import { PhTerminal, PhX, PhPlus } from "@phosphor-icons/vue";
import XTerm from "./XTerm.vue";

defineProps<{
  state: { ptyIds: number[]; activeId: number | null };
  cwd: string;
}>();
defineEmits<{ add: []; close: [ptyId: number]; select: [ptyId: number] }>();
</script>
