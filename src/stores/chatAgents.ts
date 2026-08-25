import { defineStore } from "pinia";
import { ref, watch } from "vue";
import { configReady, getConfig, setConfig, migrateFromLocalStorage } from "../lib/config";

// A chat agent uses one of three honest runtime contracts: Claude's native
// stream-json CLI, Codex's native JSON-RPC app-server, or a generic ACP adapter.
export type ChatTransport = "claude-cli" | "codex-app-server" | "acp";

export function transportLabel(transport: ChatTransport): string {
  if (transport === "claude-cli") return "Claude CLI";
  if (transport === "codex-app-server") return "Codex app-server";
  return "ACP";
}

export interface ChatAgent {
  id: string;
  name: string;
  transport: ChatTransport;
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

// Built-in agents. Codex talks to its installed app-server directly, preserving
// the user's `codex login` session just like T3 Code. The remaining adapters use
// ACP where that is their native transport.
export const BUILTIN_AGENTS: ChatAgent[] = [
  { id: "claude", name: "Claude Code", transport: "claude-cli", command: "claude", args: [], env: {}, kind: "claude", color: "#d97757", icon: "claude", shortcut: "", builtin: true },
  { id: "gemini", name: "Gemini", transport: "acp", command: "gemini", args: ["--acp"], env: {}, kind: "gemini", color: "#1a73e8", icon: "gemini", shortcut: "", builtin: true },
  { id: "codex", name: "Codex", transport: "codex-app-server", command: "codex", args: ["app-server"], env: {}, kind: "codex", color: "#74aa9c", icon: "openai", shortcut: "", builtin: true },
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
    // `claude-acp` used to be a bundled duplicate of Claude Code. Retire only
    // that exact old seed; separately-created ACP agents stay available.
    const isRetiredClaudeAcpSeed =
      s.id === "claude-acp" &&
      s.name === "Claude (ACP)" &&
      s.transport === "acp" &&
      s.command === "npx" &&
      (s.args ?? []).some((arg) => arg.includes("@agentclientprotocol/claude-agent-acp"));
    if (isRetiredClaudeAcpSeed) continue;

    const merged = { ...byId.get(s.id), ...s, args: [...(s.args ?? [])], env: { ...(s.env ?? {}) }, shortcut: s.shortcut ?? byId.get(s.id)?.shortcut ?? "" } as ChatAgent;
    // Persisted releases called both native runtimes generic transport names.
    // Upgrade them in place so the UI and the actual protocol always agree.
    if ((merged.transport as string) === "stream-json") merged.transport = "claude-cli";
    if (merged.id === "codex" && merged.command === "codex" && merged.args.includes("app-server")) {
      merged.transport = "codex-app-server";
    }
    // Migrate the previous npx bridge automatically. Explicit user changes to
    // another command remain intact; only the old bundled preset is replaced.
    if (merged.id === "codex" && merged.command === "npx" && merged.args.includes("@agentclientprotocol/codex-acp")) {
      merged.command = "codex";
      merged.args = ["app-server"];
      merged.transport = "codex-app-server";
    }
    byId.set(s.id, merged);
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
