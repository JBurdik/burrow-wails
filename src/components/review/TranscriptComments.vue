<script setup lang="ts">
import { computed, nextTick, shallowRef, useTemplateRef } from "vue";
import { PhChatCircleText, PhX, PhArrowSquareOut } from "@phosphor-icons/vue";
import { selectedTranscriptQuote, type TranscriptQuote, type TranscriptNote } from "@/lib/transcriptFeedback";

const props = defineProps<{ messageId: number; text: string; partial?: boolean; notes: TranscriptNote[] }>();
const emit = defineEmits<{
  add: [quote: TranscriptQuote, body: string];
  remove: [id: string];
  followUp: [delivery: string];
  resolve: [id: string, resolved: boolean];
}>();
const content = useTemplateRef<HTMLElement>("content");
const input = useTemplateRef<HTMLTextAreaElement>("input");
const selection = shallowRef<TranscriptQuote | null>(null);
const editing = shallowRef(false);
const body = shallowRef("");
const hasSelection = computed(() => !!selection.value && !props.partial);

function captureSelection() {
  if (editing.value || !content.value) return;
  selection.value = selectedTranscriptQuote(content.value, window.getSelection());
}

async function startComment() {
  if (!hasSelection.value) return;
  editing.value = true;
  await nextTick();
  input.value?.focus();
}

function submit() {
  if (!selection.value || !body.value.trim() || props.partial) return;
  emit("add", selection.value, body.value);
  cancel();
}

function cancel() {
  editing.value = false;
  body.value = "";
  selection.value = null;
}
</script>

<template>
  <div class="min-w-0">
    <div ref="content" tabindex="0" aria-label="Agent response, select text to comment" @mouseup="captureSelection" @keyup="captureSelection" @keydown.meta.alt.m.prevent="startComment" @keydown.ctrl.alt.m.prevent="startComment"><slot /></div>
    <button v-if="hasSelection && !editing" type="button" class="mt-1 flex items-center gap-1 rounded border border-border bg-hover px-2 py-1 text-[11px] text-secondary-foreground hover:text-foreground focus-visible:outline focus-visible:outline-1 focus-visible:outline-accent" title="Comment on selection (⌘⌥M / Ctrl+Alt+M)" @mousedown.prevent @click="startComment">
      <PhChatCircleText :size="12" /> Comment on selection
    </button>
    <form v-if="editing" class="mt-2 grid gap-1.5 rounded border border-border bg-hover p-2" @submit.prevent="submit" @keydown.esc.stop="cancel">
      <blockquote class="m-0 max-h-20 overflow-auto whitespace-pre-wrap text-[11px] text-muted-foreground">{{ selection?.quote }}</blockquote>
      <textarea ref="input" v-model="body" rows="2" aria-label="Comment on selected passage" placeholder="Add a review comment…" class="w-full resize-y rounded border border-border bg-base px-2 py-1.5 text-xs text-foreground outline-none focus:border-accent" @keydown.meta.enter.prevent="submit" @keydown.ctrl.enter.prevent="submit" />
      <div class="flex items-center justify-end gap-2">
        <button type="button" class="rounded px-2 py-1 text-[11px] text-muted-foreground hover:text-foreground" @click="cancel">Cancel</button>
        <button type="submit" :disabled="!body.trim()" class="rounded bg-accent px-2 py-1 text-[11px] text-accent-foreground disabled:opacity-40">Add comment</button>
      </div>
    </form>
    <div v-for="note in notes" :key="note.id" class="mt-2 flex items-start gap-2 rounded border border-border px-2 py-1.5 text-[11px] leading-relaxed">
      <PhChatCircleText :size="12" class="mt-0.5 shrink-0 text-muted-foreground" />
      <div class="min-w-0 flex-1">
        <details>
          <summary class="cursor-pointer truncate text-muted-foreground" :title="note.quote">{{ note.quote }}</summary>
          <blockquote class="m-0 mt-1 max-h-28 overflow-auto whitespace-pre-wrap text-muted-foreground">{{ note.quote }}</blockquote>
          <p v-if="note.source !== text" class="my-1 text-warning">This response has changed since you commented. The original passage is preserved.</p>
        </details>
        <p class="m-0 mt-0.5 whitespace-pre-wrap text-foreground">{{ note.body }}</p>
        <span v-if="!note.delivery && !note.resolvedAt" class="text-[10px] text-muted-foreground">Pending, included with your next message when the agent is ready</span>
        <button v-if="note.delivery" type="button" class="mt-1 flex items-center gap-1 text-[10px] text-muted-foreground hover:text-foreground" @click="emit('followUp', note.delivery)"><PhArrowSquareOut :size="11" /> View follow-up</button>
        <div class="mt-1 flex items-center gap-2">
          <span v-if="note.resolvedAt" class="text-[10px] text-muted-foreground">Resolved</span>
          <button type="button" class="text-[10px] text-secondary-foreground hover:text-foreground focus-visible:outline focus-visible:outline-accent" @click="emit('resolve', note.id, !note.resolvedAt)">{{ note.resolvedAt ? 'Reopen' : 'Resolve' }}</button>
        </div>
      </div>
      <button v-if="!note.delivery" type="button" class="rounded p-0.5 text-muted-foreground hover:bg-hover hover:text-foreground" aria-label="Remove pending comment" @click="emit('remove', note.id)"><PhX :size="12" /></button>
    </div>
  </div>
</template>
