// Per-chat settings records in config.json. Each is a flat map keyed by chat id
// (optionally prefixed by an AgentChat `modelKey`, e.g. "burrow.manager.model:7"),
// so a chat's model / effort / permission mode belongs to THAT chat and can't be
// moved by another chat's pick — see AgentChat.vue's loadModel/loadEffort.
//
// They live here, not in AgentChat.vue, because deleting a chat has to forget
// them and the key strings must not drift between writer and cleaner.

import { getConfig, setConfig } from "@/lib/config";

/** Records shaped `{ [chatKey]: value }`. */
export const FLAT_CHAT_KEYS = ["chatModelByChat", "chatEffortByChat", "chatAcpSettings"] as const;

/** `chatPermissionMode` nests its per-chat maps one level down. */
const PERM_KEY = "chatPermissionMode";
const PERM_SUBKEYS = ["byChat", "dangerousByChat"] as const;

/** Config key for one chat's entry: bare id, or `modelKey`-scoped. */
export function chatSettingKey(chatId: number, modelKey?: string): string {
  return modelKey ? `${modelKey}:${chatId}` : String(chatId);
}

/** Drop every stored setting for a permanently deleted chat. Matches both the
 *  bare id and any `modelKey`-scoped variant, since the deleter doesn't know
 *  which AgentChat instance wrote the entry. */
export function forgetChatSettings(chatId: number): void {
  const bare = String(chatId);
  const owns = (k: string) => k === bare || k.endsWith(`:${bare}`);
  const prune = (rec: Record<string, unknown>) => {
    const kept = Object.fromEntries(Object.entries(rec).filter(([k]) => !owns(k)));
    return { changed: Object.keys(kept).length !== Object.keys(rec).length, kept };
  };

  for (const key of FLAT_CHAT_KEYS) {
    const { changed, kept } = prune(getConfig<Record<string, unknown>>(key, {}));
    if (changed) setConfig(key, kept);
  }

  const perm = getConfig<Record<string, unknown>>(PERM_KEY, {});
  let permChanged = false;
  const next = { ...perm };
  for (const sub of PERM_SUBKEYS) {
    const rec = perm[sub];
    if (!rec || typeof rec !== "object") continue;
    const { changed, kept } = prune(rec as Record<string, unknown>);
    if (changed) { next[sub] = kept; permChanged = true; }
  }
  if (permChanged) setConfig(PERM_KEY, next);
}
