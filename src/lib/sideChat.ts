import { reactive } from "vue";
import { invoke } from "@tauri-apps/api/core";
import { useClaudeChatsStore } from "@/stores/claudeChats";

/** Which side chat the Side chat surface shows, per parent chat id. Module
 *  state so the composer's "Side chat" button can point the surface at the
 *  chat it just created; unset = the newest one. Not persisted. */
export const sideChatSelection = reactive<Record<number, number>>({});

/** Create (or reuse an unused) side chat of `parentChatId` — Go forks the
 *  parent's current session on its first send (src-wails/chatside.go) — and
 *  select it. Throws Go's refusal (generic ACP, no session yet). */
export async function newSideChat(parentChatId: number): Promise<number> {
  const chat = await invoke<{ id: number }>("chat_side_create", { chatId: parentChatId });
  // The surface looks the chat up in the store; don't wait for chats-changed.
  await useClaudeChatsStore().reload();
  sideChatSelection[parentChatId] = chat.id;
  return chat.id;
}
