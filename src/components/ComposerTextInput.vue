<template>
  <textarea
    ref="textareaEl"
    v-bind="$attrs"
    v-model="model"
    @input="emit('input', $event)"
    @keydown="emit('keydown', $event)"
    @paste="emit('paste', $event)"
  />
</template>

<script setup lang="ts">
import { nextTick, onMounted, useTemplateRef } from "vue";

defineOptions({ inheritAttrs: false });

const model = defineModel<string>({ default: "" });
const props = withDefaults(defineProps<{ autofocus?: boolean }>(), { autofocus: false });
const emit = defineEmits<{
  input: [event: Event];
  keydown: [event: KeyboardEvent];
  paste: [event: ClipboardEvent];
}>();
const textareaEl = useTemplateRef<HTMLTextAreaElement>("textareaEl");

function focus() {
  textareaEl.value?.focus();
}

onMounted(() => {
  if (props.autofocus) nextTick(focus);
});

defineExpose({ focus, element: textareaEl });
</script>
