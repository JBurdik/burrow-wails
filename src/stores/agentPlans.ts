import { defineStore } from "pinia";
import { computed, reactive } from "vue";

export type PlanStepStatus = "pending" | "in_progress" | "completed";

export interface AgentPlanStep {
  id: number;
  text: string;
  status: PlanStepStatus;
  updatedAt: number;
}

export interface AgentPlan {
  ptyId: number;
  steps: AgentPlanStep[];
  updatedAt: number;
}

const STORAGE_KEY = "burrow.agent-plans.v1";

function loadPlans(): Record<number, AgentPlan> {
  try {
    const saved = localStorage.getItem(STORAGE_KEY);
    return saved ? JSON.parse(saved) as Record<number, AgentPlan> : {};
  } catch {
    return {};
  }
}

export const useAgentPlansStore = defineStore("agentPlans", () => {
  // Plans belong to a concrete PTY, so changing tabs never leaks a plan to
  // another agent. Persisting locally keeps them available for this app session
  // and across a normal desktop-app relaunch.
  const plans = reactive<Record<number, AgentPlan>>(loadPlans());

  function persist() {
    try { localStorage.setItem(STORAGE_KEY, JSON.stringify(plans)); }
    catch { /* Plan history is best-effort when browser storage is unavailable. */ }
  }

  function getPlan(ptyId: number): AgentPlan | undefined {
    return plans[ptyId];
  }

  function ensurePlan(ptyId: number): AgentPlan {
    if (!plans[ptyId]) plans[ptyId] = { ptyId, steps: [], updatedAt: Date.now() };
    return plans[ptyId];
  }

  function replaceSteps(ptyId: number, texts: string[]) {
    const now = Date.now();
    const plan = ensurePlan(ptyId);
    const currentByText = new Map(plan.steps.map((step) => [step.text.trim(), step]));
    plan.steps = texts
      .map((text) => text.trim())
      .filter(Boolean)
      .map((text, index) => {
        const existing = currentByText.get(text);
        return existing ?? { id: now + index, text, status: "pending", updatedAt: now };
      });
    plan.updatedAt = now;
    persist();
  }

  function updateStep(ptyId: number, id: number, text: string) {
    const plan = getPlan(ptyId);
    const step = plan?.steps.find((candidate) => candidate.id === id);
    if (!plan || !step) return;
    step.text = text.trim();
    step.updatedAt = Date.now();
    plan.updatedAt = step.updatedAt;
    persist();
  }

  function setStepStatus(ptyId: number, id: number, status: PlanStepStatus) {
    const plan = getPlan(ptyId);
    const step = plan?.steps.find((candidate) => candidate.id === id);
    if (!plan || !step) return;
    const now = Date.now();
    step.status = status;
    step.updatedAt = now;
    plan.updatedAt = now;
    persist();
  }

  function removeStep(ptyId: number, id: number) {
    const plan = getPlan(ptyId);
    if (!plan) return;
    plan.steps = plan.steps.filter((step) => step.id !== id);
    plan.updatedAt = Date.now();
    persist();
  }

  function clear(ptyId: number) {
    delete plans[ptyId];
    persist();
  }

  const planCount = computed(() => Object.keys(plans).length);
  return { plans, planCount, getPlan, replaceSteps, updateStep, setStepStatus, removeStep, clear };
});
