import { invoke } from "@tauri-apps/api/core";
import type { ChatTransport } from "@/lib/providers";

/**
 * The frontend's one door to a chat's agent process, dispatching on transport
 * (docs/plans/006-chat-runtime.md). claude-cli chats are run by the Go chat
 * runtime (src-wails/chatruntime.go): `chat_send` starts the process itself
 * from the row's launch settings. Codex / generic ACP still need their
 * process started by a mounted AgentChat until they move into the runtime too.
 */

/** Launch settings as the chat row stores them. "" clears one back to the default. */
export interface LaunchSettingsPatch {
  model?: string;
  effort?: string;
  permission_mode?: string;
  profile_id?: string;
  /** Record only — leave a live process alone (it already runs this way, or the row is just being seeded). */
  row_only?: boolean;
}

export async function sendToChat(transport: ChatTransport | undefined, chatId: number, text: string, images: string[] = []): Promise<void> {
  if (transport === "codex-app-server") await invoke("codex_send", { id: chatId, text, images });
  else if (transport === "acp") await invoke("acp_send", { id: chatId, text, images });
  else await invoke("chat_send", { chatId, text, images });
}

export function setLaunchSettings(chatId: number, patch: LaunchSettingsPatch): Promise<void> {
  return invoke("chat_set_launch_settings", { chatId, patch });
}
