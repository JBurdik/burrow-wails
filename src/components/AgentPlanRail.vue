<script setup lang="ts">
import { computed, shallowRef, watch } from "vue";
import { PhCaretDown, PhCaretRight, PhCheck, PhListChecks, PhPlus, PhTrash } from "@phosphor-icons/vue";
import { useAgentPlansStore, type PlanStepStatus } from "@/stores/agentPlans";

const props = defineProps<{ ptyId: number; agentTitle?: string }>();

const store = useAgentPlansStore();
const collapsed = shallowRef(false);
const draft = shallowRef("");
const plan = computed(() => store.getPlan(props.ptyId));
const steps = computed(() => plan.value?.steps ?? []);
const completedCount = computed(() => steps.value.filter((step) => step.status === "completed").length);
const progressLabel = computed(() => steps.value.length ? `${completedCount.value}/${steps.value.length}` : "No plan");

function syncDraft() {
  draft.value = steps.value.map((step) => step.text).join("\n");
}

function saveDraft() {
  store.replaceSteps(props.ptyId, draft.value.split("\n"));
  syncDraft();
}

function addStep() {
  draft.value = draft.value.trimEnd() + (draft.value.trim() ? "\n" : "") + "New step";
  saveDraft();
}

function updateStatus(id: number, event: Event) {
  store.setStepStatus(props.ptyId, id, (event.target as HTMLSelectElement).value as PlanStepStatus);
}

function updateText(id: number, event: Event) {
  store.updateStep(props.ptyId, id, (event.target as HTMLInputElement).value);
  syncDraft();
}

watch(() => props.ptyId, syncDraft, { immediate: true });
</script>

<template>
  <section class="shrink-0 border-b border-border bg-panel" aria-label="Agent plan">
    <button class="flex h-6 w-full items-center gap-1.5 border-0 bg-transparent px-2 text-left font-sans text-[10px] text-muted-foreground hover:bg-hover" @click="collapsed = !collapsed">
      <component :is="collapsed ? PhCaretRight : PhCaretDown" :size="10" weight="bold" />
      <PhListChecks :size="13" weight="bold" />
      <span>Plan</span>
      <span v-if="agentTitle" class="max-w-[180px] truncate text-muted-foreground/70">{{ agentTitle }}</span>
      <span class="ml-auto font-mono text-[9px]">{{ progressLabel }}</span>
    </button>

    <div v-if="!collapsed" class="max-h-[220px] overflow-auto px-2 pb-2 pt-1">
      <p v-if="!steps.length" class="mb-2 mt-0.5 text-[10px] leading-snug text-muted-foreground">Add a step-by-step plan for this agent. It stays attached to this terminal.</p>
      <div v-else class="grid gap-1">
        <div v-for="step in steps" :key="step.id" class="flex min-w-0 items-center gap-1.5 py-0.5">
          <PhCheck v-if="step.status === 'completed'" :size="13" weight="bold" class="shrink-0 basis-[13px] text-accent" />
          <span
            v-else
            class="mx-0.5 h-1.5 w-1.5 shrink-0 basis-[13px] rounded-full bg-muted-foreground"
            :class="step.status === 'in_progress' && 'bg-accent shadow-[0_0_0_2px_rgba(236,72,153,0.2)]'"
          />
          <input
            :value="step.text"
            class="min-w-0 flex-1 border-0 border-b border-transparent bg-transparent py-0.5 font-sans text-[11px] text-foreground focus:border-b-accent focus:outline-none"
            :class="step.status === 'completed' && 'text-muted-foreground line-through'"
            @change="updateText(step.id, $event)"
          />
          <select
            :value="step.status"
            class="w-[70px] rounded border border-border bg-base font-sans text-[9px] text-muted-foreground"
            :aria-label="`Status for ${step.text}`"
            @change="updateStatus(step.id, $event)"
          >
            <option value="pending">Pending</option>
            <option value="in_progress">Active</option>
            <option value="completed">Done</option>
          </select>
          <button class="grid place-items-center rounded border-0 bg-transparent p-0.5 text-muted-foreground opacity-50 hover:bg-destructive/12 hover:text-destructive hover:opacity-100" title="Remove step" @click="store.removeStep(ptyId, step.id)">
            <PhTrash :size="12" />
          </button>
        </div>
      </div>

      <details class="mt-1.5 text-[10px] text-muted-foreground">
        <summary class="cursor-pointer">Edit plan as lines</summary>
        <textarea v-model="draft" rows="4" placeholder="One step per line" class="mt-1 box-border w-full resize-y rounded border border-border bg-terminal-bg p-1 font-mono text-[10px] text-foreground" @blur="saveDraft" />
        <button class="mt-0.5 rounded border-0 bg-hover px-1.5 py-0.5 text-[10px] text-muted-foreground hover:text-accent" @click="saveDraft">Save plan</button>
      </details>
      <div class="mt-1.5 flex justify-between">
        <button class="inline-flex items-center gap-0.5 rounded border-0 bg-hover px-1.5 py-0.5 text-[10px] text-muted-foreground hover:text-accent" @click="addStep"><PhPlus :size="12" weight="bold" /> Add step</button>
        <button v-if="steps.length" class="rounded border-0 bg-transparent px-1.5 py-0.5 text-[10px] text-muted-foreground hover:text-destructive hover:bg-destructive/12" @click="store.clear(ptyId); syncDraft()">Clear plan</button>
      </div>
    </div>
  </section>
</template>
