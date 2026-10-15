import { invoke } from "@tauri-apps/api/core";

// Launch settings live on the chat row (docs/plans/006-chat-runtime.md); the
// Go chat runtime (src-wails/chatruntime.go) starts every chat's process from
// it. Sending, restarting, clearing and interrupting are ChatSession intents
// (lib/chatSession.ts).

/** Launch settings as the chat row stores them. "" clears one back to the default. */
export interface LaunchSettingsPatch {
  model?: string;
  effort?: string;
  permission_mode?: string;
  profile_id?: string;
  /** Record only — leave a live process alone (it already runs this way, or the row is just being seeded). */
  row_only?: boolean;
}

export function setLaunchSettings(chatId: number, patch: LaunchSettingsPatch): Promise<void> {
  return invoke("chat_set_launch_settings", { chatId, patch });
}
