// Paired-device glue for the connection indicator and the offline outbox
// (pure halves: src/runtime/linkStatus.ts, src/runtime/remoteCache.ts).
// Inert on the desktop's own backend: `remoteActive()` is false there, so
// nothing is held and the indicator never shows.
import { computed, ref } from "vue";
import { appTransport, invoke, remoteActive } from "@/lib/wailsCompat/core";
import { activeEnvironmentId } from "@/runtime/remoteEndpoint";
import { createLinkStatus, type LinkState } from "@/runtime/linkStatus";
import {
  drainOutbox, heldCall, isOfflineError, outboxAdd, outboxKey, outboxRead, outboxRemove, outboxWrite, type HeldMessage,
} from "@/runtime/remoteCache";

export { isOfflineError, type HeldMessage };

export const linkState = ref<LinkState>("up");
const held = ref<HeldMessage[]>([]);
let up = true;
let env = "";
let started = false;

/** Subscribes once (App.vue calls it; the outbox calls it lazily too). */
export function startOffline(): void {
  if (started || !remoteActive()) return;
  started = true;
  env = activeEnvironmentId() ?? "";
  held.value = outboxRead(env);
  const status = createLinkStatus((s) => { linkState.value = s; });
  appTransport().onState((nowUp) => {
    up = nowUp;
    status.set(nowUp);
    if (nowUp) void deliverHeld();
  });
  // Another tab of the same PWA held or delivered something.
  window.addEventListener("storage", (e) => {
    if (e.key === outboxKey(env)) held.value = outboxRead(env);
  });
}

/** True when a paired device's socket is down right now. */
export function linkDown(): boolean {
  startOffline();
  return started && !up;
}

export function wakeLink(): void {
  if (remoteActive()) appTransport().wake();
}

export function heldFor(chatId: number): HeldMessage[] {
  return held.value.filter((m) => m.chatId === chatId);
}

function commit(next: HeldMessage[]): boolean {
  if (!outboxWrite(env, next)) return false;
  held.value = next;
  return true;
}

/** A typed message's client id: chat_submit / chat_steer (and the older
 *  chat_send / chat_queue_add) take it as `clientId`, and Go accepts each id
 *  once (src-wails/chatsubmit.go), also after a core restart. */
export function newClientId(): string {
  return globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2)}`;
}

/**
 * Persist a message for later delivery; `clientId` (the held id) rides along on
 * every retry, so a call that already reached Go before the socket dropped is
 * not delivered twice. False = not held (not remote, or storage full).
 */
export function holdMessage(
  chatId: number, text: string, images?: string[], clientId = newClientId(), answer?: HeldMessage["answer"],
): boolean {
  startOffline();
  if (!started || !env) return false;
  const item: HeldMessage = { id: clientId, chatId, text, ...(images?.length ? { images } : {}), ...(answer ? { answer } : {}), at: Date.now() };
  if (!commit(outboxAdd(outboxRead(env), item))) return false;
  if (up) void deliverHeld();
  return true;
}

export function dropHeld(id: string): void {
  if (env) commit(outboxRemove(outboxRead(env), id));
}

let draining: Promise<number> | null = null;
let again = false;
/** Deliver everything held, in order, through chat_submit (an end_turn answer: chat_answer_turn) with the held id (Go sends or queues it). */
export function deliverHeld(): Promise<number> {
  if (!started || !env) return Promise.resolve(0);
  if (draining) {
    // Held while a pass runs: it may already have looked past the new tail.
    again = true;
    return draining;
  }
  again = false;
  draining = drainOutbox({
    read: () => outboxRead(env),
    send: async (m) => { await invoke(...heldCall(m)); },
    remove: (id) => { commit(outboxRemove(outboxRead(env), id)); },
    fail: (id, error) => { commit(outboxRead(env).map((m) => (m.id === id ? { ...m, error } : m))); },
  }).finally(() => {
    draining = null;
    if (again && up) void deliverHeld();
  });
  return draining;
}

/** For the indicator: how many messages wait, all chats. */
export const heldCount = computed(() => held.value.length);
