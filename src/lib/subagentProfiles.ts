import { getConfig, setConfig } from "@/lib/config";
import { shellQuote } from "@/lib/agentCommand";
import type { ProviderInstance } from "@/lib/providers";

export type ProfilePermission = "default" | "auto" | "plan";
export interface SubagentProfile {
  id: string;
  name: string;
  instructions: string;
  agentId: string;
  model: string;
  permissionMode: ProfilePermission;
}
export interface SubagentLaunchSettings {
  agentId: string;
  model: string;
  permissionMode: ProfilePermission;
}
export const PROFILE_PERMISSION_LABELS: Record<ProfilePermission, string> = {
  default: "Supervised", auto: "Auto", plan: "Plan (no workspace writes)",
};
export function defaultSubagentProfiles(): SubagentProfile[] {
  return [
    { id: "scout", name: "Scout", agentId: "", model: "", permissionMode: "plan", instructions: "Investigate the task and locate relevant code. Do not change files. Report concrete findings, file paths, and a suggested approach to the parent agent." },
    { id: "worker", name: "Worker", agentId: "", model: "", permissionMode: "default", instructions: "Implement the assigned task within its stated scope. Respect repository instructions and other agents' work. Run appropriate checks and report the changes, validation, and any remaining issues." },
    { id: "reviewer", name: "Reviewer", agentId: "", model: "", permissionMode: "plan", instructions: "Review the assigned changes for bugs, regressions, and missing validation. Do not change files. Report actionable findings ordered by severity, with file paths, evidence, and suggested fixes. Say explicitly when no findings remain." },
  ];
}
export function normalizeSubagentProfiles(value: unknown): SubagentProfile[] {
  const rows = Array.isArray(value) ? value : [];
  return defaultSubagentProfiles().map((fallback) => {
    const saved = rows.find((row) => row && typeof row === "object" && row.id === fallback.id);
    if (!saved) return fallback;
    return { ...fallback,
      instructions: typeof saved.instructions === "string" ? saved.instructions : fallback.instructions,
      agentId: typeof saved.agentId === "string" ? saved.agentId : "",
      model: typeof saved.model === "string" ? saved.model : "",
      permissionMode: ["default", "auto", "plan"].includes(saved.permissionMode) ? saved.permissionMode : fallback.permissionMode,
    };
  });
}
export function profileTask(profile: SubagentProfile, task: string): string {
  return `Role: ${profile.name}\n\n${profile.instructions.trim()}\n\nTask:\n${task.trim()}`;
}
export function assertProfileProvider(agent: ProviderInstance): void {
  if (!agent.enabled || !["claude-cli", "codex-app-server"].includes(agent.transport)) {
    throw new Error("Sub-agent profiles require an enabled Claude CLI or Codex app-server provider. Choose one in Settings > Sub-agent profiles.");
  }
}
/** Seed only this child, before it becomes observable. Never change global defaults. */
export function seedSubagentSettings(chatId: number, settings: SubagentLaunchSettings): void {
  const key = String(chatId);
  const providers = getConfig<Record<string, string>>("chatProfileSelection", {});
  setConfig("chatProfileSelection", { ...providers, [key]: settings.agentId });
  const permissions = getConfig<{ byChat?: Record<string, string>; dangerousByChat?: Record<string, boolean> }>("chatPermissionMode", {});
  setConfig("chatPermissionMode", { ...permissions, byChat: { ...permissions.byChat, [key]: settings.permissionMode } });
  const models = getConfig<Record<string, string>>("chatModelByChat", {});
  if (settings.model) setConfig("chatModelByChat", { ...models, [key]: settings.model });
  const acp = getConfig<Record<string, Record<string, string>>>("chatAcpSettings", {});
  setConfig("chatAcpSettings", { ...acp, [key]: {
    ...acp[key], mode: settings.permissionMode === "default" ? "supervised" : settings.permissionMode,
    ...(settings.model ? { model: settings.model } : {}),
  } });
}
/** Native Codex must finish restoring the profile before its first prompt. */
export async function restoreSubagentCodexSettings(chatId: number, invoke: <T>(command: string, args?: Record<string, unknown>) => Promise<T>): Promise<void> {
  const saved = getConfig<Record<string, { mode?: string; model?: string }>>("chatAcpSettings", {})[String(chatId)];
  if (saved?.mode) await invoke("acp_set_mode", { id: chatId, modeId: saved.mode });
  if (saved?.model) await invoke("acp_set_config", { id: chatId, configId: "model", value: saved.model });
}
export function profileTerminalCommand(agent: ProviderInstance, settings: SubagentLaunchSettings, task: string): string {
  assertProfileProvider(agent);
  if (agent.args.length || agent.terminalArgs.trim()) {
    throw new Error("Terminal role profiles cannot be combined with custom provider arguments. Use a provider without extra arguments or launch the role in a chat.");
  }
  const env = { ...agent.env, ...(agent.kind === "claude" && agent.configDir ? { CLAUDE_CONFIG_DIR: agent.configDir } : {}) };
  if (Object.keys(env).some((key) => !/^[A-Za-z_][A-Za-z0-9_]*$/.test(key))) {
    throw new Error("The selected provider has an invalid environment variable name.");
  }
  const parts = Object.keys(env).length ? ["env", ...Object.entries(env).map(([key, value]) => shellQuote(`${key}=${value}`))] : [];
  parts.push(shellQuote(agent.binary || agent.kind));
  if (settings.model) parts.push("--model", shellQuote(settings.model));
  if (agent.kind === "claude") parts.push("--permission-mode", settings.permissionMode);
  else if (settings.permissionMode === "auto") parts.push("--approve-for-me");
  else parts.push("--sandbox", settings.permissionMode === "plan" ? "read-only" : "workspace-write", "--ask-for-approval", "on-request", "-c", shellQuote('approvals_reviewer="user"'));
  parts.push(shellQuote(task));
  return parts.join(" ");
}
