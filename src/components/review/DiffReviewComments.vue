<script setup lang="ts">
import { computed } from "vue";
interface Comment {
  id: number; file: string; line: number; body: string; sent_at: number; resolved_at?: number;
}
const props = defineProps<{ notes: Comment[]; busy?: boolean }>();
defineEmits<{ resolve: [id: number, resolved: boolean] }>();
const openCount = computed(() => props.notes.filter((note) => !note.resolved_at).length);
</script>

<template>
  <details v-if="notes.length" class="shrink-0 border-b border-border text-[11px]">
    <summary class="cursor-pointer px-3 py-1.5 text-secondary-foreground">Review comments · {{ openCount }} open · {{ notes.length - openCount }} resolved</summary>
    <div class="max-h-60 overflow-auto px-3 pb-2">
      <div v-for="note in notes" :key="note.id" class="border-t border-border py-2">
        <div class="flex flex-wrap items-center gap-2">
          <span class="min-w-0 flex-1 break-all font-mono text-muted-foreground">{{ note.file }}:{{ note.line }}</span>
          <span class="text-muted-foreground">{{ note.resolved_at ? 'Resolved' : note.sent_at ? 'Sent' : 'Pending' }}</span>
          <button type="button" :disabled="busy" class="rounded px-1 text-secondary-foreground hover:bg-hover hover:text-foreground disabled:opacity-40 focus-visible:outline focus-visible:outline-accent" :aria-label="`${note.resolved_at ? 'Reopen' : 'Resolve'} comment on ${note.file}:${note.line}`" @click="$emit('resolve', note.id, !note.resolved_at)">{{ note.resolved_at ? 'Reopen' : 'Resolve' }}</button>
        </div>
        <p class="m-0 mt-1 whitespace-pre-wrap break-words text-secondary-foreground">{{ note.body }}</p>
      </div>
    </div>
  </details>
</template>
