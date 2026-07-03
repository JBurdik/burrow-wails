import { defineStore } from "pinia";
import { ref, watch } from "vue";
import { configReady, getConfig, setConfig, migrateFromLocalStorage } from "../lib/config";

export type AgentIcon = "sparkle" | "code" | "git-branch" | "robot" | "terminal" | "claude" | "openai" | "github-copilot";

export interface AgentConfig {
  id: string;
  name: string;
  command: string;
  args: string;
  shortcut: string;
  color: string;
  icon: AgentIcon;
}

const CONFIG_KEY = "agentPresets";
const LEGACY_STORAGE_KEY = "agentic-ide.agents";

// Maps built-in agent IDs to their canonical icon (upgraded when loading old localStorage data).
const ICON_MIGRATIONS: Record<string, AgentIcon> = {
  claude: "claude",
  codex: "openai",
  gh: "git-branch",
  "gh-copilot": "github-copilot",
};

const DEFAULTS: AgentConfig[] = [
  { id: "claude", name: "Claude Code", command: "claude", args: "--dangerously-skip-permissions", shortcut: "⌘⇧1", color: "#d97757", icon: "claude" },
  { id: "codex", name: "Codex", command: "codex", args: "", shortcut: "⌘⇧2", color: "#34d399", icon: "openai" },
  { id: "gh-copilot", name: "GitHub Copilot", command: "copilot", args: "", shortcut: "⌘⇧3", color: "#8957e5", icon: "github-copilot" },
  { id: "aider", name: "Aider", command: "aider", args: "", shortcut: "⌘⇧4", color: "#fbbf24", icon: "robot" },
  { id: "cursor", name: "Cursor AI", command: "cursor-agent", args: "", shortcut: "⌘⇧5", color: "#f472b6", icon: "terminal" },
];

function normalize(parsed: unknown): AgentConfig[] {
  if (Array.isArray(parsed) && parsed.length) {
    return parsed.map((a) => {
      const base: AgentConfig = { args: "", shortcut: "", icon: "robot", color: "#888888", ...a };
      if (ICON_MIGRATIONS[base.id]) base.icon = ICON_MIGRATIONS[base.id];
      return base;
    });
  }
  return clone(DEFAULTS);
}

function clone(list: AgentConfig[]): AgentConfig[] {
  return list.map((a) => ({ ...a }));
}

let counter = 0;
function makeId(): string {
  counter++;
  return `agent-${counter}-${counter * 7 + 13}`;
}

export const useAgentsStore = defineStore("agents", () => {
  const agents = ref<AgentConfig[]>(clone(DEFAULTS));

  configReady.then(() => {
    migrateFromLocalStorage(LEGACY_STORAGE_KEY, CONFIG_KEY);
    agents.value = normalize(getConfig<unknown>(CONFIG_KEY, clone(DEFAULTS)));
  });

  watch(
    agents,
    (val) => setConfig(CONFIG_KEY, val),
    { deep: true },
  );

  function add() {
    const palette = ["#a78bfa", "#34d399", "#60a5fa", "#f472b6", "#fbbf24", "#22d3ee"];
    agents.value.push({
      id: makeId(),
      name: "New Agent",
      command: "",
      args: "",
      shortcut: "",
      color: palette[agents.value.length % palette.length],
      icon: "robot",
    });
  }

  function addFromTemplate(t: { name: string; command: string; args: string; color: string; icon: AgentIcon }) {
    agents.value.push({
      id: makeId(),
      name: t.name,
      command: t.command,
      args: t.args,
      shortcut: "",
      color: t.color,
      icon: t.icon,
    });
  }

  function update(id: string, patch: Partial<Omit<AgentConfig, "id">>) {
    const a = agents.value.find((x) => x.id === id);
    if (a) Object.assign(a, patch);
  }

  function remove(id: string) {
    agents.value = agents.value.filter((x) => x.id !== id);
  }

  function reset() {
    agents.value = clone(DEFAULTS);
  }

  // Reorder: move the agent at index `from` to index `to`. Toolbar renders in
  // array order, so this drives the toolbar order too.
  function move(from: number, to: number) {
    const list = agents.value;
    if (from < 0 || from >= list.length || to < 0 || to >= list.length || from === to) return;
    const [item] = list.splice(from, 1);
    list.splice(to, 0, item);
  }

  // Full command line used to launch the agent in a terminal.
  function commandLine(a: AgentConfig): string {
    return a.args.trim() ? `${a.command} ${a.args}`.trim() : a.command.trim();
  }

  return { agents, add, addFromTemplate, update, remove, move, reset, commandLine };
});
