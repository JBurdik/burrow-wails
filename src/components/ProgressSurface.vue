<script setup lang="ts">
import { computed } from "vue";
import { PhCheckCircle, PhCircle, PhCircleNotch, PhListChecks } from "@phosphor-icons/vue";
import { chatSession } from "@/lib/chatSession";
import { latestTodos } from "@/lib/chatTodos";

// The active chat's live plan: its latest TodoWrite call. Read-only — the
// session registry owns the transcript; this only projects it.
const props = defineProps<{ chatId: number | null }>();

const todos = computed(() => (props.chatId ? latestTodos(chatSession(props.chatId).messages.value) : []));
const doneCount = computed(() => todos.value.filter((t) => t.status === "completed").length);
</script>

<template>
  <section class="flex min-h-0 flex-1 flex-col overflow-hidden text-[11px] text-secondary-foreground">
    <header class="flex items-center justify-between border-b border-border p-2">
      <span class="flex items-center gap-1.5 font-semibold text-foreground"><PhListChecks :size="14" /> Progress</span>
      <span v-if="todos.length" class="text-muted-foreground">{{ doneCount }}/{{ todos.length }} done</span>
    </header>
    <div v-if="todos.length === 0" class="p-4 text-center leading-relaxed text-muted-foreground">Progress on longer tasks appears here.</div>
    <div v-else class="flex-1 overflow-auto">
      <div
        v-for="(t, i) in todos"
        :key="i"
        class="flex items-start gap-2 border-b border-border/65 px-2 py-1.5"
        :class="t.status === 'in_progress' && 'bg-hover'"
      >
        <PhCheckCircle v-if="t.status === 'completed'" :size="13" weight="fill" class="mt-px shrink-0 text-success" />
        <PhCircleNotch v-else-if="t.status === 'in_progress'" :size="13" class="mt-px shrink-0 animate-spin text-accent" />
        <PhCircle v-else :size="13" class="mt-px shrink-0 text-muted-foreground" />
        <span
          class="min-w-0 flex-1 leading-snug"
          :class="{
            'text-muted-foreground line-through': t.status === 'completed',
            'font-medium text-foreground': t.status === 'in_progress',
          }"
        >{{ t.status === "in_progress" && t.activeForm ? t.activeForm : t.content }}</span>
      </div>
    </div>
  </section>
</template>
