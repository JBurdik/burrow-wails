<template>
  <div class="flex flex-col overflow-hidden border-t border-border bg-[var(--terminal-bg)]">
    <div class="flex h-9 shrink-0 items-center gap-1 overflow-x-auto px-2 hide-scrollbar">
      <button
        v-for="id in state.ptyIds"
        :key="id"
        class="group flex h-7 shrink-0 items-center gap-1.5 rounded-lg px-2.5 text-[12px] transition-colors duration-75"
        :class="state.activeId === id ? 'bg-selected text-foreground' : 'text-muted-foreground hover:bg-hover hover:text-foreground'"
        @click="$emit('select', id)"
      >
        <PhTerminal :size="12" class="shrink-0" />
        <span>Terminal</span>
        <span
          class="ml-0.5 flex h-4 w-4 items-center justify-center rounded text-muted-foreground opacity-0 hover:bg-hover hover:text-foreground group-hover:opacity-100"
          role="button"
          aria-label="Close terminal"
          @click.stop="$emit('close', id)"
        ><PhX :size="10" /></span>
      </button>
      <button class="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg text-muted-foreground hover:bg-hover hover:text-foreground" title="New terminal" @click="$emit('add')">
        <PhPlus :size="13" />
      </button>
    </div>
    <!-- Wrapper is load-bearing: XTerm has a fragment root, so v-show on it is ignored. -->
    <div
      v-for="id in state.ptyIds"
      :key="id"
      v-show="state.activeId === id"
      class="relative flex min-h-0 flex-1 flex-col"
    >
      <XTerm :pty-id="id" :cwd="cwd" />
    </div>
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
