// Shared persistence for ACP/Codex composer selections. Both the welcome
// composer and an already-running chat must use these same records so a choice
// made before the app-server thread exists is restored once it starts.

import { getConfig, setConfig } from "@/lib/config";
import type { AcpConfigOption, AcpModes } from "@/lib/chatTypes";

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
