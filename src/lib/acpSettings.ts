// Shared persistence for ACP/Codex composer selections. Both the welcome
// composer and an already-running chat must use these same records so a choice
// made before the app-server thread exists is restored once it starts.

import { getConfig, setConfig } from "@/lib/config";
import type { AcpConfigOption, AcpModes } from "@/lib/chatTypes";
import type { LaunchSettingsPatch } from "@/lib/chatRuntime";

export type AcpSetting = "mode" | "model" | "effort";
export type AcpChatSettings = Partial<Record<AcpSetting, string>>;

const CHAT_SETTINGS_KEY = "chatAcpSettings";
const LAST_SETTINGS_KEY = "chatAcpLast";
const CAPABILITIES_KEY = "chatAcpCapabilities";

export interface AcpCapabilities {
  agentId: string;
  modes: AcpModes | null;
  configOptions: AcpConfigOption[];
}

export function getAcpChatSetting(chatId: number, field: AcpSetting): string | undefined {
  return getConfig<Record<string, AcpChatSettings>>(CHAT_SETTINGS_KEY, {})[String(chatId)]?.[field];
}

export function getLastAcpSetting(agentId: string, field: AcpSetting): string | undefined {
  return getConfig<Record<string, AcpChatSettings>>(LAST_SETTINGS_KEY, {})[agentId]?.[field];
}

export function setLastAcpSetting(agentId: string, field: AcpSetting, value: string): void {
  const settings = { ...getConfig<Record<string, AcpChatSettings>>(LAST_SETTINGS_KEY, {}) };
  settings[agentId] = { ...settings[agentId], [field]: value };
  setConfig(LAST_SETTINGS_KEY, settings);
}

export function setAcpChatSetting(chatId: number, agentId: string, field: AcpSetting, value: string): void {
  const settings = { ...getConfig<Record<string, AcpChatSettings>>(CHAT_SETTINGS_KEY, {}) };
  settings[String(chatId)] = { ...settings[String(chatId)], [field]: value };
  setConfig(CHAT_SETTINGS_KEY, settings);
  setLastAcpSetting(agentId, field, value);
}

/** Last selector catalogue reported by this chat's runtime.
 *
 * ACP/Codex only sends this during the session handshake. Keeping the latest
 * catalogue lets a cold or remounted chat render the same effort and
 * permission controls immediately instead of dropping them until the process
 * starts again. The agent id prevents a chat switched to another provider
 * from briefly showing the previous provider's choices. */
export function getAcpCapabilities(chatId: number, agentId: string): AcpCapabilities | undefined {
  const cached = getConfig<Record<string, AcpCapabilities>>(CAPABILITIES_KEY, {})[String(chatId)];
  return cached?.agentId === agentId ? cached : undefined;
}

export function setAcpCapabilities(chatId: number, capabilities: AcpCapabilities): void {
  const all = { ...getConfig<Record<string, AcpCapabilities>>(CAPABILITIES_KEY, {}) };
  all[String(chatId)] = capabilities;
  setConfig(CAPABILITIES_KEY, all);
}

/**
 * What a fresh Codex / ACP chat's row must hold before its first send. Go
 * (ensureRPC) starts the process from the ROW, while the welcome composer keeps
 * its picks in chatAcpLast — an empty row ran the first turn on the CLI's
 * default model. Null when there is nothing to seed (resumed chat, row full).
 * `last` first for the model: welcome writes it on every pick, while its
 * selectedModel can still hold a Claude id before the Codex catalog lands.
 * Mode only for Codex: its ids are fixed, and a mode that fails to apply
 * refuses the start (fails closed) — a stale generic-ACP mode would brick it.
 */
export function rpcLaunchSeed(
  row: { model?: string; effort?: string; permissionMode?: string; claudeSessionId?: string },
  agentId: string,
  isCodex: boolean,
  defaultModel?: string,
): LaunchSettingsPatch | null {
  if (row.claudeSessionId) return null;
  const last = (f: AcpSetting) => getLastAcpSetting(agentId, f);
  const patch: LaunchSettingsPatch = { row_only: true };
  const model = last("model") || defaultModel;
  if (!row.model && model) patch.model = model;
  if (!row.effort && last("effort")) patch.effort = last("effort");
  if (!row.permissionMode && isCodex && last("mode")) patch.permission_mode = last("mode");
  return Object.keys(patch).length > 1 ? patch : null;
}
