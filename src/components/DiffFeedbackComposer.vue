<script setup lang="ts">
import { computed, shallowRef } from "vue";
import { Button } from "@/components/ui/button";

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
  <form class="grid gap-1.5 border-b border-border bg-hover px-3 py-2" @submit.prevent="submit">
    <div class="text-[10px] text-muted-foreground">{{ selectionLabel }}</div>
    <textarea
      v-model="comment"
      class="w-full resize-y rounded border border-border bg-base p-1.5 font-mono text-[11px] leading-snug text-foreground focus:border-accent focus:outline focus:outline-1 focus:outline-accent"
      rows="3"
      placeholder="Describe the change you want…"
      :disabled="!targetAvailable || sending"
      @keydown.meta.enter.prevent="submit"
      @keydown.ctrl.enter.prevent="submit"
    />
    <div class="flex items-center gap-1.5">
      <span class="flex-1 text-[10px] text-muted-foreground" :class="{ 'text-destructive': !targetAvailable }">{{ status }}</span>
      <Button type="button" variant="secondary" size="sm" @click="emit('cancel')">Cancel</Button>
      <Button type="submit" size="sm" :disabled="!comment.trim() || !targetAvailable || sending">
        {{ sending ? "Sending…" : "Send to agent" }}
      </Button>
    </div>
  </form>
</template>
