// Shared persistence for ACP/Codex composer selections. Both the welcome
// composer and an already-running chat must use these same records so a choice
// made before the app-server thread exists is restored once it starts.

import { getConfig, setConfig } from "@/lib/config";

export type AcpSetting = "mode" | "model" | "effort";
export type AcpChatSettings = Partial<Record<AcpSetting, string>>;

const CHAT_SETTINGS_KEY = "chatAcpSettings";
const LAST_SETTINGS_KEY = "chatAcpLast";

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
