<script setup lang="ts">
import { computed, reactive } from "vue";

export interface CodexUserInputQuestion {
  id: string;
  header: string;
  question: string;
  isOther?: boolean;
  isSecret?: boolean;
  options: Array<{ label: string; description?: string }>;
}

const props = defineProps<{
  questions: CodexUserInputQuestion[];
  submitting?: boolean;
}>();

const emit = defineEmits<{
  submit: [answers: Record<string, string[]>];
  cancel: [];
}>();

const choices = reactive<Record<string, string>>({});
const customAnswers = reactive<Record<string, string>>({});
const canSubmit = computed(() => props.questions.every((question) => {
  return Boolean((customAnswers[question.id] ?? "").trim() || choices[question.id]);
}));

function choose(questionId: string, label: string) {
  choices[questionId] = label;
  customAnswers[questionId] = "";
}

function submit() {
  if (!canSubmit.value || props.submitting) return;
  const answers: Record<string, string[]> = {};
  for (const question of props.questions) {
    const custom = (customAnswers[question.id] ?? "").trim();
    const value = custom || choices[question.id];
    if (value) answers[question.id] = [value];
  }
  emit("submit", answers);
}
</script>

<template>
  <div class="question-panel perm-slide-in border-b border-border bg-[color-mix(in_srgb,var(--chat-info)_4%,transparent)] px-3.5 py-3">
    <div v-for="question in questions" :key="question.id" class="mb-4 last:mb-0">
      <span v-if="question.header" class="rounded bg-[color-mix(in_srgb,var(--chat-info)_22%,transparent)] px-1.5 py-0.5 text-[9px] font-bold uppercase tracking-[0.04em] text-[var(--chat-info)]">{{ question.header }}</span>
      <p class="mb-1 mt-2 text-[13px] font-semibold text-foreground">{{ question.question }}</p>
      <div class="mt-1.5 flex flex-col gap-1.5">
        <button
          v-for="option in question.options"
          :key="option.label"
          type="button"
          class="question-opt flex w-full cursor-pointer items-center gap-2 rounded-md border px-2.5 py-[7px] text-left transition-colors"
          :class="choices[question.id] === option.label
            ? 'border-[var(--chat-info)] bg-[color-mix(in_srgb,var(--chat-info)_16%,var(--bg-base))]'
            : 'border-border bg-base hover:border-[color-mix(in_srgb,var(--chat-info)_55%,transparent)]'"
          :disabled="submitting"
          @click="choose(question.id, option.label)"
        >
          <span class="flex min-w-0 flex-1 flex-col gap-px">
            <span class="text-xs font-semibold text-foreground">{{ option.label }}</span>
            <span v-if="option.description" class="text-[10px] text-secondary-foreground">{{ option.description }}</span>
          </span>
        </button>
        <input
          v-if="question.isOther || question.options.length === 0"
          v-model="customAnswers[question.id]"
          :type="question.isSecret ? 'password' : 'text'"
          class="question-opt-other w-full rounded-md border border-border bg-base px-2.5 py-[7px] text-xs text-foreground outline-none placeholder:text-muted-foreground focus:border-[color-mix(in_srgb,var(--chat-info)_55%,transparent)]"
          placeholder="Write your answer…"
          :disabled="submitting"
          @input="choices[question.id] = ''"
        />
      </div>
    </div>
    <div class="mt-2.5 flex justify-end gap-2">
      <button class="perm-btn perm-deny !px-2 !py-1 text-[11px]" :disabled="submitting" @click="emit('cancel')">Cancel</button>
      <button class="perm-btn perm-allow !px-3 !py-1 text-[11px]" :disabled="!canSubmit || submitting" @click="submit">Submit</button>
    </div>
  </div>
</template>
