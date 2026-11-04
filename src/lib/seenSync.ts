import { invoke } from "@tauri-apps/api/core";
import { listen } from "@tauri-apps/api/event";

// Read receipts shared across devices (src-wails/seenreceipts.go). Each client
// still keeps `burrow.seenAt` in localStorage as its cache and writes it
// optimistically; this module tells Go and folds every other client's receipt
// back in. Go stamps the time, so a skewed phone clock can't pre-read a turn.
// Keys: `chat:<id>` for chats, the bare pty id for terminal leaves.

const KEY = "burrow.seenAt";
type Sub = (key: string, at: number) => void; // at 0 = cleared
const subs = new Set<Sub>();
let started = false;

function store(key: string, at: number) {
  try {
    const stored = JSON.parse(localStorage.getItem(KEY) ?? "{}") as Record<string, number>;
    if (at) stored[key] = at;
    else delete stored[key];
    localStorage.setItem(KEY, JSON.stringify(stored));
  } catch { /* receipts are best effort */ }
  for (const fn of subs) fn(key, at);
}

function start() {
  if (started) return;
  started = true;
  void listen<{ key: string; at: number }>("seen-changed", (e) => {
    if (e.payload?.key) store(e.payload.key, e.payload.at);
  }).catch(() => {});
  void invoke<{ key: string; at: number }[]>("seen_list")
    .then((rows) => {
      // ponytail: on boot the server only moves a receipt forward; a local
      // receipt the server never heard of (pre-sync) stays local.
      let local: Record<string, number> = {};
      try { local = JSON.parse(localStorage.getItem(KEY) ?? "{}"); } catch { /* corrupt cache */ }
      for (const r of rows ?? []) if (r.at > (local[r.key] ?? 0)) store(r.key, r.at);
    })
    .catch(() => {});
}

/** This client looked at `key`; every other client stops showing it unread. */
export function shareSeen(key: string): void {
  void invoke("seen_mark", { key }).catch(() => {});
}

/** Called for every receipt that arrives from Go (incl. this client's own echo). */
export function onSeenChanged(fn: Sub): () => void {
  subs.add(fn);
  start();
  return () => subs.delete(fn);
}
