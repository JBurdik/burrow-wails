<script setup lang="ts">
import { computed, shallowRef } from "vue";

const props = defineProps<{
  selection: string;
  targetAvailable: boolean;
  sending: boolean;
  status: string;
}>();

const emit = defineEmits<{
  submit: [comment: string];
  cancel: [];
}>();

const comment = shallowRef("");
const selectionLabel = computed(() =>
  props.selection ? "Selected diff context will be included." : "Select diff lines to include context.",
);

function submit() {
  const value = comment.value.trim();
  if (!value || !props.targetAvailable || props.sending) return;
  emit("submit", value);
  comment.value = "";
}
</script>

<template>
  <form class="feedback-composer" @submit.prevent="submit">
    <div class="feedback-context">{{ selectionLabel }}</div>
    <textarea
      v-model="comment"
      class="feedback-input"
      rows="3"
      placeholder="Describe the change you want…"
      :disabled="!targetAvailable || sending"
      @keydown.meta.enter.prevent="submit"
      @keydown.ctrl.enter.prevent="submit"
    />
    <div class="feedback-actions">
      <span class="feedback-status" :class="{ error: !targetAvailable }">{{ status }}</span>
      <button type="button" class="feedback-btn secondary" @click="emit('cancel')">Cancel</button>
      <button type="submit" class="feedback-btn" :disabled="!comment.trim() || !targetAvailable || sending">
        {{ sending ? "Sending…" : "Send to agent" }}
      </button>
    </div>
  </form>
</template>

<style scoped>
.feedback-composer { display: grid; gap: 7px; padding: 8px 12px; border-bottom: 1px solid var(--border); background: var(--bg-hover); }
.feedback-context, .feedback-status { color: var(--text-muted); font-size: 10px; }
.feedback-input { width: 100%; resize: vertical; box-sizing: border-box; border: 1px solid var(--border); border-radius: 4px; background: var(--bg-base); color: var(--text-primary); font: 11px/1.4 var(--font-mono); padding: 6px 8px; }
.feedback-input:focus { outline: 1px solid var(--accent); border-color: var(--accent); }
.feedback-actions { display: flex; align-items: center; gap: 6px; }
.feedback-status { flex: 1; }
.feedback-status.error { color: var(--danger, #e56); }
.feedback-btn { border: 1px solid var(--accent); border-radius: 3px; background: var(--accent); color: var(--bg-base); cursor: pointer; font-size: 10px; padding: 3px 8px; }
.feedback-btn:disabled { cursor: default; opacity: .5; }
.feedback-btn.secondary { background: transparent; border-color: var(--border); color: var(--text-secondary); }
</style>
