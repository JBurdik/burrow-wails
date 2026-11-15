/**
 * A paired device's local copies, keyed by environment id (one per Mac in
 * remoteEndpoint.ts's catalog):
 *
 *  - read cache: the last reply of a few list-shaped reads (`CACHED_READS`) so
 *    the app paints before the socket opens. A cached `shell_snapshot` is only
 *    ever a first paint: nothing here touches the transport's seq — resume
 *    still comes from live data only.
 *  - outbox: chat messages typed while the socket was down, delivered in order
 *    via `chat_queue_add` once it is back. Removed only after a success.
 *
 * Pure over an injected Storage (localStorage by default); every write is
 * best effort except the outbox, whose failure the caller must see.
 */

export const CACHED_READS: ReadonlySet<string> = new Set(["shell_snapshot", "list_chats", "list_workspaces"]);

const CACHE_PREFIX = "burrow.cache.";
const OUTBOX_PREFIX = "burrow.outbox.";

type Store = Pick<Storage, "getItem" | "setItem" | "removeItem" | "key" | "length">;

function local(): Store | null {
  return typeof localStorage === "undefined" ? null : localStorage;
}

export const cacheKey = (env: string, cmd: string) => `${CACHE_PREFIX}${env}.${cmd}`;
export const outboxKey = (env: string) => `${OUTBOX_PREFIX}${env}`;

export function cacheWrite(env: string, cmd: string, value: unknown, store = local()): void {
  if (!store || !env || !CACHED_READS.has(cmd)) return;
  try {
    store.setItem(cacheKey(env, cmd), JSON.stringify(value));
  } catch {
    // quota: a cache that could not be written is just a slower first paint
  }
}

export function cacheRead<T>(env: string, cmd: string, store = local()): T | null {
  if (!store || !env) return null;
  try {
    const raw = store.getItem(cacheKey(env, cmd));
    return raw ? (JSON.parse(raw) as T) : null;
  } catch {
    return null;
  }
}

/** Drops every cached read of one environment (unpair, revoke, switch). The outbox stays. */
export function cacheClear(env: string, store = local()): void {
  if (!store || !env) return;
  const prefix = `${CACHE_PREFIX}${env}.`;
  const doomed: string[] = [];
  for (let i = 0; i < store.length; i++) {
    const k = store.key(i);
    if (k?.startsWith(prefix)) doomed.push(k);
  }
  for (const k of doomed) store.removeItem(k);
}

export interface HeldMessage {
  id: string;
  chatId: number;
  text: string;
  images?: string[];
  at: number;
  /** Last refusal that was not the network's (e.g. the chat is gone). */
  error?: string;
}

export function outboxRead(env: string, store = local()): HeldMessage[] {
  if (!store || !env) return [];
  try {
    const list = JSON.parse(store.getItem(outboxKey(env)) ?? "[]");
    return Array.isArray(list) ? list : [];
  } catch {
    return [];
  }
}

/** False when the write failed (quota) — the caller must not drop the text. */
export function outboxWrite(env: string, items: HeldMessage[], store = local()): boolean {
  if (!store || !env) return false;
  try {
    if (items.length) store.setItem(outboxKey(env), JSON.stringify(items));
    else store.removeItem(outboxKey(env));
    return true;
  } catch {
    return false;
  }
}

/** Appends unless that id is already held (a retried hold is one message). */
export function outboxAdd(items: HeldMessage[], item: HeldMessage): HeldMessage[] {
  return items.some((m) => m.id === item.id) ? items : [...items, item];
}

export function outboxRemove(items: HeldMessage[], id: string): HeldMessage[] {
  return items.filter((m) => m.id !== id);
}

/** A failure that means "no connection", not "the server said no". */
export function isOfflineError(e: unknown): boolean {
  return /disconnected|transport unreachable|transport closed|no endpoint/i.test(String(e instanceof Error ? e.message : e));
}

/**
 * Sends held messages oldest first. An offline error stops the whole drain
 * (the rest waits for the next reconnect); any other refusal marks that
 * message and skips the rest of ITS chat — a later message must never overtake
 * an earlier one in the same chat — while other chats go on.
 *
 * `read` is re-read after every step, so a message deleted mid-drain is not sent.
 */
export async function drainOutbox(io: {
  read(): HeldMessage[];
  send(m: HeldMessage): Promise<void>;
  remove(id: string): void;
  fail(id: string, error: string): void;
}): Promise<number> {
  let sent = 0;
  const blocked = new Set<number>();
  const tried = new Set<string>();
  for (;;) {
    const next = io.read().find((m) => !tried.has(m.id) && !blocked.has(m.chatId));
    if (!next) return sent;
    tried.add(next.id);
    try {
      await io.send(next);
    } catch (e) {
      if (isOfflineError(e)) return sent;
      io.fail(next.id, String(e instanceof Error ? e.message : e));
      blocked.add(next.chatId);
      continue;
    }
    io.remove(next.id);
    sent++;
  }
}
