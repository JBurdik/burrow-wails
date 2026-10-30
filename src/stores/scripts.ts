import { defineStore } from "pinia";
import { ref } from "vue";
import { invoke } from "@tauri-apps/api/core";

export interface ProjectSettings {
  claude_config_dir?: string  // override CLAUDE_CONFIG_DIR for this project
  env_file?: string           // path to .env relative to project root (default: .env)
}

// A Script is an ORDERED LIST OF STEPS run SEQUENTIALLY in one terminal tab.
// Steps are chained into a single shell command line:
//   continueOnError = false → "cmd1 && cmd2"  (next runs only if prev SUCCEEDED)
//   continueOnError = true  → "cmd1 ; cmd2"   (next runs regardless)
export interface Script {
  id: string;
  name: string;
  steps: string[];
  continueOnError: boolean;
  /** Run in every new worktree of this repo, right after it is created. */
  runOnWorktreeCreate?: boolean;
  icon?: string;
  color?: string;
}

function normalize(s: Record<string, unknown>): Script {
  return {
    id: String(s?.id ?? makeId()),
    name: String(s?.name ?? "Script"),
    steps: Array.isArray(s?.steps) ? (s.steps as unknown[]).map((x) => String(x)) : [],
    continueOnError: !!s?.continueOnError,
    runOnWorktreeCreate: !!s?.runOnWorktreeCreate,
    icon: s?.icon != null ? String(s.icon) : undefined,
    color: s?.color != null ? String(s.color) : undefined,
  };
}

let counter = 0;
function makeId(): string {
  counter++;
  return `script-${counter}-${counter * 7 + 13}-${counter * 31 + 5}`;
}

const PALETTE = ["#60a5fa", "#34d399", "#a78bfa", "#f472b6", "#fbbf24", "#22d3ee"];

