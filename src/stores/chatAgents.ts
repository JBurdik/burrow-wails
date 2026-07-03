import { defineStore } from "pinia";
import { ref, watch } from "vue";
import { configReady, getConfig, setConfig, migrateFromLocalStorage } from "../lib/config";

// A chat agent backs a ClaudeChat session. "stream-json" agents use the native
// claude_* commands; "acp" agents are spawned through acp_start with the
// command/args/env below (any ACP-compatible CLI works).
export interface ChatAgent {
  id: string;
  name: string;
  transport: "stream-json" | "acp";
  command: string; // adapter program: "npx", "gemini", "codex", "opencode", …
  args: string[]; // adapter args, e.g. ["@agentclientprotocol/claude-agent-acp"]
  env: Record<string, string>; // extra env vars passed to the adapter process
  // Drives Rust-side special env injection (CLAUDE_CODE_EXECUTABLE, blank
  // ANTHROPIC_API_KEY for subscription auth, CODEX_API_KEY forwarding). "custom"
  // = no injection.
  kind: "claude" | "gemini" | "codex" | "custom";
  color: string;
  icon: string; // key into AGENT_ICONS (see src/lib/agentIcons.ts)
  shortcut: string; // launch shortcut, e.g. "⌘⇧6" — always opens a new chat tab
  builtin?: boolean;
}

const CONFIG_KEY = "chatAgentPresets";
const LEGACY_STORAGE_KEY = "agentic-ide.chatAgents";

// Built-in agents. claude-acp/codex use the @agentclientprotocol npx adapters
// (same org, subscription-safe); gemini/opencode have native ACP modes.
export const BUILTIN_AGENTS: ChatAgent[] = [
  { id: "claude", name: "Claude Code", transport: "stream-json", command: "claude", args: [], env: {}, kind: "claude", color: "#d97757", icon: "claude", shortcut: "", builtin: true },
  { id: "claude-acp", name: "Claude (ACP)", transport: "acp", command: "npx", args: ["@agentclientprotocol/claude-agent-acp"], env: {}, kind: "claude", color: "#a855f7", icon: "claude", shortcut: "", builtin: true },
  { id: "gemini", name: "Gemini", transport: "acp", command: "gemini", args: ["--acp"], env: {}, kind: "gemini", color: "#1a73e8", icon: "gemini", shortcut: "", builtin: true },
  { id: "codex", name: "Codex", transport: "acp", command: "npx", args: ["@agentclientprotocol/codex-acp"], env: {}, kind: "codex", color: "#74aa9c", icon: "openai", shortcut: "", builtin: true },
  { id: "opencode", name: "opencode", transport: "acp", command: "opencode", args: ["acp"], env: {}, kind: "custom", color: "#f59e0b", icon: "terminal", shortcut: "", builtin: true },
];

function clone(list: ChatAgent[]): ChatAgent[] {
  return list.map((a) => ({ ...a, args: [...a.args], env: { ...a.env } }));
}

// Merge persisted agents over the built-in seeds: built-ins always present (so a
// new release's additions appear), but user edits to a built-in win.
function normalize(parsed: unknown): ChatAgent[] {
  const base = clone(BUILTIN_AGENTS);
  if (!Array.isArray(parsed)) return base;
  const saved = parsed as ChatAgent[];
  const byId = new Map(base.map((a) => [a.id, a]));
  for (const s of saved) {
    byId.set(s.id, { ...byId.get(s.id), ...s, args: [...(s.args ?? [])], env: { ...(s.env ?? {}) }, shortcut: s.shortcut ?? byId.get(s.id)?.shortcut ?? "" } as ChatAgent);
  }
  return Array.from(byId.values());
}

export const useChatAgentsStore = defineStore("chatAgents", () => {
  const agents = ref<ChatAgent[]>(clone(BUILTIN_AGENTS));

  configReady.then(() => {
    migrateFromLocalStorage(LEGACY_STORAGE_KEY, CONFIG_KEY);
    agents.value = normalize(getConfig<unknown>(CONFIG_KEY, clone(BUILTIN_AGENTS)));
  });

  watch(agents, (val) => setConfig(CONFIG_KEY, val), { deep: true });

  function byId(id: string): ChatAgent {
    return agents.value.find((a) => a.id === id) ?? agents.value[0];
  }

  function add(): ChatAgent {
    const id = `custom-${agents.value.length}-${Date.now().toString(36)}`;
    const a: ChatAgent = { id, name: "New Agent", transport: "acp", command: "", args: [], env: {}, kind: "custom", color: "#9ca3af", icon: "robot", shortcut: "" };
    agents.value.push(a);
    return a;
  }

  function remove(id: string) {
    const a = byId(id);
    if (a?.builtin) return; // built-ins can be edited but not deleted
    agents.value = agents.value.filter((x) => x.id !== id);
  }

  function reset(id: string) {
    const def = BUILTIN_AGENTS.find((b) => b.id === id);
    if (!def) return;
    const i = agents.value.findIndex((a) => a.id === id);
    if (i !== -1) agents.value[i] = { ...def, args: [...def.args], env: { ...def.env } };
  }

  return { agents, byId, add, remove, reset };
});
