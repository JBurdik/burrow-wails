import { ref } from "vue";
import { defineStore } from "pinia";

// Tracks Task-tool (sub-agent) invocations across chat sessions so the right
// panel can show a live "who's running what" list instead of the user having
// to scroll through a chat transcript to find them.
export interface SubagentEntry {
  toolUseId: string;
  chatId: number;
  subagentType?: string;
  description: string;
  status: "running" | "done" | "failed";
  startedAt: number;
  finishedAt?: number;
}

const MAX_ENTRIES = 200;

export const useSubagentsStore = defineStore("subagents", () => {
  const entries = ref<SubagentEntry[]>([]);

  function started(chatId: number, toolUseId: string, input: Record<string, unknown> | undefined) {
    const inp = input ?? {};
    entries.value.push({
      toolUseId,
      chatId,
      subagentType: typeof inp.subagent_type === "string" ? inp.subagent_type : undefined,
      description: typeof inp.description === "string" ? inp.description : "Sub-agent task",
      status: "running",
      startedAt: Date.now(),
    });
    if (entries.value.length > MAX_ENTRIES) entries.value.splice(0, entries.value.length - MAX_ENTRIES);
  }

  function completed(toolUseId: string, failed: boolean) {
    const entry = entries.value.find((e) => e.toolUseId === toolUseId);
    if (!entry) return;
    entry.status = failed ? "failed" : "done";
    entry.finishedAt = Date.now();
  }

  function forChats(chatIds: number[]): SubagentEntry[] {
    const ids = new Set(chatIds);
    return entries.value.filter((e) => ids.has(e.chatId)).slice().reverse();
  }

  return { entries, started, completed, forChats };
});
