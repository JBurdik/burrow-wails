// Mini chat: a project-less chat in its own small floating window
// (src-wails/minichat.go). Go picks / creates the chat in the Scratch
// workspace and builds the route; this only opens the window.
import { invoke } from "@tauri-apps/api/core";
import { openWindow } from "@/lib/windows";

export const MINI_CHAT_KEY = "minichat";

interface MiniChat { chatId: number; workspaceId: number; cwd: string; route: string }

export function miniChat(fresh = false): Promise<MiniChat> {
  return invoke<MiniChat>("mini_chat", { fresh });
}

export async function openMiniChat(): Promise<void> {
  const m = await miniChat();
  await openWindow(MINI_CHAT_KEY, m.route, "Mini chat", 420, 600, true);
}
