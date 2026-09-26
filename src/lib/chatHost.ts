import { isPermissionGranted, requestPermission, sendNotification } from "@tauri-apps/plugin-notification";
import type { CanUseToolReq } from "@/lib/chatTypes";
import type { ClaudeSession } from "@/stores/claudeChats";
import { useClaudeChatsStore } from "@/stores/claudeChats";
import { useSubagentsStore } from "@/stores/subagents";
import { useNotificationsStore } from "@/stores/notifications";
import { useUIStore, type NtfyEvent } from "@/stores/ui";
import { playSound } from "@/lib/sounds";
import { notifyNtfy } from "@/lib/ntfy";

/**
 * What a chat session needs from the rest of the app, with or without a view
 * on screen: the shared chat list, the sub-agent registry, and the user's
 * attention (toast, sound, ntfy, OS notification).
 *
 * The seam exists for the tests — the real host is Pinia stores and Tauri
 * plugins, a test hands in a recording fake.
 */
export interface ChatHost {
  /** The chat's row in the shared list (title, workspace, transport…). */
  chat(chatId: number): ClaudeSession | undefined;
  syncChat(chatId: number, patch: Parameters<ReturnType<typeof useClaudeChatsStore>["sync"]>[1]): void;
  recordTurn(inputTokens: number, outputTokens: number): void;
  hasPermissionRule(keys: string[]): boolean;
  subagentStarted(chatId: number, toolCallId: string, input: Record<string, unknown> | undefined): void;
  subagentCompleted(toolCallId: string, failed: boolean): void;
  /** A turn finished. `watching`: the user saw it finish (no chime). */
  notifyDone(chatId: number, watching: boolean): void;
  /** The agent is blocked on a permission / question / plan decision. */
  notifyPermission(chatId: number, request: CanUseToolReq): void;
}

function maybeNtfy(event: NtfyEvent, message: string) {
  const ui = useUIStore();
  if (!ui.ntfyEnabled || !ui.ntfyTopic) return;
  if (!ui.ntfyEvents.includes(event)) return;
  if (ui.ntfyOnlyWhenAway && document.hasFocus()) return;
  notifyNtfy(
    { server: ui.ntfyServer, topic: ui.ntfyTopic, token: ui.ntfyToken || undefined },
    event,
    message || "Chat",
  ).catch(() => {}); // best-effort: a failed push must never disrupt the UI
}

async function osNotify(title: string, body: string) {
  if (document.hasFocus()) return;
  let granted = await isPermissionGranted();
  if (!granted) granted = (await requestPermission()) === "granted";
  if (granted) sendNotification({ title, body });
}

/** The app's host: Pinia stores, resolved at call time (after Pinia is up). */
export const appChatHost: ChatHost = {
  chat: (chatId) => useClaudeChatsStore().sessions.find((s) => s.id === chatId),
  syncChat: (chatId, patch) => useClaudeChatsStore().sync(chatId, patch),
  recordTurn: (inp, out) => useClaudeChatsStore().recordTurn(inp, out),
  hasPermissionRule: (keys) => useClaudeChatsStore().hasPermissionRule(keys),
  subagentStarted: (chatId, id, input) => useSubagentsStore().started(chatId, id, input),
  subagentCompleted: (id, failed) => useSubagentsStore().completed(id, failed),
  notifyDone(chatId, watching) {
    const chat = appChatHost.chat(chatId);
    const body = chat?.title || "Claude finished";
    useNotificationsStore().push({ type: "done", title: "Claude", body, workspaceId: chat?.workspaceId, source: "agent" });
    // Mirror Terminal.vue: no chime while the user is watching the turn finish.
    if (!watching) playSound("done");
    maybeNtfy("done", body);
    void osNotify("Burrow", `✓ ${body}`);
  },
  notifyPermission(chatId, cr) {
    const target = (cr.input?.command ?? cr.input?.file_path ?? cr.input?.path ?? cr.description ?? "") as string;
    const body = target ? `${cr.toolName}: ${String(target).slice(0, 80)}` : cr.toolName;
    useNotificationsStore().push({ type: "info", title: "Povolení", body, workspaceId: appChatHost.chat(chatId)?.workspaceId, source: "agent" });
    playSound("waiting");
    maybeNtfy("permission", body);
    void osNotify("Burrow — povolení", body);
  },
};