function parseTomlValue(val: string): unknown {
  if (val === "true") return true;
  if (val === "false") return false;
  if (val.startsWith("[") && val.endsWith("]")) {
    const inner = val.slice(1, -1).trim();
    if (!inner) return [];
    // Simple CSV split respecting quoted strings
    const items: unknown[] = [];
    let cur = "";
    let inQ = false;
    for (let i = 0; i < inner.length; i++) {
      const ch = inner[i];
      if (ch === '"' && inner[i - 1] !== "\\") { inQ = !inQ; continue; }
      if (ch === "," && !inQ) { items.push(cur); cur = ""; continue; }
      cur += ch;
    }
    if (cur.trim()) items.push(cur.trim());
    return items;
  }
  if ((val.startsWith('"') && val.endsWith('"')) || (val.startsWith("'") && val.endsWith("'"))) {
    return val.slice(1, -1).replace(/\\"/g, '"').replace(/\\\\/g, "\\");
  }
  return val;
}

function parseSettingsToml(content: string): ProjectSettings {
  const settings: ProjectSettings = {};
  // Match [settings] block up to the next section header or EOF
  const m = content.match(/(?:^|\n)\[settings\]\n([\s\S]*?)(?=\n\[|\s*$)/);
  if (!m) return settings;
  for (const line of m[1].split("\n")) {
    const kv = line.match(/^\s*(\w+)\s*=\s*(.+?)\s*$/);
    if (!kv) continue;
    const val = parseTomlValue(kv[2].trim());
    if (typeof val === "string") (settings as Record<string, string>)[kv[1]] = val;
  }
  return settings;
}

function serializeSettingsToml(settings: ProjectSettings): string {
  const pairs = (Object.entries(settings) as [string, string | undefined][]).filter(
    ([, v]) => v != null && v !== "",
  );
  if (!pairs.length) return "";
  return "[settings]\n" + pairs.map(([k, v]) => `${k} = "${escapeToml(String(v))}"`).join("\n") + "\n\n";
}

function parseScriptsToml(content: string): Script[] {
  const scripts: Script[] = [];
  const sections = content.split(/^\[\[scripts\]\]\s*$/m).slice(1);
  for (const section of sections) {
    const raw: Record<string, unknown> = {};
    for (const line of section.split("\n")) {
      const m = line.match(/^\s*(\w+)\s*=\s*(.+?)\s*$/);
      if (!m) continue;
      raw[m[1]] = parseTomlValue(m[2].trim());
    }
    if (raw.id || raw.name) scripts.push(normalize(raw));
  }
  return scripts;
}

function escapeToml(s: string): string {
  return s.replace(/\\/g, "\\\\").replace(/"/g, '\\"');
}

function serializeScriptsToml(scripts: Script[]): string {
  return scripts.map((s) => {
    const stepsArr = "[" + s.steps.map((x) => `"${escapeToml(x)}"`).join(", ") + "]";
    let block = `[[scripts]]\n`;
    block += `id = "${escapeToml(s.id)}"\n`;
    block += `name = "${escapeToml(s.name)}"\n`;
    block += `steps = ${stepsArr}\n`;
    block += `continueOnError = ${s.continueOnError}\n`;
    if (s.runOnWorktreeCreate) block += `runOnWorktreeCreate = true\n`;
    if (s.color) block += `color = "${escapeToml(s.color)}"\n`;
    if (s.icon) block += `icon = "${escapeToml(s.icon)}"\n`;
    return block;
  }).join("\n");
}

export const useScriptsStore = defineStore("scripts", () => {
  const scriptsCache = ref<Record<string, Script[]>>({});
  const settingsCache = ref<Record<string, ProjectSettings>>({});

  async function loadForPath(workspacePath: string): Promise<Script[]> {
    try {
      const content = await invoke<string>("read_text_file", {
        path: workspacePath + "/.burrow/config.toml",
      });
      settingsCache.value[workspacePath] = parseSettingsToml(content);
      const parsed = parseScriptsToml(content);
      scriptsCache.value[workspacePath] = parsed;
      return parsed;
    } catch {
      settingsCache.value[workspacePath] = {};
      scriptsCache.value[workspacePath] = [];
      return [];
    }
  }

  async function saveForPath(workspacePath: string): Promise<void> {
    const scripts = scriptsCache.value[workspacePath] ?? [];
    const settings = settingsCache.value[workspacePath] ?? {};
    const content = serializeSettingsToml(settings) + serializeScriptsToml(scripts);
    await invoke("write_text_file", {
      path: workspacePath + "/.burrow/config.toml",
      content,
    });
  }

  function settingsFor(workspacePath: string | null | undefined): ProjectSettings {
    if (!workspacePath) return {};
    return settingsCache.value[workspacePath] ?? {};
  }

  function updateSettings(workspacePath: string, patch: Partial<ProjectSettings>): void {
    settingsCache.value[workspacePath] = { ...(settingsCache.value[workspacePath] ?? {}), ...patch };
    saveForPath(workspacePath);
  }

  function scriptsFor(workspacePath: string | null | undefined): Script[] {
    if (!workspacePath) return [];
    return scriptsCache.value[workspacePath] ?? [];
  }

  function addScript(workspacePath: string): void {
    const list = scriptsCache.value[workspacePath] ?? [];
    list.push({
      id: makeId(),
      name: "New Script",
      steps: [""],
      continueOnError: false,
      color: PALETTE[list.length % PALETTE.length],
    });
    scriptsCache.value[workspacePath] = list;
    saveForPath(workspacePath);
  }

  function updateScript(
    workspacePath: string,
    id: string,
    patch: Partial<Omit<Script, "id">>,
  ): void {
    const s = (scriptsCache.value[workspacePath] ?? []).find((x) => x.id === id);
    if (s) Object.assign(s, patch);
    saveForPath(workspacePath);
  }

  function removeScript(workspacePath: string, id: string): void {
    const list = scriptsCache.value[workspacePath];
    if (!list) return;
    scriptsCache.value[workspacePath] = list.filter((x) => x.id !== id);
    saveForPath(workspacePath);
  }

  // Join steps into a single shell line. && (stop on first failure) by default;
  // ; (continue regardless) when continueOnError is set.
  function commandLine(s: Script): string {
    const steps = s.steps.map((x) => x.trim()).filter(Boolean);
    return steps.join(s.continueOnError ? " ; " : " && ");
  }

  return {
    scriptsCache,
    settingsCache,
    scriptsFor,
    settingsFor,
    updateSettings,
    loadForPath,
    addScript,
    updateScript,
    removeScript,
    commandLine,
  };
});
