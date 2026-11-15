import type { ChatMessage } from "@/lib/chatTypes";

// A chat's transcript as the client holds it: a window of Go's rows, fed only
// by page reads, versioned splices, streaming tail appends and chat_subscribe
// (ADR 0001 phase B, src-wails/chatsplice.go). Go is the only writer; nothing
// here folds stream events or saves anything. Pure, so the rules are table-tested.

/** `chat-messages-changed-<id>`: transcript[fromIndex:] = messages, on top of baseRev. */
export interface ChatSplice {
  baseRev: number;
  rev: number;
  fromIndex: number;
  messages: ChatMessage[];
  /** Rewind / clear / a subscribe ring miss: applies whatever the client holds. */
  authoritative?: boolean;
}

/** `chat-tail-<id>`: text appended to the streaming row at `index` (`offset` = its UTF-16 length before). */
export interface ChatTail {
  rev: number;
  index: number;
  offset: number;
  append: string;
  kind: "text" | "thinking";
}

/** `chat_subscribe(chatId, sinceRev)` reply. */
export interface ChatSubscribeReply {
  rev: number;
  splices: ChatSplice[];
  /** Full text so far of the streaming last row. */
  inProgress: { index: number; text: string } | null;
}

/** Rows [start, start + rows.length) of the transcript, as of splice revision `rev` (-1 = nothing loaded yet). */
export interface TranscriptWindow {
  start: number;
  rows: ChatMessage[];
  rev: number;
}

/** A row's id is its transcript index: unique in any window, and what rewind / fork / load_chat_image take. */
function indexed(from: number, messages: ChatMessage[]): ChatMessage[] {
  return (messages ?? []).map((m, i) => ({ ...m, id: from + i }));
}

export function windowFromPage(page: { messages: ChatMessage[]; start: number; rev?: number }): TranscriptWindow {
  return { start: page.start, rows: indexed(page.start, page.messages), rev: page.rev ?? 0 };
}

/** Older rows from a `load_chat_messages_page(before: w.start)` read, put in front of the window. */
export function prependPage(w: TranscriptWindow, page: { messages: ChatMessage[]; start: number }): void {
  const older = indexed(page.start, page.messages).filter((m) => m.id < w.start);
  w.rows = [...older, ...w.rows];
  w.start = Math.min(w.start, page.start);
}

/**
 * - `applied`: the window now reflects `rev`.
 * - `stale`: already seen (or nothing loaded yet — the loader's subscribe covers it); ignore.
 * - `gap`: a splice in between was missed; catch up with chat_subscribe(w.rev).
 */
export type SpliceResult = "applied" | "stale" | "gap";

export function applySplice(w: TranscriptWindow, sp: ChatSplice): SpliceResult {
  if (w.rev < 0) return "stale";
  if (sp.authoritative ? sp.rev < w.rev : sp.rev <= w.rev) return "stale";
  if (!sp.authoritative && sp.baseRev !== w.rev) return "gap";
  const rows = indexed(sp.fromIndex, sp.messages);
  const rel = sp.fromIndex - w.start;
  if (rel < 0 && sp.authoritative) {
    // Cut above the window (clear, or a rewind into unloaded history): the window restarts there.
    w.start = sp.fromIndex;
    w.rows = rows;
  } else if (rel < 0) {
    // Reaches back over rows this window never loaded: keep the window's start.
    w.rows = rows.slice(-rel);
  } else if (rel > w.rows.length) {
    // Starts past the window's end: what lies between is history again ("Load earlier").
    w.start = sp.fromIndex;
    w.rows = rows;
  } else {
    w.rows = [...w.rows.slice(0, rel), ...rows];
  }
  w.rev = sp.rev;
  return "applied";
}

/** Apply a tail append in place. Only on top of the rev it was cut at; a repeat is harmless (offset). */
export function applyTail(w: TranscriptWindow, t: ChatTail): boolean {
  if (t.rev !== w.rev) return false;
  const row = w.rows[t.index - w.start];
  // Shorter than the offset: a tail was missed; the next splice heals it.
  if (!row || row.text.length < t.offset) return false;
  row.text = row.text.slice(0, t.offset) + t.append;
  return true;
}

/** Apply a chat_subscribe reply: its splices in order, then the streaming row's text so far. */
export function applySubscribe(w: TranscriptWindow, reply: ChatSubscribeReply): SpliceResult {
  for (const sp of reply.splices ?? []) {
    if (applySplice(w, sp) === "gap") return "gap";
  }
  // Go's splices since sinceRev end at reply.rev by contract.
  if (reply.rev > w.rev) w.rev = reply.rev;
  const p = reply.inProgress;
  const row = p ? w.rows[p.index - w.start] : undefined;
  if (p && row) row.text = p.text;
  return "applied";
}

/** A client-only row (echo, pending-request marker, error notice): shown before transcript row `after`. */
export interface LocalRow {
  row: ChatMessage;
  after: number;
}

/** The window with client-only rows interleaved at their anchors. */
export function mergeLocal(w: { start: number; rows: readonly ChatMessage[] }, local: readonly LocalRow[]): ChatMessage[] {
  if (!local.length) return w.rows as ChatMessage[];
  const sorted = [...local].sort((a, b) => a.after - b.after);
  const out: ChatMessage[] = [];
  let li = 0;
  w.rows.forEach((row, i) => {
    while (li < sorted.length && sorted[li].after <= w.start + i) out.push(sorted[li++].row);
    out.push(row);
  });
  while (li < sorted.length) out.push(sorted[li++].row);
  return out;
}
