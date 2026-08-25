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
  <section class="plan-rail" :class="{ collapsed }" aria-label="Agent plan">
    <button class="plan-header" @click="collapsed = !collapsed">
      <component :is="collapsed ? PhCaretRight : PhCaretDown" :size="10" weight="bold" />
      <PhListChecks :size="13" weight="bold" />
      <span>Plan</span>
      <span v-if="agentTitle" class="plan-agent">{{ agentTitle }}</span>
      <span class="plan-progress">{{ progressLabel }}</span>
    </button>

    <div v-if="!collapsed" class="plan-content">
      <p v-if="!steps.length" class="plan-empty">Add a step-by-step plan for this agent. It stays attached to this terminal.</p>
      <div v-else class="plan-steps">
        <div v-for="step in steps" :key="step.id" class="plan-step" :class="`is-${step.status}`">
          <PhCheck v-if="step.status === 'completed'" :size="13" weight="bold" class="step-check" />
          <span v-else class="step-dot" />
          <input :value="step.text" class="step-text" @change="updateText(step.id, $event)" />
          <select :value="step.status" class="step-status" :aria-label="`Status for ${step.text}`" @change="updateStatus(step.id, $event)">
            <option value="pending">Pending</option>
            <option value="in_progress">Active</option>
            <option value="completed">Done</option>
          </select>
          <button class="step-remove" title="Remove step" @click="store.removeStep(ptyId, step.id)"><PhTrash :size="12" /></button>
        </div>
      </div>

      <details class="plan-editor">
        <summary>Edit plan as lines</summary>
        <textarea v-model="draft" rows="4" placeholder="One step per line" @blur="saveDraft" />
        <button class="plan-save" @click="saveDraft">Save plan</button>
      </details>
      <div class="plan-actions">
        <button class="plan-add" @click="addStep"><PhPlus :size="12" weight="bold" /> Add step</button>
        <button v-if="steps.length" class="plan-clear" @click="store.clear(ptyId); syncDraft()">Clear plan</button>
      </div>
    </div>
  </section>
</template>

<style scoped>
.plan-rail { flex-shrink: 0; border-bottom: 1px solid var(--border); background: var(--bg-panel); }
.plan-header { width: 100%; height: 24px; padding: 0 8px; display: flex; align-items: center; gap: 5px; border: 0; background: transparent; color: var(--text-muted); font: 10px var(--font-ui); cursor: pointer; text-align: left; }
.plan-header:hover { background: var(--bg-hover); }
.plan-agent { overflow: hidden; max-width: 180px; color: var(--text-dim); text-overflow: ellipsis; white-space: nowrap; }
.plan-progress { margin-left: auto; font: 9px var(--font-mono); }
.plan-content { max-height: 220px; overflow: auto; padding: 5px 8px 7px; }
.plan-empty { margin: 2px 0 7px; color: var(--text-muted); font-size: 10px; line-height: 1.4; }
.plan-steps { display: grid; gap: 3px; }
.plan-step { display: flex; align-items: center; gap: 5px; min-width: 0; padding: 2px 0; }
.step-dot, .step-check { flex: 0 0 13px; color: var(--accent); }
.step-dot { width: 7px; height: 7px; margin: 0 3px; border-radius: 50%; background: var(--text-muted); }
.is-in_progress .step-dot { background: var(--accent); box-shadow: 0 0 0 2px color-mix(in srgb, var(--accent) 20%, transparent); }
.step-text { min-width: 0; flex: 1; border: 0; border-bottom: 1px solid transparent; padding: 2px 0; background: transparent; color: var(--text); font: 11px var(--font-ui); }
.step-text:focus { outline: 0; border-bottom-color: var(--accent); }
.is-completed .step-text { color: var(--text-muted); text-decoration: line-through; }
.step-status { width: 70px; border: 1px solid var(--border); border-radius: 3px; background: var(--bg-base); color: var(--text-muted); font: 9px var(--font-ui); }
.step-remove, .plan-clear, .plan-add, .plan-save { border: 0; border-radius: 3px; background: transparent; color: var(--text-muted); font: 10px var(--font-ui); cursor: pointer; }
.step-remove { display: grid; place-items: center; padding: 2px; opacity: .5; }.step-remove:hover, .plan-clear:hover { color: var(--red); background: color-mix(in srgb, var(--red) 12%, transparent); opacity: 1; }
.plan-editor { margin-top: 6px; color: var(--text-muted); font-size: 10px; }.plan-editor summary { cursor: pointer; }.plan-editor textarea { box-sizing: border-box; width: 100%; margin-top: 4px; resize: vertical; border: 1px solid var(--border); border-radius: 3px; padding: 4px; background: var(--terminal-bg); color: var(--text); font: 10px var(--font-mono); }.plan-save { margin-top: 3px; padding: 3px 5px; background: var(--bg-hover); }
.plan-actions { display: flex; justify-content: space-between; margin-top: 5px; }.plan-add { display: inline-flex; align-items: center; gap: 3px; padding: 3px 5px; background: var(--bg-hover); }.plan-add:hover, .plan-save:hover { color: var(--accent); }
</style>
